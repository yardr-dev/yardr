package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
)

type packNeedsEntry struct {
	Pack  string                `json:"pack"`
	Depot string                `json:"depot,omitempty"`
	Needs []pack.LineNeedStatus `json:"needs"`
}

type packNeedsEdit struct {
	Pack    string `json:"pack"`
	Depot   string `json:"depot,omitempty"`
	Need    string `json:"need"`
	Action  string `json:"action"`
	Changed bool   `json:"changed"`
}

// printPlanNeeds prints the checked needs of a line in a plan (without
// running check commands).
func printPlanNeeds(w io.Writer, p *pack.Plan) {
	if len(p.LineNeeds) == 0 {
		return
	}
	fmt.Fprintln(w, "\nneeds")
	for _, n := range p.LineNeeds {
		fmt.Fprintf(w, "  %s\n", pack.FormatLineNeed(n, "yardr"))
		if len(n.Check) > 0 {
			fmt.Fprintf(w, "    check command: %s\n", strings.Join(n.Check, " "))
		}
	}
}

// printPackNeeds prints the needs declared in pack.yaml for pack show,
// without running any check commands.
func printPackNeeds(w io.Writer, p *pack.Pack) {
	fmt.Fprintf(w, "needs of pack %s:\n", p.Name)
	for _, n := range p.Needs {
		if n.Kind() == pack.LineNeedConnector {
			fmt.Fprintf(w, "  connector: %s\n", n.Connector)
			continue
		}
		line := "  " + n.Name
		if len(n.Check) > 0 {
			line += " (" + pack.CheckNotRunDetail + ")"
		}
		fmt.Fprintln(w, line)
		if len(n.Check) > 0 {
			fmt.Fprintf(w, "    check command: %s\n", strings.Join(n.Check, " "))
		}
	}
}

func (a *app) packNeedsCmd() *cobra.Command {
	var depot string
	cmd := &cobra.Command{
		Use:   "needs [<line> [add|rm <need>]]",
		Short: "Check or edit the tools and connectors an applied line needs",
		Long: `Checks the tools and connectors recorded on each applied line (or the
one named), running any check commands on this machine unless every group of
the line is worked by a peer runner.

With add or rm, edits the yard's recorded needs list for the named line (on
every depot it is applied to, or only the one --depot names). <need> is a
tool name or connector:<source>. A tool with any, env or check can be removed
with rm, but add takes a bare tool name or connector:<source> alone.`,
		Example: `  yardr pack needs
  yardr pack needs visuals --depot studio
  yardr pack needs visuals add ffmpeg
  yardr pack needs visuals rm ffmpeg
  yardr pack needs visuals add connector:acme`,
		Args: func(_ *cobra.Command, args []string) error {
			switch len(args) {
			case 0, 1:
				return nil
			case 3:
				if args[1] != "add" && args[1] != "rm" {
					return fmt.Errorf("unknown action %q: want add or rm (yardr pack needs <line> add|rm <need>)", args[1])
				}
				return nil
			default:
				return fmt.Errorf("want [<line>] or <line> add|rm <need>, got %d arguments", len(args))
			}
		},
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 3 {
				return a.packNeedsEditRun(c, args[0], args[1], args[2], depot)
			}
			line := ""
			if len(args) == 1 {
				line = args[0]
			}
			return a.packNeedsListRun(c, line, depot)
		},
	}
	cmd.Flags().StringVar(&depot, "depot", "", "limit to the line imported into this depot")
	return cmd
}

func filterPackRecords(recs []store.PackImport, line, depot string) []store.PackImport {
	var out []store.PackImport
	for _, r := range recs {
		if line != "" && r.Pack != line {
			continue
		}
		if depot != "" && r.Depot != depot {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (a *app) packNeedsListRun(c *cobra.Command, line, depot string) error {
	ctx := c.Context()
	st, err := a.store(ctx)
	if err != nil {
		return err
	}
	cfg, err := a.config()
	if err != nil {
		return err
	}
	recs, err := st.Packs(ctx)
	if err != nil {
		return err
	}
	matched := filterPackRecords(recs, line, depot)
	if line != "" && len(matched) == 0 {
		if depot != "" {
			return fmt.Errorf("no imported line named %q in depot %s", line, depot)
		}
		return fmt.Errorf("no imported line named %q", line)
	}
	srcs, err := st.Sources(ctx)
	if err != nil {
		return err
	}
	groups, err := st.Groups(ctx)
	if err != nil {
		return err
	}
	yardr := yardrAt(cfg.Home)
	entries := []packNeedsEntry{}
	for _, rec := range matched {
		statuses, err := pack.CheckRecordNeeds(ctx, srcs, groups, rec)
		if err != nil {
			return fmt.Errorf("line %s in %s: %w", rec.Pack, inDepot(rec.Depot), err)
		}
		if len(statuses) == 0 {
			continue
		}
		entries = append(entries, packNeedsEntry{Pack: rec.Pack, Depot: rec.Depot, Needs: statuses})
	}
	return a.out(c.OutOrStdout(), entries, func(w io.Writer) {
		if len(entries) == 0 {
			if line != "" {
				fmt.Fprintf(w, "line %s: no needs recorded\n", line)
			} else {
				fmt.Fprintln(w, "no line needs recorded")
			}
			return
		}
		for _, e := range entries {
			for _, s := range e.Needs {
				fmt.Fprintln(w, pack.FormatLineNeedLine(e.Pack, e.Depot, s, yardr))
			}
		}
	})
}

func (a *app) packNeedsEditRun(c *cobra.Command, line, verb, word, depot string) error {
	need, err := pack.ParseLineNeedWord(word)
	if err != nil {
		return err
	}
	ctx := c.Context()
	st, err := a.store(ctx)
	if err != nil {
		return err
	}
	recs, err := st.Packs(ctx)
	if err != nil {
		return err
	}
	matched := filterPackRecords(recs, line, depot)
	if len(matched) == 0 {
		if depot != "" {
			return fmt.Errorf("no imported line named %q in depot %s", line, depot)
		}
		return fmt.Errorf("no imported line named %q", line)
	}
	edits := make([]packNeedsEdit, 0, len(matched))
	for _, rec := range matched {
		var updated string
		var changed bool
		if verb == "add" {
			updated, changed, err = pack.AddLineNeed(rec.Needs, need)
		} else {
			updated, changed, err = pack.RemoveLineNeed(rec.Needs, need)
		}
		if err != nil {
			return fmt.Errorf("line %s in %s: %w", rec.Pack, inDepot(rec.Depot), err)
		}
		if changed {
			rec.Needs = updated
			if _, err := st.RecordPack(ctx, rec, false, a.who()); err != nil {
				return err
			}
		}
		edits = append(edits, packNeedsEdit{Pack: rec.Pack, Depot: rec.Depot, Need: need.Word(), Action: verb, Changed: changed})
	}
	return a.out(c.OutOrStdout(), edits, func(w io.Writer) {
		for _, e := range edits {
			switch {
			case e.Action == "add" && e.Changed:
				fmt.Fprintf(w, "line %s in %s: added %s\n", e.Pack, inDepot(e.Depot), e.Need)
			case e.Action == "add":
				fmt.Fprintf(w, "line %s in %s: already needs %s; nothing changed\n", e.Pack, inDepot(e.Depot), e.Need)
			case e.Changed:
				fmt.Fprintf(w, "line %s in %s: removed %s\n", e.Pack, inDepot(e.Depot), e.Need)
			default:
				fmt.Fprintf(w, "line %s in %s: does not list %s; nothing changed\n", e.Pack, inDepot(e.Depot), e.Need)
			}
		}
	})
}
