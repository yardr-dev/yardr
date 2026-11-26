package main

import (
	"os"
	"path/filepath"
	"testing"
)

// holdAgentStart goes on when the test renames agent-hold to agent-released
// and fails the start when the hold only goes. The rename is one step, but the
// poll reads the release and then the hold, so it can fall between the two
// reads: neither file is there, and only a second look at the release tells
// the rename from a hold that went. No timing lands there on purpose, so each
// row does it: stat is what the poll reads with, and it changes the state dir
// right after the first read of the release finds it missing.
func TestHoldAgentStart(t *testing.T) {
	// Sequential: it points the package's state and stat at a directory of its own.
	for _, c := range []struct {
		name string
		// between is what the test does to the held start, in the dir the
		// state names, after the poll's first read of the release.
		between func(hold, released string) error
		want    bool
	}{
		{"the rename falls between the two reads", os.Rename, true},
		{"the hold only goes", func(hold, _ string) error { return os.Remove(hold) }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			oldState, oldStat := state, stat
			t.Cleanup(func() { state, stat = oldState, oldStat })
			state = t.TempDir()
			hold, released := filepath.Join(state, "agent-hold"), filepath.Join(state, "agent-released")
			if err := os.WriteFile(hold, []byte("held\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			done := false
			stat = func(name string) (os.FileInfo, error) {
				fi, err := os.Stat(name)
				if err != nil && name == released && !done {
					done = true
					if err := c.between(hold, released); err != nil {
						t.Fatal(err)
					}
				}
				return fi, err
			}
			if got := holdAgentStart(); got != c.want {
				t.Errorf("holdAgentStart() = %v, want %v", got, c.want)
			}
			if !done {
				t.Error("the poll never read the release, so the row did not run")
			}
		})
	}
}
