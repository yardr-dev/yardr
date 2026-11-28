package pack

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"yardr.dev/yardr/internal/depotfile"
	"yardr.dev/yardr/internal/store"
)

// depotRole names the depot's own role file that stands in for the pack's role
// rel (roles/builder.md -> .yardr/roles/builder.md), or "" when the depot has
// none or the pack's file is no role by yardr's naming (roles/<name>.md).
// A new group gets the pack's role unless the import chooses the depot's
// role; a new crew member always gets the pack's role.
// The depot's roles are read together on the first asked for, instead of
// starting git per role.
func (b *planner) depotRole(rel string) (string, error) {
	dir, name := filepath.Split(filepath.FromSlash(rel))
	if filepath.Clean(dir) != "roles" || filepath.Ext(name) != ".md" {
		return "", nil
	}
	if !b.rolesRead {
		b.rolesRead = true
		var names []string
		seen := map[string]bool{}
		add := func(rel string) {
			d, n := filepath.Split(filepath.FromSlash(rel))
			if filepath.Clean(d) == "roles" && filepath.Ext(n) == ".md" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		for _, pr := range b.p.Crew {
			if pr.Role != "" {
				add(pr.Role)
			}
		}
		for _, g := range b.p.Groups {
			if g.Agent != "" {
				add(g.Agent)
			}
		}
		rels := make([]string, len(names))
		for i, n := range names {
			rels[i] = filepath.Join(".yardr", "roles", n)
		}
		files, _, err := depotfile.ReadDepotFiles(b.depot, rels...)
		if err != nil {
			b.rolesErr = err
		} else {
			b.roles = map[string]depotfile.File{}
			for i, n := range names {
				b.roles[n] = files[i]
			}
		}
	}
	if b.rolesErr != nil {
		return "", fmt.Errorf("depot %s: looking for its own role %s: %w", b.depot.Name, name, b.rolesErr)
	}
	if f := b.roles[name]; f.Err != nil {
		return "", fmt.Errorf("depot %s: looking for its own role %s: %w", b.depot.Name, name, f.Err)
	} else if !f.Found {
		return "", nil
	}
	return ".yardr/roles/" + name, nil
}

// Ident is the record of an object the pack carries: what a later import
// finds it by. ok is false for what is not the pack's to own: a crew member
// and a group that takes the pack's, which stay the yard's, and an object
// the pack does not carry.
func Ident(o Object) (po store.PackObject, ok bool) {
	if o.Action == Dropped {
		if o.Owned == nil {
			return po, false
		}
		return *o.Owned, true
	}
	if o.Action == Takes {
		return po, false
	}
	switch {
	case o.Kind == "file" && o.File != nil:
		// A line's record holds names and no sums; only a connector's
		// declared source keeps its declaration's sum.
		return store.PackObject{Kind: o.Kind, Name: o.File.To}, true
	case o.Kind == "group":
		// What this import binds, or what the last one did and still stands.
		binds := o.bindsKept
		if o.Binds != "" {
			binds = o.Binds
		}
		return store.PackObject{Kind: o.Kind, Name: o.Name, Binds: binds}, true
	case o.Kind == "flow":
		return store.PackObject{Kind: o.Kind, Name: o.Name}, true
	case o.Kind == "pointer" && o.Pointer != nil:
		return store.PackObject{Kind: o.Kind, Type: o.Pointer.Type, Flow: o.Pointer.Flow}, true
	case o.Kind == "route" && o.Route != nil:
		return store.PackObject{Kind: o.Kind, Stage: o.Route.Stage, Type: o.Route.Type, Group: o.Route.Group}, true
	case o.Kind == "subscription" && o.Subscription != nil && o.Subscription.Add != nil:
		return store.PackObject{Kind: o.Kind, CrewMember: o.Subscription.CrewMember, Subscription: o.Subscription.Add}, true
	case o.Kind == "map" && o.Map != nil:
		return store.PackObject{Kind: o.Kind, Name: o.Map.Hook, CrewMember: o.Map.As, Map: o.Map.Map, Says: o.Map.Says, Makes: o.Map.Makes}, true
	case o.Kind == "source" && o.Source != nil:
		return store.PackObject{Kind: o.Kind, Name: o.Source.Name, SHA256: sum([]byte(o.Source.Text))}, true
	}
	return po, false
}

// dropped adds declared sources that the last import of this pack brought
// and the pack no longer carries, as far as the yard still has them. A prune
// removes them, except those held. A line's dropped groups, routes, crew
// members, subscriptions and hook lines stay untouched and are not listed.
func (b *planner) dropped() error {
	if b.rec == nil {
		return nil
	}
	carried := map[string]bool{}
	for _, o := range b.plan.Objects {
		if po, ok := Ident(o); ok {
			carried[po.Key()] = true
		}
	}
	for _, po := range b.rec.Objects {
		if carried[po.Key()] {
			continue
		}
		o, err := b.gone(po)
		if err != nil {
			return err
		}
		if o == nil {
			continue // the yard no longer has it
		}
		o.Action, o.Owned = Dropped, &po
		b.add(*o)
	}
	return nil
}

// gone is the declared source an earlier import recorded as po, as the yard
// has it now and with what removing it takes; nil for every other kind or
// when the yard no longer has it.
func (b *planner) gone(po store.PackObject) (*Object, error) {
	switch po.Kind {
	case "source":
		src, err := b.st.GetSource(b.ctx, po.Name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
		// A declaration added by hand since is the yard's.
		if sum([]byte(src.Text)) != po.SHA256 {
			return nil, nil
		}
		o := &Object{Kind: po.Kind, Name: po.Name, Source: &SourceOp{Name: po.Name, Text: src.Text}}
		// A source is the yard's, shared by its depots, and a user who is
		// told through it would be told nowhere without a word.
		for _, r := range b.all {
			if (r.Pack != b.p.Name || r.Depot != b.o.Depot) && slices.ContainsFunc(r.Objects, func(x store.PackObject) bool { return x.Kind == "source" && x.Name == po.Name }) {
				o.Held = fmt.Sprintf("pack %s in %s names it too", r.Pack, inDepot(r.Depot))
			}
		}
		users, err := b.st.Users(b.ctx)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if o.Held == "" && u.Notify == po.Name {
				o.Held = fmt.Sprintf("user %s is told through it (yardr user set %s %s= ends that)", u.Name, u.Name, store.UserNotify)
			}
		}
		return o, nil
	}
	return nil, nil
}
