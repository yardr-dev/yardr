package herdr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/runner"
	"yardr.dev/yardr/internal/store"
)

// A runner does not start work when it cannot install the hook that gives
// session commits their trailers.
func TestRunnerDoesNotStartWithoutCommitHook(t *testing.T) {
	t.Parallel()
	home := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(home, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "started")
	job := dispatch.Job{
		Bead:  store.Bead{ID: "web-3", Type: "task", Stage: "new"},
		Group: store.Group{Name: "builders", Config: map[string]any{"cmd": "touch " + marker}},
	}
	if _, err := (&runner.Exec{Home: home}).Start(t.Context(), job); err == nil ||
		!strings.Contains(err.Error(), "install yard commit-msg hook") {
		t.Fatalf("exec start error = %v, want a commit-hook install error", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("exec command started without its hook: %v", err)
	}

	job.Depot = &store.Depot{Name: "web", Path: t.TempDir(), Kind: store.DepotGit}
	if _, err := (&Herdr{YardHome: home}).Start(t.Context(), job); err == nil ||
		!strings.Contains(err.Error(), "install yard commit-msg hook") {
		t.Fatalf("herdr start error = %v, want a commit-hook install error", err)
	}
}
