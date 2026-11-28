package config

import (
	"slices"
	"testing"
)

func TestScriptOfAndSplitCmd(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cmd     string
		assigns []string
		script  string
		alone   bool
	}{
		{"", nil, "", false},
		{"A=1 B=2", []string{"A=1", "B=2"}, "", false},
		{"/home/packs/starter/scripts/merge-local", nil, "/home/packs/starter/scripts/merge-local", true},
		{"YARDR_PUSH=origin /home/packs/starter/scripts/merge-local", []string{"YARDR_PUSH=origin"}, "/home/packs/starter/scripts/merge-local", false},
		{"'/my dir/packs/starter/scripts/merge-local' --fast", nil, "/my dir/packs/starter/scripts/merge-local", false},
		{`A="x y" '/my dir/it'\''s/scripts/land'`, []string{`A="x y"`}, "/my dir/it's/scripts/land", false},
	} {
		assigns, _, _ := SplitCmd(c.cmd)
		if !slices.Equal(assigns, c.assigns) {
			t.Errorf("SplitCmd(%q) assigns = %q, want %q", c.cmd, assigns, c.assigns)
		}
		got, alone := ScriptOf(c.cmd)
		if got != c.script || alone != c.alone {
			t.Errorf("ScriptOf(%q) = (%q, %v), want (%q, %v)", c.cmd, got, alone, c.script, c.alone)
		}
	}
}
