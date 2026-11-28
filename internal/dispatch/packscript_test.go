package dispatch_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/packs"
)

// When an exec script group's run ends in died or failed, the failure event
// checks a script under <home>/packs/<line>/scripts/: for an embedded line a
// differing copy says so with the --replace command and an equal copy adds
// nothing; for a non-embedded line the event names the script alone; and a
// script under <home>/scripts/ gets no extra line.
func TestExecFailureNamesDifferingPackScript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	origMerge, err := fs.ReadFile(packs.FS, "starter/scripts/merge-local")
	if err != nil {
		t.Fatal(err)
	}

	writeScript := func(t *testing.T, path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name       string
		setup      func(t *testing.T, home string) string // returns group cmd
		timeout    bool                                   // true -> session_failed via timeout, false -> session_died via reap
		wantSub    []string
		forbidSub  []string
		wantNoLine bool
	}{
		{
			name: "embedded differs on died",
			setup: func(t *testing.T, home string) string {
				p := filepath.Join(config.At(home).Pack("starter"), "scripts", "merge-local")
				writeScript(t, p, []byte("#!/bin/sh\n# yard edit\n"))
				return "YARDR_PUSH=origin " + p
			},
			wantSub: []string{
				"scripts/merge-local differs from yardr's",
				"yardr pack apply starter --depot web --replace scripts/merge-local",
			},
		},
		{
			name: "embedded differs on failed timeout",
			setup: func(t *testing.T, home string) string {
				p := filepath.Join(config.At(home).Pack("starter"), "scripts", "merge-local")
				writeScript(t, p, []byte("#!/bin/sh\n# yard edit\n"))
				return p
			},
			timeout: true,
			wantSub: []string{
				"scripts/merge-local differs from yardr's",
				"yardr pack apply starter --depot web --replace scripts/merge-local",
			},
		},
		{
			name: "embedded equal adds no line",
			setup: func(t *testing.T, home string) string {
				p := filepath.Join(config.At(home).Pack("starter"), "scripts", "merge-local")
				writeScript(t, p, origMerge)
				return p
			},
			wantNoLine: true,
		},
		{
			name: "embedded missing uses apply",
			setup: func(t *testing.T, home string) string {
				return filepath.Join(config.At(home).Pack("starter"), "scripts", "merge-local")
			},
			wantSub: []string{
				"scripts/merge-local is missing from the yard's copy",
				"yardr pack apply starter --depot web",
			},
			forbidSub: []string{"--replace"},
		},
		{
			name: "embedded non-executable uses replace",
			setup: func(t *testing.T, home string) string {
				p := filepath.Join(config.At(home).Pack("starter"), "scripts", "merge-local")
				writeScript(t, p, origMerge)
				if err := os.Chmod(p, 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantSub: []string{
				"scripts/merge-local differs from yardr's",
				"yardr pack apply starter --depot web --replace scripts/merge-local",
			},
		},
		{
			name: "non-embedded line names script alone",
			setup: func(t *testing.T, home string) string {
				p := filepath.Join(config.At(home).Pack("reports"), "scripts", "report-post")
				writeScript(t, p, []byte("#!/bin/sh\nexit 1\n"))
				return p
			},
			wantSub:   []string{"packs/reports/scripts/report-post"},
			forbidSub: []string{"differs", "--replace"},
		},
		{
			name: "home scripts gets no line",
			setup: func(t *testing.T, home string) string {
				p := filepath.Join(home, "scripts", "merge-local")
				writeScript(t, p, []byte("#!/bin/sh\n# different from binary\n"))
				return p
			},
			wantNoLine: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			s := storetest.New(t)
			r := &fakeRunner{dead: map[string]bool{}}
			now := time.Now()
			d := &dispatch.Dispatcher{
				Store:       s,
				Runners:     map[string]dispatch.Runner{"exec": r},
				Home:        home,
				Backoff:     time.Hour,
				MaxAttempts: 5,
			}
			dispatch.SetClock(d, func() time.Time { return now })

			must(t, s.PutDepot(ctx, store.Depot{Name: "web", Path: t.TempDir(), Kind: "dir"}, "me"))
			cfg := map[string]any{"cmd": tc.setup(t, home)}
			if tc.timeout {
				cfg["timeout"] = "1ns"
			}
			must(t, s.PutGroup(ctx, store.Group{Name: "web-assembly", Runner: "exec", Limit: 1, Config: cfg}, "me"))
			anyOrig := store.OriginatorAny
			_, err := s.AddRoute(ctx, store.Route{Stage: "approved", Group: "web-assembly", Originator: &anyOrig}, "me")
			must(t, err)
			bead, err := s.CreateBead(ctx, store.NewBead{Title: "land it", Stage: "approved", Depot: "web"}, "me")
			must(t, err)

			// First pass claims and starts the exec session.
			must(t, d.Reconcile(ctx))
			dispatch.WaitStartsIdle(d)
			running, err := s.ListSessions(ctx, store.SessionFilter{Running: true})
			must(t, err)
			if len(running) != 1 {
				t.Fatalf("running sessions = %d, want 1", len(running))
			}
			sessID := running[0].ID

			wantKind := "session_died"
			if tc.timeout {
				wantKind = "session_failed"
			} else {
				r.mu.Lock()
				r.dead[sessID] = true
				r.mu.Unlock()
			}
			must(t, d.Reconcile(ctx))

			evs, err := s.EventsSince(ctx, 0, 0)
			must(t, err)
			var reason string
			found := false
			for _, e := range evs {
				if e.Kind == wantKind && e.Bead != nil && *e.Bead == bead.ID {
					reason = fmt.Sprint(e.Data["reason"])
					found = true
				}
			}
			if !found {
				t.Fatalf("no %s event for %s", wantKind, bead.ID)
			}
			if tc.wantNoLine {
				if strings.Contains(reason, "\n") || strings.Contains(reason, "script") || strings.Contains(reason, "--replace") {
					t.Errorf("reason has unexpected extra line: %q", reason)
				}
				return
			}
			for _, want := range tc.wantSub {
				if !strings.Contains(reason, want) {
					t.Errorf("reason %q lacks %q", reason, want)
				}
			}
			for _, forbid := range tc.forbidSub {
				if strings.Contains(reason, forbid) {
					t.Errorf("reason %q unexpectedly contains %q", reason, forbid)
				}
			}
		})
	}
}
