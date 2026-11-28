package pack

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

func (r *reader) group(e entry) Group {
	g := Group{Name: e.key}
	var kinds []string
	var calls, env *entry
	for _, f := range r.entries(e.v, e.path, "agent", "run", "people", "limit", "binds_as", "calls", "env") {
		switch f.key {
		case "binds_as":
			if as, ok := r.str(f.v, f.path); ok {
				g.BindsAs = as
				r.binds = append(r.binds, boundAs{n: f.v, key: f.path, group: g.Name, as: as})
			}
		case "calls":
			calls = &f
			g.Calls = r.calls(f)
		case "env":
			env = &f
			g.Env = r.env(f)
		case "agent":
			kinds = append(kinds, f.key)
			g.Agent = r.rel(f.v, f.path, "role")
		case "run":
			kinds = append(kinds, f.key)
			g.Run = r.rel(f.v, f.path, "script")
		case "people":
			// A value that is no boolean has its own error; it still counts
			// as the kind, so the group is not also said to have none.
			var ok bool
			if g.People, ok = r.boolean(f.v, f.path); g.People || !ok {
				kinds = append(kinds, f.key)
			}
		case "limit":
			if n, ok := r.count(f.v, f.path, "beads"); ok {
				g.Limit = n
			}
		}
	}
	if e.v.Kind == yaml.MappingNode && len(kinds) != 1 {
		got := "none"
		if len(kinds) > 0 {
			got = strings.Join(kinds, " and ")
		}
		r.errf(e.k, e.path, "a group is exactly one of agent: <role file>, run: <script>, people: true; this one has %s", got)
	}
	// The variables are set in the cmd that runs the script. A group of no
	// kind has its own error, and an empty mapping says nothing.
	if env != nil && env.v.Kind == yaml.MappingNode && len(env.v.Content) > 0 && len(kinds) > 0 && !slices.Contains(kinds, "run") {
		r.errf(env.k, env.path, "an agent group or a people group has no cmd to set variables in: env is for a group with run: <script>")
		g.Env = nil
	}
	// A person's call is told by who they are, never by the group whose
	// stage they work in (store.sourceCallerIn): a grant to a people group
	// would let nobody call.
	if calls != nil && g.People {
		r.errf(calls.k, calls.path, "a people group makes no call: a person calls as who they are. calls is for a group with agent: or run:")
		g.Calls = nil
	}
	return g
}

// env reads a script group's env: variable names to the strings the import
// sets them to before the script. A name is what the shell takes for one,
// since setScript tells a cmd's assignments from its command by that rule.
// A value is a printable string of one line: pack check prints these words
// for the operator to judge what the pack sets, and a terminal would act on
// an escape or a line break in them. An empty mapping is no env.
func (r *reader) env(e entry) map[string]string {
	var out map[string]string
	for _, f := range r.entriesMatching(e.v, e.path, func(string) bool { return true }) {
		if !envName.MatchString(f.key) {
			r.errf(f.k, f.path, "name %q: use letters, digits and '_', and no digit first", f.key)
			continue
		}
		v, ok := r.str(f.v, f.path)
		if !ok {
			continue
		}
		if i := strings.IndexFunc(v, func(c rune) bool { return !unicode.IsPrint(c) }); i >= 0 {
			c, _ := utf8.DecodeRuneInString(v[i:])
			r.errf(f.v, f.path, "the value holds %U, which is not printable: a variable set before a script is one line of text", c)
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[f.key] = v
	}
	return out
}

// checkBindsAs holds each binds_as to the groups read: it names an agent
// group of the pack that comes before the one that says it, so the import has
// settled the named group's agent, model and arguments when it comes to this
// one, and no group binds as itself or in a circle.
func (r *reader) checkBindsAs(groups []Group) {
	for _, b := range r.binds {
		i := slices.IndexFunc(groups, func(g Group) bool { return g.Name == b.group })
		j := slices.IndexFunc(groups, func(g Group) bool { return g.Name == b.as })
		switch {
		case i >= 0 && groups[i].Agent == "":
			r.errf(b.n, b.key, "a group binds as another only with agent: it is the agent, model and arguments that are taken")
		case j < 0:
			names := make([]string, len(groups))
			for k, g := range groups {
				names[k] = g.Name
			}
			r.errf(b.n, b.key, "the pack has no group %s (it has: %s)", b.as, strings.Join(names, ", "))
		case j >= i:
			r.errf(b.n, b.key, "group %s is not declared before %s: declare it first, so it is bound when the import comes to this group", b.as, b.group)
		case groups[j].Agent == "":
			r.errf(b.n, b.key, "group %s is not an agent group: nothing of it is taken", b.as)
		}
	}
}
