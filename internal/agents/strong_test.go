package agents

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"yardr.dev/yardr/harnesses"
	"yardr.dev/yardr/internal/store"
)

// skipsPrompts says whether arg is a flag that skips an agent's permission
// prompts (design yardr-m90n 3.5): the --dangerously- flags,
// --always-approve, --yolo, --trust-all-tools and its --trust-tools form,
// --disable-approval, and --auto, which the files of kimi and opencode both
// give the meaning of asking nothing (claude's own auto is the value of
// --permission-mode, no flag of this name). The test names them itself and
// reads none from the code it holds, so the mark cannot be right by
// following a list that is wrong.
func skipsPrompts(arg string) bool {
	if strings.HasPrefix(arg, "--dangerously-") || strings.HasPrefix(arg, "--trust-tools") {
		return true
	}
	return slices.Contains([]string{"--always-approve", "--yolo", "--auto", "--trust-all-tools", "--disable-approval"}, arg)
}

// strongMarkProblems are the presets of presets whose Strong mark is not
// what their flags say: one that passes a flag that skips the agent's
// permission prompts without the mark, and one marked Strong that passes
// none.
func strongMarkProblems(presets map[string][]store.Permission) []string {
	var problems []string
	for kind, list := range presets {
		for _, p := range list {
			var skipping []string
			for _, arg := range p.Args {
				if skipsPrompts(arg) {
					skipping = append(skipping, arg)
				}
			}
			switch {
			case len(skipping) > 0 && !p.Strong:
				problems = append(problems, fmt.Sprintf("%s preset %q passes %s, which skips the agent's permission prompts, and is not marked Strong", kind, p.Label, strings.Join(skipping, " ")))
			case len(skipping) == 0 && p.Strong:
				problems = append(problems, fmt.Sprintf("%s preset %q is marked Strong and passes no flag that skips the agent's permission prompts", kind, p.Label))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// A preset is Strong exactly when its flags skip the agent's permission
// prompts, so a preset added later cannot reach a group or a crew member
// without the card that approves it, by omission or by a mark it does not
// earn (design yardr-m90n 3.5). The check is run on the files and on a
// doctored table, which it must fail twice, once for each way to be wrong.
func TestHarnessPresetsAreStrongWhenTheirFlagsAskNothing(t *testing.T) {
	t.Parallel()
	presets := map[string][]store.Permission{}
	for _, h := range Harnesses {
		presets[h.Kind] = h.Flags.Permissions
	}
	if problems := strongMarkProblems(presets); len(problems) > 0 {
		t.Errorf("harness files: %s", strings.Join(problems, "; "))
	}
	// The lookup the cards use hands the mark on with the preset.
	for _, c := range []struct {
		kind, label string
		strong      bool
	}{{"claude", "bypass", true}, {"claude", "auto", false}, {"codex", "approve for me", false}, {"kimi", "auto", true}} {
		if p, ok := Preset(c.kind, c.label); !ok || p.Strong != c.strong {
			t.Errorf("Preset(%q, %q) = %+v, %v, want Strong %v", c.kind, c.label, p, ok, c.strong)
		}
	}
	doctored := map[string][]store.Permission{"x": {
		{Label: "quiet", Args: []string{"--yolo"}},
		{Label: "careful", Args: []string{"--permission-mode", "auto"}, Strong: true},
		{Label: "asks", Args: []string{"--permission-mode", "acceptEdits"}},
		{Label: "trusting", Args: []string{"--trust-tools=*"}, Strong: true},
	}}
	want := []string{
		`x preset "careful" is marked Strong and passes no flag that skips the agent's permission prompts`,
		`x preset "quiet" passes --yolo, which skips the agent's permission prompts, and is not marked Strong`,
	}
	if got := strongMarkProblems(doctored); !slices.Equal(got, want) {
		t.Errorf("strongMarkProblems(doctored) = %q, want %q", got, want)
	}
	for _, arg := range []string{"--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox", "--always-approve", "--yolo", "--auto", "--trust-all-tools", "--trust-tools=*", "--trust-tools", "--disable-approval"} {
		if !skipsPrompts(arg) {
			t.Errorf("%s is not read as skipping the agent's permission prompts", arg)
		}
	}
	for _, arg := range []string{"--permission-mode", "auto", "acceptEdits", "--approve-for-me", "--settings", "--enable", "-c", "--model"} {
		if skipsPrompts(arg) {
			t.Errorf("%s is read as skipping the agent's permission prompts", arg)
		}
	}
}

// The strong mark is the weight of the card that approves a preset, so a
// harness file that is not built into the binary may not set it: it would
// choose which of its own presets ask for no card. The same bytes load as a
// built-in file, and a copy that sets none loads as a yard's.
func TestYardFileMayNotMarkStrong(t *testing.T) {
	t.Parallel()
	body, err := fs.ReadFile(harnesses.FS, "claude.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// auto is claude's first preset, and its own file does not mark it.
	const auto = "      description: Claude judges each action; asks only about risky ones\n"
	if !strings.Contains(string(body), auto) {
		t.Fatal("claude.yaml no longer has the auto preset this test marks")
	}
	want := "claude.yaml: flags.permissions[0].strong: only yardr's own harness files mark a preset strong"
	for _, mark := range []struct {
		value  string
		strong bool
	}{{"true", true}, {"false", false}, {"null", false}} {
		t.Run(mark.value, func(t *testing.T) {
			marked := strings.Replace(string(body), auto, auto+"      strong: "+mark.value+"\n", 1)
			files := fstest.MapFS{"claude.yaml": {Data: []byte(marked)}}
			if _, err := load(files, false); err == nil || err.Error() != want {
				t.Errorf("a yard's file that sets auto strong to %s: %v, want %q", mark.value, err, want)
			}
			hs, err := load(files, true)
			if err != nil {
				t.Fatalf("the same bytes as a built-in file: %v", err)
			}
			if len(hs) != 1 || hs[0].Flags.Permissions[0].Strong != mark.strong {
				t.Errorf("a built-in file's mark %s was not read: %+v", mark.value, hs)
			}
		})
	}
	unmarked := fstest.MapFS{"claude.yaml": {Data: []byte(strings.ReplaceAll(string(body), "      strong: true\n", ""))}}
	if _, err := load(unmarked, false); err != nil {
		t.Errorf("a yard's file that marks nothing strong: %v", err)
	}
}
