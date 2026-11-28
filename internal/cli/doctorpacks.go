package cli

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/version"
	"yardr.dev/yardr/packs"
)

// stalePackNotes are the imports of embedded packs whose copied files
// differ from or are missing against what the running binary carries.
// Each differing script names its --replace command, missing files keep
// the pack apply advice, and other differing files are counted with the
// pack check --diff command. Embedded sources alone are compared: a
// directory, URL or earlier record is not compared. Notes, never quieted
// and never faults.
func stalePackNotes(ctx context.Context, st store.API, cfg *config.Config) ([]string, error) {
	recs, err := st.Packs(ctx)
	if err != nil {
		return nil, err
	}
	yardr := yardrAt(cfg.Home)
	build := version.Describe()
	var notes []string
	for _, rec := range recs {
		if !fromEmbedded(rec.Source) || !packs.Carries(rec.Pack) {
			continue
		}
		sub, err := fs.Sub(packs.FS, rec.Pack)
		if err != nil {
			continue
		}
		p, err := pack.Load(sub, packs.Embedded+" in yardr "+build)
		if err != nil {
			continue
		}
		// The last import's flow, crew, groups and roles, as a check
		// without flags repeats them: what differs is the binary's.
		plan, err := pack.Build(ctx, st, p, pack.Options{Depot: rec.Depot, Home: cfg.Home})
		if err != nil {
			continue
		}
		var diffScripts, diffOther, missing []string
		for _, o := range plan.Objects {
			if o.Kind != "file" {
				continue
			}
			switch o.Action {
			case pack.Differs:
				if o.File != nil && o.File.Mode == "0755" {
					diffScripts = append(diffScripts, o.Name)
				} else {
					diffOther = append(diffOther, o.Name)
				}
			case pack.Create:
				missing = append(missing, o.Name)
			}
		}
		if len(diffScripts) == 0 && len(diffOther) == 0 && len(missing) == 0 {
			continue
		}
		depot := ""
		if rec.Depot != "" {
			depot = " --depot " + rec.Depot
		}
		var parts []string
		for _, s := range diffScripts {
			parts = append(parts, fmt.Sprintf("%s differs from yardr %s (take it: %s pack apply %s%s --replace %s)",
				s, build, yardr, rec.Pack, depot, s))
		}
		if len(diffOther) > 0 {
			verb := "differs"
			if len(diffOther) > 1 {
				verb = "differ"
			}
			if len(diffScripts) > 0 {
				parts = append(parts, fmt.Sprintf("%s %s (%s pack check %s%s --diff)",
					plural(len(diffOther), "other file"), verb, yardr, rec.Pack, depot))
			} else {
				parts = append(parts, fmt.Sprintf("%s %s from yardr %s (%s pack check %s%s --diff)",
					plural(len(diffOther), "file"), verb, build, yardr, rec.Pack, depot))
			}
		}
		if len(missing) > 0 {
			names := missing
			if len(names) > 3 {
				names = append(slices.Clone(names[:3]), "...")
			}
			parts = append(parts, fmt.Sprintf("%s not yet copied (%s): %s pack apply %s%s",
				plural(len(missing), "file"), strings.Join(names, ", "), yardr, rec.Pack, depot))
		}
		notes = append(notes, fmt.Sprintf("pack %s in %s: %s",
			rec.Pack, inDepot(rec.Depot), strings.Join(parts, "; ")))
	}
	return notes, nil
}
