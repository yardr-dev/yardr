// The --replace half of the pack apply row: where a line's file under
// <home>/packs/<line>/ (a role's copy, a script or the page frame) differs
// from the line's original, a card asked with --replace <file> overwrites
// the yard's copy with the line's bytes. It shows the file whole, weighs
// code, is never summarised, and approval is held to the hashes the card
// showed.
package store

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// packReplaceKey is the context key of the file names a pack card replaces.
type packReplaceKey struct{}

// WithPackReplace gives a pack reading the file names the card replaces, as
// the terminal's --replace names them: a reader refuses a name its plan cannot
// carry out (Plan.CheckReplace) and carries each replaced file as a change
// whole, which a card that replaces must show.
func WithPackReplace(ctx context.Context, replace []string) context.Context {
	if len(replace) == 0 {
		return ctx
	}
	return context.WithValue(ctx, packReplaceKey{}, replace)
}

// PackReplaceOf are the file names a reading is asked to replace
// (WithPackReplace), nil for none.
func PackReplaceOf(ctx context.Context) []string {
	replace, _ := ctx.Value(packReplaceKey{}).([]string)
	return replace
}

// packReplaceNames reads the --replace words of pack apply into the card's
// one stored form: each a clean relative slash path under the line's
// directory, none twice, sorted, so the same replace in another order is the
// same card. Whether the plan has that differing file is the reading's to say.
func packReplaceNames(words []string) ([]string, error) {
	var out []string
	for _, name := range words {
		if !fs.ValidPath(name) || name == "." {
			return nil, fmt.Errorf("--run takes %s: --replace %s is not a relative file path under the line's directory (scripts/<name>, roles/<name>)", PackApplyUse, name)
		}
		if slices.Contains(out, name) {
			return nil, fmt.Errorf("--run takes %s: --replace names %s twice", PackApplyUse, name)
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out, nil
}

// packSourceWords refuses flags a pack of kind source takes none of: it has
// no files under a line's directory to replace.
func packSourceWords(name string, replace []string) error {
	if len(replace) > 0 {
		return fmt.Errorf("pack %s: --replace sets the original's version of a differing file under a line's directory, and a pack of kind source has none: propose the apply without it", name)
	}
	return nil
}

// packReplaceWhole holds each file named by --replace to what the reading
// carried: a file object marked as change with both its standing and new
// hashes, so a card never overwrites a file it does not show whole.
func packReplaceWhole(name string, reading *PackReading, replace []string) error {
	for _, f := range replace {
		i := slices.IndexFunc(reading.Plan.Objects, func(o AskPlanObject) bool { return o.Kind == "file" && o.Name == f })
		switch {
		case i < 0 || reading.Plan.Objects[i].Action != "change":
			return fmt.Errorf("pack %s: --replace %s: the file does not differ in the plan, so there is nothing to replace (yardr pack check %s --depot %s shows what differs)", name, f, name, reading.Depot)
		case reading.Plan.Objects[i].FileNewSHA == "" || reading.Plan.Objects[i].FileOldSHA == "" || reading.Plan.Objects[i].OldText == nil:
			return fmt.Errorf("pack %s: --replace %s: the reading carries nothing of it to show whole, so this is the operator's to type in a terminal (yardr pack apply %s --depot %s --replace %s)", name, f, name, reading.Depot, f)
		}
	}
	return nil
}

// packReplaceDoes appends the card's own sentence for each file it replaces:
// approval overwrites the yard's copy with the line's bytes and keeps no
// other copy of it.
func packReplaceDoes(does []string, replace []string) []string {
	for _, f := range replace {
		does = append(does, fmt.Sprintf("file %s: approval overwrites the yard's copy with the line's bytes and keeps no other copy of it.", f))
	}
	return does
}

// packPlanFileLines are the full texts and diff a card prints for a file:
// both sides of a replacement stand beside the hashes approval is held to.
func packPlanFileLines(o AskPlanObject) []string {
	var out []string
	for _, change := range o.Changes {
		if change.Field == "mode" {
			out = append(out, fmt.Sprintf("  the file %s mode changes %s -> %s.", OneLine(o.Name), OneLine(change.Old), OneLine(change.New)))
		}
	}
	if o.OldText != nil {
		out = append(out, "  the file "+OneLine(o.Name)+" as it stands in the yard, whole as carried:")
		out = append(out, packTextLines(*o.OldText)...)
	}
	if o.Diff != "" {
		out = append(out, "  the change to file "+OneLine(o.Name)+" (sha256 "+OneLine(or(o.FileOldSHA, "(none)"))+" -> "+OneLine(or(o.FileNewSHA, "(none)"))+"), whole as carried:")
		for _, line := range strings.Split(strings.TrimSuffix(o.Diff, "\n"), "\n") {
			out = append(out, "      "+NoteLetters(line))
		}
	}
	if o.Text != "" || o.FileNewSHA != "" {
		out = append(out, "  the file "+OneLine(o.Name)+" as the pack would install it, whole as carried:")
		out = append(out, packTextLines(o.Text)...)
	}
	return out
}

// packTextLines are a file's whole lines in a card; an empty file still has
// a line so it cannot be mistaken for text the card omitted.
func packTextLines(text string) []string {
	if text == "" {
		return []string{"      (empty file)"}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i := range lines {
		lines[i] = "      " + NoteLetters(lines[i])
	}
	return lines
}

// packReplaceWeight weighs a --replace card as code: a role, script or page
// frame under the line's directory is what the yard runs, briefs agents with
// or serves in pages, so replacing the yard's copy takes the strong doors.
func packReplaceWeight(run AskRun) []AskWeight {
	if run.Pack == nil || len(run.Pack.Replace) == 0 {
		return nil
	}
	return []AskWeight{weighs(WeightCode, fmt.Sprintf("the pack %s's version of %s, set over the yard's copy", run.Pack.Name, andList(run.Pack.Replace)))}
}
