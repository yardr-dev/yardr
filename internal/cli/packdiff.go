package cli

import (
	"fmt"
	"io"
	"strings"

	"yardr.dev/yardr/internal/pack"
)

// printPlanDiffHint prints one line after a plain pack check when any object
// differs, pointing at --diff to see both texts and their unified diff.
func printPlanDiffHint(w io.Writer, p *pack.Plan, arg string) {
	n := 0
	for _, o := range p.Objects {
		if o.Action == pack.Differs {
			n++
		}
	}
	if n == 0 {
		return
	}
	cmd := "yardr pack check " + arg
	if p.Depot != "" {
		cmd += " --depot " + p.Depot
	}
	cmd += " --diff"
	fmt.Fprintf(w, "\n%d differ and are left as the yard has them: %s shows both texts of each\n", n, cmd)
}

// printPlanDiffs prints, for each differing object of p, the yard's text,
// the pack's original text and their unified diff.
func printPlanDiffs(w io.Writer, p *pack.Plan) {
	for _, o := range p.Objects {
		if o.Action != pack.Differs {
			continue
		}
		fmt.Fprintf(w, "\n=== %s %s ===\n", diffLabel(o.Kind), diffLabel(o.Name))
		if o.YardText == nil && o.OriginalText == nil && o.Diff == nil {
			fmt.Fprintln(w, "(non-text file; no diff shown)")
			continue
		}
		yardLabel, origLabel := "yard", "original"
		if o.Kind == "file" && o.File != nil {
			yardLabel = "yard (" + diffLabel(o.File.To) + ")"
			origLabel = "original (" + diffLabel(o.File.From) + ")"
		}
		fmt.Fprintf(w, "--- %s ---\n", yardLabel)
		if o.YardText != nil {
			writeBlock(w, escapeControls(*o.YardText))
		}
		fmt.Fprintf(w, "--- %s ---\n", origLabel)
		if o.OriginalText != nil {
			writeBlock(w, escapeControls(*o.OriginalText))
		}
		fmt.Fprintln(w, "--- diff ---")
		if o.Diff == nil || *o.Diff == "" {
			fmt.Fprintln(w, "(texts match; only metadata differs)")
		} else {
			writeBlock(w, escapeControls(*o.Diff))
		}
	}
}

func diffLabel(s string) string {
	return strings.NewReplacer("\n", `\n`, "\t", `\t`).Replace(escapeControls(s))
}

func writeBlock(w io.Writer, s string) {
	if s == "" {
		return
	}
	io.WriteString(w, s)
	if !strings.HasSuffix(s, "\n") {
		io.WriteString(w, "\n")
	}
}
