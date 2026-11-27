package cli

import (
	"log/slog"
	"maps"
	"slices"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/runner"
	"yardr.dev/yardr/internal/runner/herdr"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// builtinRunners is the one place the runner list is built: each name a
// group can refer to gets its runner, wired to the yard it was built for, and
// the daemon and yardr down get the same list.
func TestBuiltinRunners(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	st := storetest.New(t)
	rs := builtinRunners(home, st, slog.New(slog.DiscardHandler), func(error) {})
	want := []string{"exec", "herdr", "manual", store.PeerRunner}
	if got := slices.Sorted(maps.Keys(rs)); !slices.Equal(got, want) {
		t.Fatalf("runners %v, want %v", got, want)
	}
	self := selfPath()
	if self == "" {
		t.Fatal("this process has no executable path to give the runners")
	}
	// wired says what is wrong with a runner's fields, one message for each.
	wired := func(name string, fields map[string]bool) {
		t.Helper()
		for field, ok := range fields {
			if !ok {
				t.Errorf("%s runner: %s is not this yard's", name, field)
			}
		}
	}
	if m, ok := rs["manual"].(*runner.Manual); !ok {
		t.Errorf("manual is a %T, want a *runner.Manual", rs["manual"])
	} else {
		wired("manual", map[string]bool{"Store": m.Store == st, "Show (it can show a notification)": m.Show != nil})
	}
	if e, ok := rs["exec"].(*runner.Exec); !ok {
		t.Errorf("exec is a %T, want a *runner.Exec", rs["exec"])
	} else {
		wired("exec", map[string]bool{"Home": e.Home == home, "Bin (this binary)": e.Bin == self})
	}
	if h, ok := rs["herdr"].(*herdr.Herdr); !ok {
		t.Errorf("herdr is a %T, want a *herdr.Herdr", rs["herdr"])
	} else {
		wired("herdr", map[string]bool{"Bin (herdr)": h.Bin == "herdr", "YardHome": h.YardHome == home, "YardBin (this binary)": h.YardBin == self,
			"WorktreeRoot": h.WorktreeRoot == config.At(home).Worktrees(), "Store": h.Store == st, "OnError": h.OnError != nil})
	}
	if p, ok := rs[store.PeerRunner].(*runner.Peer); !ok {
		t.Errorf("peer is a %T, want a *runner.Peer", rs[store.PeerRunner])
	} else {
		wired("peer", map[string]bool{"Store": p.Store == st, "Home": p.Home == home})
	}
	// The seams the daemon and yardr down go through give that list.
	if got := slices.Sorted(maps.Keys(daemonRunners(home, st, nil, nil))); !slices.Equal(got, want) {
		t.Errorf("the daemon's runners %v, want %v", got, want)
	}
	if got := slices.Sorted(maps.Keys(yardRunners(home, st))); !slices.Equal(got, want) {
		t.Errorf("yardr down's runners %v, want %v", got, want)
	}
}
