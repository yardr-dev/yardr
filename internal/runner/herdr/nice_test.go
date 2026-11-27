package herdr

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/runner"
	"yardr.dev/yardr/internal/store"
)

// paneShell is a stand-in for the shell of a herdr pane, run under nice by
// extra as a herdr started at a niceness would have it: a process that has
// started nothing yet, whose pid a test gives the fake herdr as the pane's
// shell. It is handed out once it has run a command of its own. A renice
// that lands in the few milliseconds after a process starts is undone by
// the exec it is finishing (seen on macOS: lost in half the tries 2 ms after
// the start, never after 10), and a wrapper such as nice has not set its
// niceness before then either. yardr asks for the pid of a pane's shell
// long after herdr started it, but a test that starts its own shell and
// uses it at once is not. release lets the shell go on to start a child,
// which says its niceness, and returns that once the shell has ended.
func paneShell(t *testing.T, extra int) (pid int, release func() int) {
	t.Helper()
	dir := t.TempDir()
	out, up := filepath.Join(dir, "child"), filepath.Join(dir, "up")
	argv := []string{"sh", "-c", `echo up >"$1"; read go; sh -c 'ps -o nice= -p $$' >"$0"`, out, up}
	if extra > 0 {
		argv = append([]string{"nice", "-n", strconv.Itoa(extra)}, argv...)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	mustT(t, err)
	mustT(t, cmd.Start())
	ended := make(chan struct{})
	go func() { _ = cmd.Wait(); close(ended) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-ended })
	waitFile(t, up)
	return cmd.Process.Pid, func() int {
		t.Helper()
		_, err := io.WriteString(stdin, "go\n")
		mustT(t, err)
		<-ended
		n, err := strconv.Atoi(strings.TrimSpace(string(mustRead(t, out))))
		if err != nil {
			t.Fatalf("the shell's child said %q (%v)", mustRead(t, out), err)
		}
		return n
	}
}

// The shell of a pane is set to the niceness the group asks for before the
// agent is started in it, and what the shell starts then carries it
// (yardr-7a36). One that is that nice already is left as it is: a process
// cannot lower its own niceness, and the group asked for no more than that.
// A group without the key asks nothing. Every niceness is told from this
// process's own, which the gate may itself have been started at.
func TestNiceShell(t *testing.T) {
	t.Parallel()
	own := psNice(t, os.Getpid())
	for _, c := range []struct {
		name   string
		extra  int                 // the shell starts this much nicer than this process
		ask    func(shell int) int // what the group asks for, from the niceness the shell starts at
		raised bool                // the shell ends at what is asked, and not where it was
	}{
		{"raised to what is asked", 0, func(shell int) int { return above(shell, 5) }, true},
		{"nicer than asked already", 15, func(shell int) int { return shell - 5 }, false},
		{"asked for the niceness it has", 0, func(shell int) int { return shell }, false},
		{"no niceness asked", 0, func(int) int { return 0 }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.raised && above(own, 5) > store.MaxNice {
				t.Skipf("no niceness a group can ask for is nicer than %d", own)
			}
			pid, release := paneShell(t, c.extra)
			// What nice(1) made of the shell's niceness is the system's to
			// say (macOS goes to 20 where Linux stops at 19), so it is read.
			before := psNice(t, pid)
			if got, err := runner.NiceOf(pid); err != nil || got != before {
				t.Fatalf("NiceOf(%d) = %d, %v; ps says %d", pid, got, err, before)
			}
			want, after := c.ask(before), before
			if c.raised {
				after = want
			}
			if err := niceShell(map[string]any{"shell_pid": pid}, want); err != nil {
				t.Fatal(err)
			}
			if got := psNice(t, pid); got != after {
				t.Errorf("shell at niceness %d after asking for %d, want %d (it was %d)", got, want, after, before)
			}
			if got := release(); got != after {
				t.Errorf("a child of the shell is at niceness %d, want its shell's %d", got, after)
			}
		})
	}

	// A pane whose shell herdr did not name has none to set; a group that
	// asks nothing needs none.
	if err := niceShell(map[string]any{}, 5); err == nil || !strings.Contains(err.Error(), "did not report the pane shell") {
		t.Errorf("niceShell with no shell = %v, want it said that herdr did not report one", err)
	}
	if err := niceShell(map[string]any{}, 0); err != nil {
		t.Errorf("niceShell with no shell and no niceness asked = %v, want nothing", err)
	}
}

// A herdr group's nice reaches the shell of the pane the agent is started in,
// and is there when herdr is asked to start the agent: the agent and all it
// starts descend from that shell (yardr-7a36). The fake herdr says the
// shell's niceness as it is asked to. Start and startDir both end in the
// brief that does this, so one of them stands for both.
func TestHerdrStartNicesTheShellBeforeTheAgent(t *testing.T) {
	t.Parallel()
	own := psNice(t, os.Getpid())
	raised := min(above(own, 5), store.MaxNice)
	for _, c := range []struct {
		name string
		cfg  map[string]any
		want int
		skip bool
	}{
		{"asked", map[string]any{"nice": raised}, raised, raised <= own},
		{"not set", nil, own, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.skip {
				t.Skipf("nothing to show at niceness %d", own)
			}
			wt := seedEmptyRepo(t)
			log := filepath.Join(t.TempDir(), "calls")
			shell, release := paneShell(t, 0)
			h := &Herdr{YardHome: t.TempDir(), YardBin: "/bin/yardr", Socket: fakeHerdrAPI(t, log), Bin: fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "worktree create") echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"`+wt+`"}}}' ;;
  "pane process-info") echo '{"result":{"process_info":{"pane_id":"p2","shell_pid":`+fmt.Sprint(shell)+`}}}' ;;
  "agent start") echo the shell is at niceness $(ps -o nice= -p `+fmt.Sprint(shell)+`) >> `+log+`; echo '{"result":{"type":"ok"}}' ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)}
			job := dispatch.Job{
				Bead:    store.Bead{ID: "yard-13", Stage: "new"},
				Group:   store.Group{Name: "yard-builders", Config: c.cfg},
				Depot:   &store.Depot{Name: "yard", Path: t.TempDir(), Base: "main"},
				Session: store.Session{ID: "s1"},
			}
			if _, err := h.Start(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			calls := strings.Split(string(mustRead(t, log)), "\n")
			start := slices.IndexFunc(calls, func(l string) bool { return strings.HasPrefix(l, "agent start yard-13-new ") })
			if want := fmt.Sprintf("the shell is at niceness %d", c.want); start < 0 || calls[start+1] != want {
				t.Errorf("herdr was not asked to start the agent with its shell at niceness %d:\n%s", c.want, strings.Join(calls, "\n"))
			}
			if got := release(); got != c.want {
				t.Errorf("a child of the shell is at niceness %d, want %d", got, c.want)
			}
		})
	}
}
