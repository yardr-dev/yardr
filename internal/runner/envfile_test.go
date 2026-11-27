package runner

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
)

// An exec session's command inherits the daemon's environment, but not the
// settings of the yard's server: the assembly's tests ran with the daemon's
// YARDR_HOOK_ADDR, and a server its tests started listened there
// (yardr-t9n1). A dir depot's bead, whose inherited environment is cut
// further, is no exception. What a test binary names itself with
// (YARDR_TEST_OWNER, and the tick the server watches on) is kept: a script
// the exec runner starts for a test gets that environment, and the server its
// yardr starts must stop with the test (yardr-g600). So is the wait that
// yardr gives the server to start, which is the test's too (yardr-5jd7).
func TestExecSessionsGetNoServerSettings(t *testing.T) {
	// Serial: sets the daemon's environment.
	for _, n := range serverSettingNames {
		t.Setenv(n, "the-daemons")
	}
	t.Setenv("YARDR_T9N1_KEPT", "kept")
	t.Setenv("YARDR_TEST_OWNER", "4242 987654")
	t.Setenv("YARDR_TEST_HOME_CHECK", "1s")
	t.Setenv("YARDR_TEST_START_WAIT", "90s")
	dir := t.TempDir()
	for name, depot := range map[string]*store.Depot{
		"git": {Name: "web", Path: dir, Base: "main"},
		"dir": {Name: "docs", Path: dir, Kind: store.DepotDir},
	} {
		out := filepath.Join(dir, name+".env")
		job := dispatch.Job{
			Bead: store.Bead{ID: "yd-7", Stage: "approved"},
			Group: store.Group{Name: "assembly", Config: map[string]any{"cmd": "env > " + out + ".tmp && mv " + out + ".tmp " + out,
				"log": filepath.Join(dir, "log")}},
			Depot:   depot,
			Session: store.Session{ID: "s1"},
		}
		if _, err := (&Exec{Home: t.TempDir(), Bin: "/bin/yardr"}).Start(context.Background(), job); err != nil {
			t.Fatal(err)
		}
		var got string
		for deadline := time.Now().Add(5 * time.Second); got == "" && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			b, _ := os.ReadFile(out)
			got = string(b)
		}
		for _, want := range []string{"YARDR_T9N1_KEPT=kept\n", "YARDR_BEAD=yd-7\n", "YARDR_TEST_OWNER=4242 987654\n", "YARDR_TEST_HOME_CHECK=1s\n", "YARDR_TEST_START_WAIT=90s\n"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s depot: the command's environment lacks %q:\n%s", name, strings.TrimSuffix(want, "\n"), got)
			}
		}
		for _, n := range serverSettingNames {
			if strings.Contains(got, n+"=") {
				t.Errorf("%s depot: the command got %s:\n%s", name, n, got)
			}
		}
	}
}

// execEnvJob is a job of an exec group whose command writes its environment
// to the returned file, whole once it is there.
func execEnvJob(t *testing.T, depot *store.Depot, envFile string) (dispatch.Job, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "env")
	return dispatch.Job{
		Bead: store.Bead{ID: "yd-7", Stage: "deploy"},
		Group: store.Group{Name: "deployers", Config: map[string]any{"cmd": "env > " + out + ".tmp && mv " + out + ".tmp " + out,
			"log": filepath.Join(dir, "log"), "env_file": envFile}},
		Depot:   depot,
		Session: store.Session{ID: "s1"},
	}, out
}

// An exec group's command gets its env_file's entries, as a herdr group's
// agent does: over what the daemon was itself started with, but not over a
// key yardr sets, and only the key names reach the log.
func TestExecStartLoadsGroupEnvFile(t *testing.T) {
	// Serial: sets the daemon's environment.
	t.Setenv("LINEAR_API_KEY", "the-daemons")
	var logs bytes.Buffer
	e := &Exec{Home: t.TempDir(), Bin: "/bin/yardr",
		Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	job, out := execEnvJob(t, &store.Depot{Name: "web", Path: t.TempDir(), Base: "main"}, envFileT(t, 0o600))
	handle, err := e.Start(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	got := waitFile(t, out)
	if !strings.Contains(got, "LINEAR_API_KEY="+envSecret+"\n") || strings.Contains(got, "the-daemons") {
		t.Errorf("the command lacks the env file's entry, or has the daemon's:\n%s", got)
	}
	if !strings.Contains(got, "YARDR_ACTOR=deployers\n") || strings.Contains(got, "someone-else") {
		t.Errorf("env file overrode the identity:\n%s", got)
	}
	if h := fmt.Sprint(handle); strings.Contains(h, envSecret) {
		t.Errorf("handle holds the value: %s", h)
	}
	if l := logs.String(); strings.Contains(l, envSecret) || !strings.Contains(l, "LINEAR_API_KEY") || !strings.Contains(l, "group deployers") {
		t.Errorf("the log shows the value, or lacks the key names:\n%s", l)
	}
	if raw, _ := os.ReadFile(job.Group.Config["log"].(string)); strings.Contains(string(raw), envSecret) {
		t.Errorf("the session's log shows the value:\n%s", raw)
	}
}

// An env_file gives an exec command nothing the daemon's own environment
// may not: no setting of the yard's server, no test command for a depot with
// none, and for a dir depot's bead no base, branch or worktree.
func TestExecEnvFileGivesNoWithheldVariable(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "deploy.env")
	body := "DOKKU_KEY_FILE=/k\nYARDR_TEST=rm -rf\nYARDR_BASE=theirs\nYARDR_BRANCH=theirs\nYARDR_WORKTREE=/theirs\n"
	for _, n := range serverSettingNames {
		body += n + "=theirs\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	job, out := execEnvJob(t, &store.Depot{Name: "docs", Path: t.TempDir(), Kind: store.DepotDir}, p)
	if _, err := (&Exec{Home: t.TempDir(), Bin: "/bin/yardr"}).Start(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	got := waitFile(t, out)
	if !strings.Contains(got, "DOKKU_KEY_FILE=/k\n") {
		t.Fatalf("the command lacks the env file's entry:\n%s", got)
	}
	if strings.Contains(got, "theirs") || strings.Contains(got, "YARDR_TEST=") {
		t.Errorf("the env file gave a variable the command must not have:\n%s", got)
	}
}

// A file others can read, or none at all, refuses an exec group's start as
// Blocked, as a herdr group's is, before the command runs or its log is
// made, and the error shows no value.
func TestExecEnvFileRefusedBlocksStart(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{"0644": envFileT(t, 0o644), "0640": envFileT(t, 0o640),
		"missing": filepath.Join(t.TempDir(), "nope.env"), "directory": t.TempDir()} {
		var logs bytes.Buffer
		e := &Exec{Home: t.TempDir(), Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
		job, out := execEnvJob(t, &store.Depot{Name: "web", Path: t.TempDir(), Base: "main"}, path)
		handle, err := e.Start(context.Background(), job)
		if !dispatch.IsBlocked(err) {
			t.Errorf("%s: err = %v, want Blocked", name, err)
		} else if strings.Contains(err.Error(), envSecret) || !strings.Contains(err.Error(), "env_file") ||
			!strings.Contains(err.Error(), "group deployers") {
			t.Errorf("%s: error %q", name, err)
		}
		if handle != nil {
			t.Errorf("%s: a refused start left a handle: %v", name, handle)
		}
		// The command would have written out at once; give it a moment.
		time.Sleep(50 * time.Millisecond)
		for _, f := range []string{out, job.Group.Config["log"].(string)} {
			if _, err := os.Stat(f); err == nil {
				t.Errorf("%s: %s was made despite the refusal", name, f)
			}
		}
		if strings.Contains(logs.String(), envSecret) {
			t.Errorf("%s: logs show the value", name)
		}
	}
}
