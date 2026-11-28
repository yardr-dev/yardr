package cli

import (
	"fmt"
	"io"
	"strings"

	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
)

func printRecord(w io.Writer, r store.PackImport) {
	fmt.Fprintf(w, "%s in %s  %s by %s\n", r.Pack, inDepot(r.Depot), r.At.Local().Format("2006-01-02 15:04"), r.By)
	src := r.Source
	if r.Commit != "" {
		src += " at " + r.Commit[:min(12, len(r.Commit))]
		if r.Dirty {
			src += " with uncommitted changes"
		}
	}
	fmt.Fprintf(w, "  source     %s\n", src)
	if r.Auth != "" {
		fmt.Fprintf(w, "  auth       %s\n", r.Auth)
	}
	if r.Flow != "" {
		fmt.Fprintf(w, "  flow       %s (stored as %s)\n", r.Flow, pack.FlowName(r.Pack, r.Flow))
	}
	pairs := func(m map[string]string) string {
		var out []string
		for _, k := range sortedKeys(m) {
			out = append(out, k+" -> "+m[k])
		}
		return or(strings.Join(out, ", "), "none")
	}
	roles := "the pack's"
	if r.DepotRoles {
		roles = "the depot's own where it has them (--depot-roles)"
	}
	fmt.Fprintf(w, "  roles      %s\n", roles)
	fmt.Fprintf(w, "  groups     %s\n", pairs(r.Groups))
	fmt.Fprintf(w, "  crew       %s\n", pairs(r.Crew))
	count := map[string]int{}
	for _, o := range r.Objects {
		count[o.Kind]++
	}
	var parts []string
	for _, kind := range planKinds {
		if n := count[kind[0]]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, kind[1]))
		}
	}
	fmt.Fprintf(w, "  objects    %s\n", or(strings.Join(parts, ", "), "none"))
	for _, name := range sortedKeys(r.OpenThreads) {
		var said []string
		for _, src := range sortedKeys(r.OpenThreads[name]) {
			said = append(said, src+" "+map[bool]string{true: "yes", false: "no"}[r.OpenThreads[name][src]])
		}
		fmt.Fprintf(w, "  may open   %s: %s\n", name, strings.Join(said, ", "))
	}
	for _, who := range sortedKeys(r.Calls) {
		var said []string
		for _, call := range sortedKeys(r.Calls[who]) {
			said = append(said, call+" "+map[bool]string{true: "yes", false: "no"}[r.Calls[who][call]])
		}
		fmt.Fprintf(w, "  may call   %s: %s\n", who, strings.Join(said, ", "))
	}
}
