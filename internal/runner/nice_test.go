package runner

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
)

// A command is run under nice only when the niceness asked for is above the
// one this process has, which the command would start with, and by the
// difference: nice(1) adds to it. A group that asks for none (0) is left
// alone, whatever this process's niceness is. The shell goes to nice by its
// path.
func TestUnderNiceWrapsByTheDifference(t *testing.T) {
	t.Parallel()
	nice, err := exec.LookPath("nice")
	if err != nil {
		t.Skip("no nice on this machine's PATH")
	}
	own, err := NiceOf(os.Getpid())
	mustT(t, err)
	up := above(own, 3)
	for _, c := range []struct {
		name   string
		want   int
		nicer  int // by how much nice is told to raise it; 0: the command is left alone
		absent bool
	}{
		{"none asked", 0, 0, false},
		{"as nice as this process", own, 0, false},
		{"nicer than this process", up, up - own, up > store.MaxNice},
		{"less nice than this process", own - 1, 0, own <= 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.absent {
				t.Skipf("nothing to show at niceness %d", own)
			}
			cmd := exec.Command("sh", "-c", "true")
			sh := cmd.Path
			mustT(t, underNice(cmd, c.want))
			if c.nicer == 0 {
				if cmd.Path != sh || strings.Join(cmd.Args, " ") != "sh -c true" {
					t.Errorf("command = %s %v, want it left as it was", cmd.Path, cmd.Args)
				}
				return
			}
			want := []string{"nice", "-n", strconv.Itoa(c.nicer), sh, "-c", "true"}
			if cmd.Path != nice || strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") {
				t.Errorf("command = %s %q, want %s %q", cmd.Path, cmd.Args, nice, want)
			}
		})
	}
}

// What nice(1) is told to add is the difference between the niceness asked for
// and the one the process has, and nothing where the group asked for none or
// for no more than the process has. The tests above read the process's own
// niceness, which none of them can lower, so they meet a start below 0 only in
// a run that began so (some CI runners start every process at -10); this table
// gives it on any machine.
func TestNiceBy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		want, own int
		by        int
	}{
		{"none asked", 0, 0, 0},
		{"none asked, process above 0", 0, 10, 0},
		{"none asked, process below 0", 0, -10, 0},
		{"as nice as the process", 5, 5, 0},
		{"less nice than the process", 3, 5, 0},
		{"nicer than the process", 8, 5, 3},
		{"nicer than a process at 0", 3, 0, 3},
		{"nicer than a process below 0", 5, -10, 15},
	} {
		if got := NiceBy(c.want, c.own); got != c.by {
			t.Errorf("%s: NiceBy(%d, %d) = %d, want %d", c.name, c.want, c.own, got, c.by)
		}
	}
}

// A group's nice runs its command, and everything the command starts, at that
// niceness from its first instruction (yardr-7a36): the shell and a child of
// it both say so. A group without the key, or with 0, starts as it did. A
// renice of the shell after its start would miss what it forks at once;
// that is why the command is started under nice.
func TestExecRunsAtTheGroupsNiceness(t *testing.T) {
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
		{"zero", map[string]any{"nice": 0}, own, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.skip {
				t.Skipf("nothing to show at niceness %d", own)
			}
			said := filepath.Join(t.TempDir(), "said")
			cfg := maps.Clone(c.cfg)
			if cfg == nil {
				cfg = map[string]any{}
			}
			cfg["cmd"] = `{ ps -o nice= -p $$; sh -c 'ps -o nice= -p $$'; } >` + said
			h, err := (&Exec{Home: filepath.Join(t.TempDir(), "home"), noReap: true}).Start(context.Background(), dispatch.Job{
				Bead:    store.Bead{ID: "b-1"},
				Session: store.Session{ID: "s-1"},
				Group:   store.Group{Name: "tick", Runner: "exec", Config: cfg},
			})
			mustT(t, err)
			pid, err := pidOf(h)
			mustT(t, err)
			_, _ = syscall.Wait4(pid, nil, 0, nil) // the command has said what it saw
			want := strconv.Itoa(c.want)
			if got := strings.Fields(string(mustRead(t, said))); len(got) != 2 || got[0] != want || got[1] != want {
				t.Errorf("the command and its child were at niceness %v, want %s for both", got, want)
			}
		})
	}
}

// A group that asks for a niceness needs nice on the daemon's PATH: without
// it no session of the group can start, and that is the runtime's failure
// and not the bead's (the group backs off), as a missing sh is. A group that
// asks for none looks for no nice.
func TestExecNiceNeedsNice(t *testing.T) {
	// Serial: sets PATH.
	own := psNice(t, os.Getpid()) // before PATH is changed: it runs ps
	ask := above(own, 1)
	if ask > store.MaxNice {
		t.Skipf("nothing to show at niceness %d", own)
	}
	sh, err := exec.LookPath("sh")
	mustT(t, err)
	bin := t.TempDir()
	mustT(t, os.Symlink(sh, filepath.Join(bin, "sh")))
	t.Setenv("PATH", bin)
	start := func(cfg map[string]any) error {
		cfg["cmd"], cfg["log"] = "exit 0", filepath.Join(t.TempDir(), "log")
		h, err := (&Exec{noReap: true}).Start(context.Background(), dispatch.Job{
			Bead:    store.Bead{ID: "b-1"},
			Session: store.Session{ID: "s-1"},
			Group:   store.Group{Name: "tick", Runner: "exec", Config: cfg},
		})
		if err == nil {
			pid, perr := pidOf(h)
			mustT(t, perr)
			_, _ = syscall.Wait4(pid, nil, 0, nil)
		}
		return err
	}
	if err := start(map[string]any{"nice": 0}); err != nil {
		t.Errorf("a group that asks for no niceness looked for nice: %v", err)
	}
	if err := start(map[string]any{"nice": ask}); err == nil || !dispatch.IsRuntime(err) || !strings.Contains(err.Error(), `"nice"`) {
		t.Errorf("a group that asks for a niceness, with no nice to run its command under, started: %v; want the runtime's failure naming nice", err)
	}
}
