package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
)

// printPlanMaps prints each planned stage map and hook table section of p.
// Hook lines already present are left alone: Create prints what is set or
// added, and Differs prints the hook's own as or map left as it is.
func printPlanMaps(w io.Writer, p *pack.Plan, optional map[string]bool, refused map[string]string) {
	for _, m := range p.Maps {
		if optional[m.Source] {
			continue
		}
		if m.Pack != "" {
			fmt.Fprintf(w, "\nstage map of source %s, as crew member %s\n", m.Source, m.As)
			fmt.Fprintf(w, "  the pack says %s\n", strings.Join(pack.MapLines(m.Map), " "))
		} else {
			fmt.Fprintf(w, "\nwhat the events of source %s say of a bead, and make\n", m.Source)
		}
		for _, l := range store.SayLines(m.Says) {
			fmt.Fprintf(w, "  the pack says %s\n", l)
		}
		for _, l := range store.MakeLines(m.Makes) {
			fmt.Fprintf(w, "  the pack makes %s\n", l)
		}
		var mapChanges []pack.Diff
		for _, o := range p.Objects {
			if o.Kind == "map" && o.Map != nil && o.Map.Hook == m.Hook && o.Map.Source == m.Source {
				mapChanges = o.Changes
				break
			}
		}
		switch {
		case refused[m.Source] != "":
			fmt.Fprintf(w, "  not set on hook %s: %s\n", m.Hook, refused[m.Source])
		case m.Skipped != "":
			fmt.Fprintf(w, "  skipped: %s\n", m.Skipped)
		case m.Action == pack.Create:
			for _, d := range mapChanges {
				old, _ := d.Old.(string)
				switch d.Field {
				case "as":
					if old != "" {
						fmt.Fprintf(w, "  hook %s keeps its own as (%s)\n", m.Hook, old)
					} else {
						fmt.Fprintf(w, "  would set as %s on hook %s\n", planValue(d.New), m.Hook)
					}
				case "map":
					if old != "" {
						fmt.Fprintf(w, "  hook %s keeps its own map (%s)\n", m.Hook, old)
					} else {
						fmt.Fprintf(w, "  would set map on hook %s\n", m.Hook)
					}
				case "says":
					var have []string
					if old != "" {
						have = strings.Split(old, "; ")
					}
					for _, l := range store.SayLines(m.Says) {
						if !slices.Contains(have, l) {
							fmt.Fprintf(w, "  would add the says line %q to hook %s\n", l, m.Hook)
						}
					}
				case "makes":
					var have []string
					if old != "" {
						have = strings.Split(old, "; ")
					}
					for _, l := range store.MakeLines(m.Makes) {
						if !slices.Contains(have, l) {
							fmt.Fprintf(w, "  would add the makes line %q to hook %s\n", l, m.Hook)
						}
					}
				}
			}
		case m.Action == pack.Differs:
			for _, d := range mapChanges {
				if d.Field == "as" || d.Field == "map" {
					fmt.Fprintf(w, "  hook %s keeps its own %s (%s)\n", m.Hook, d.Field, planValue(d.Old))
				}
			}
		case m.Action == pack.Unchanged:
			fmt.Fprintf(w, "  hook %s has it\n", m.Hook)
		default:
			fmt.Fprintln(w, "  nothing would be set: the import goes through without it, and the next one sets it once the yard has what is missing")
		}
		for _, n := range m.Needs {
			fmt.Fprintf(w, "  %-8s %-12s %s\n", n.Status, strings.ReplaceAll(n.What, "_", " "), n.Detail)
		}
	}
}

// printPlanDeclared prints, for each source the pack declares, where its
// calls go and what an import would do about it: added, replaced with
// what the replacement widens or still grants to somebody already, or
// already had; and a warning for what its declaration holds that is read
// otherwise than it looks. refused says, by source, why a declared source
// the plan would add is left out by this run.
func printPlanDeclared(w io.Writer, p *pack.Plan, refused map[string]string) {
	for _, d := range p.Declared {
		fmt.Fprintf(w, "\ndeclared source %s, from %s\n", d.Source, d.File)
		fmt.Fprintf(w, "  its calls go to %s: %s\n", d.Base, strings.Join(d.Calls, ", "))
		for _, c := range d.Commands {
			if c.SHA256 == "" {
				fmt.Fprintf(w, "  command %s: %s (the program is the operator's own, not copied)\n", c.Key, c.Program)
			} else {
				fmt.Fprintf(w, "  command %s: %s, sha256 %s\n", c.Key, c.Program, c.SHA256)
			}
		}
		switch {
		case refused[d.Source] != "":
			fmt.Fprintf(w, "  not added: %s\n", refused[d.Source])
		case d.Action == pack.Create:
			fmt.Fprintln(w, "  would be added to the yard's sources")
		case d.Action == pack.Change:
			fmt.Fprintln(w, "  would replace the declaration the yard has under that name, which keeps its env_file (what changes is under declared sources, above)")
		default:
			fmt.Fprintln(w, "  the yard has it")
		}
		for _, l := range d.Warnings {
			fmt.Fprintf(w, "  warning: %s\n", l)
		}
		if r := d.Reaches; r != nil {
			who := make([]string, len(r.Who))
			for i, holder := range r.Who {
				who[i] = holder.Holder()
			}
			fmt.Fprintf(w, "  through their grant of %s, %s would then call: %s (nobody is asked: %s covers the read calls a later file adds; yardr source revoke %s %s takes it back)\n",
				store.GrantRead, strings.Join(who, ", "), strings.Join(r.Calls, ", "), store.GrantRead, d.Source, store.GrantRead)
		}
		for _, l := range d.StillGranted {
			fmt.Fprintf(w, "  %s\n", l)
		}
		for _, n := range d.Needs {
			fmt.Fprintf(w, "  %-8s %-12s %s\n", n.Status, strings.ReplaceAll(n.What, "_", " "), n.Detail)
		}
	}
}
