package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/testload"
)

// generalYard is a carry yard with general cards on.
func generalCarrierYard(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	st, home, bead := carryYard(t)
	if err := st.SetSetting(context.Background(), store.GeneralCardsSetting, "true", "mckean"); err != nil {
		t.Fatal(err)
	}
	return st, home, bead
}

func generalAskOf(words ...string) store.Ask {
	return store.Ask{Question: "Run it?", To: "mckean", Context: []string{"no card fits it"},
		Run: &store.AskRun{General: words}}
}

// generalScript stands in for the yard's binary: body runs with the
// re-exec's argv, directory, environment and closed stdin.
func generalScript(t *testing.T, body string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "yardr-standin")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := executable
	executable = func() (string, error) { return script, nil }
	t.Cleanup(func() { executable = old })
}

// An approved general card is carried out once, as typed: the binary
// with the home and the approver, the words in order, from the home,
// with exactly the five yardr variables dropped and stdin closed.
func TestCarryGeneralRunsAsTyped(t *testing.T) {
	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	generalScript(t, "printf 'args:%s\\ndir:%s\\nkeep:%s\\nsession:%s bead:%s actor:%s home:%s crew:%s card:%s\\nstdin:[%s]\\n' \"$*\" \"$(pwd)\" \"${GENERAL_KEEP-}\" \"${YARDR_SESSION-UNSET}\" \"${YARDR_BEAD-UNSET}\" \"${YARDR_ACTOR-UNSET}\" \"${YARDR_HOME-UNSET}\" \"${YARDR_CREW-UNSET}\" \"${YARDR_GENERAL_CARD-UNSET}\" \"$(cat)\"\n")
	t.Setenv("GENERAL_KEEP", "kept")
	t.Setenv("YARDR_SESSION", "pane-1")
	t.Setenv("YARDR_BEAD", "yardr-other")
	t.Setenv("YARDR_ACTOR", "yardmaster")
	t.Setenv("YARDR_HOME", "/elsewhere")
	t.Setenv("YARDR_CREW", "yardmaster")
	seq, err := st.AddAsk(ctx, bead, generalAskOf("bead", "create", "Made", "by", "card"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	c := testCarrier(st, home, "boot-one")
	if did, err := c.carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	rec := carryState(t, st, bead, seq)
	if rec.State != store.CarryDone {
		t.Fatalf("the carrying record = %+v", rec)
	}
	dir, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"args:--home " + home + " --actor mckean bead create Made by card",
		"dir:" + dir,
		"keep:kept",
		"session:UNSET bead:UNSET actor:UNSET home:UNSET crew:UNSET card:1",
		"stdin:[]",
	} {
		if !strings.Contains(rec.Result, want) {
			t.Errorf("what ran does not hold %q:\n%s", want, rec.Result)
		}
	}
	if lines := resolvedLines(t, st, bead, seq); !strings.Contains(lines, "args:--home") {
		t.Errorf("the card does not read what ran:\n%s", lines)
	}
	// The commands whose card was deleted ride the same way, flags among
	// their words and all: a flagged one, a used one and a device's.
	for _, words := range [][]string{
		{"bead", "update", bead, "--originator", "ada"},
		{"route", "add", "--stage", "review", "--group", "reviewers", "--depot", "web"},
		{"user", "device", "rm", "ada", "phone"},
	} {
		seq, err := st.AddAsk(ctx, bead, generalAskOf(words...), "yardmaster")
		if err != nil {
			t.Fatalf("filing %q: %v", words, err)
		}
		if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
			t.Fatal(err)
		}
		if did, err := c.carryNext(ctx); err != nil || !did {
			t.Fatalf("carryNext of %q = %v, %v", words, did, err)
		}
		rec := carryState(t, st, bead, seq)
		if want := "args:--home " + home + " --actor mckean " + strings.Join(words, " "); rec.State != store.CarryDone || !strings.Contains(rec.Result, want) {
			t.Errorf("%q carried as %+v, want it done with %q", words, rec, want)
		}
	}
}

// The dropped list is exactly the five, and nothing else goes.
func TestGeneralEnvDropsExactlyFive(t *testing.T) {
	in := []string{"PATH=/bin", "YARDR_SESSION=s", "YARDR_BEAD=b", "YARDR_ACTOR=a", "YARDR_HOME=h", "YARDR_CREW=c", "YARDR_OTHER=o", "HOME=/home"}
	want := []string{"PATH=/bin", "YARDR_OTHER=o", "HOME=/home"}
	if got := scrubbedGeneralEnv(in); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("scrubbedGeneralEnv = %v, want %v", got, want)
	}
	if len(generalDropped) != 5 {
		t.Errorf("generalDropped = %v, want the five", generalDropped)
	}
}

// A general command that fails fails the carry with its stderr tail
// and the terminal line; one past the cap names the cap.
func TestCarryGeneralFailures(t *testing.T) {
	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	generalScript(t, "echo went-wrong >&2\nexit 3\n")
	seq, err := st.AddAsk(ctx, bead, generalAskOf("bead", "create", "Nope"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	c := testCarrier(st, home, "boot-one")
	if did, err := c.carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	rec := carryState(t, st, bead, seq)
	if rec.State != store.CarryFailed || !strings.Contains(rec.Result, "went-wrong") ||
		!strings.Contains(rec.Result, "Finish it with yardr bead create Nope in a terminal") {
		t.Fatalf("the carrying record = %+v", rec)
	}

	st2, home2, bead2 := generalCarrierYard(t)
	generalScript(t, "head -c 70000 /dev/zero | tr '\\0' a\n")
	seq2, err := st2.AddAsk(ctx, bead2, generalAskOf("bead", "create", "Loud"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st2.ApproveAsk(ctx, bead2, seq2, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	if did, err := testCarrier(st2, home2, "boot-one").carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	if rec := carryState(t, st2, bead2, seq2); rec.State != store.CarryFailed ||
		!strings.Contains(rec.Result, "printed more than 65536 bytes") {
		t.Fatalf("the loud carrying record = %+v", rec)
	}

	// A tool install whose launch fails (executable error or missing
	// binary) fails the card with failWords.
	for _, tc := range []struct {
		name string
		exec func() (string, error)
		want string
	}{
		{"executable error", func() (string, error) { return "", errors.New("no binary here") }, "no binary here"},
		{"spawn error", func() (string, error) { return filepath.Join(t.TempDir(), "missing-yardr"), nil }, "missing-yardr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st3, home3, bead3 := generalCarrierYard(t)
			old := executable
			executable = tc.exec
			t.Cleanup(func() { executable = old })
			seq3, err := st3.AddAsk(ctx, bead3, generalAskOf("pack", "install", "l", "--tool", "t", "--", "sh", "-c", "x"), "yardmaster")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st3.ApproveAsk(ctx, bead3, seq3, "mckean", true, nil); err != nil {
				t.Fatal(err)
			}
			if did, err := testCarrier(st3, home3, "boot-one").carryNext(ctx); err != nil || !did {
				t.Fatalf("carryNext = %v, %v", did, err)
			}
			rec := carryState(t, st3, bead3, seq3)
			if rec.State != store.CarryFailed || !strings.Contains(rec.Result, tc.want) ||
				!strings.Contains(rec.Result, "Finish it with yardr pack install l --tool t -- sh -c x in a terminal") {
				t.Fatalf("failed launch record = %+v", rec)
			}
		})
	}
}

// A full cap of output still completes: the answer note holds it
// whole, under no local note bound but the cap itself.
func TestCarryGeneralHoldsFullCap(t *testing.T) {
	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	generalScript(t, "head -c 65536 /dev/zero | tr '\\0' a\n")
	seq, err := st.AddAsk(ctx, bead, generalAskOf("bead", "create", "Full"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	if did, err := testCarrier(st, home, "boot-one").carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	if rec := carryState(t, st, bead, seq); rec.State != store.CarryDone || len(rec.Result) != 65536 {
		t.Fatalf("the full-cap record = state %s, %d bytes", rec.State, len(rec.Result))
	}
}

// Past its timeout the group is killed and the carry fails with the
// timeout's words, in the test's lowered bound.
func TestCarryGeneralTimeoutKillsGroup(t *testing.T) {
	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	generalScript(t, "sleep 30\n")
	old := generalTimeout
	generalTimeout = 200 * time.Millisecond
	t.Cleanup(func() { generalTimeout = old })
	seq, err := st.AddAsk(ctx, bead, generalAskOf("bead", "create", "Slow"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if did, err := testCarrier(st, home, "boot-one").carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Fatalf("the carry took %v: the group was not killed at the timeout", took)
	}
	if rec := carryState(t, st, bead, seq); rec.State != store.CarryFailed ||
		!strings.Contains(rec.Result, "no answer within") {
		t.Fatalf("the timed-out record = %+v", rec)
	}
}

// A general setup takes the general leg, never the setup carry, and a
// general carry completes with the apply lock held elsewhere.
func TestCarryGeneralLegAndNoLock(t *testing.T) {
	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	generalScript(t, "echo carried\n")
	seq, err := st.AddAsk(ctx, bead, generalAskOf("setup", "x"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	release, err := config.AcquireApplyLock(home)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if did, err := testCarrier(st, home, "boot-one").carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext with the lock held = %v, %v", did, err)
	}
	if rec := carryState(t, st, bead, seq); rec.State != store.CarryDone || !strings.Contains(rec.Result, "carried") {
		t.Fatalf("the carrying record = %+v", rec)
	}
}

// A general tool install launches detached with --bead inserted before
// --, completes at once with the started words so a following ordinary
// general card runs while the install still sleeps, and finishes its
// work afterwards.
func TestCarryGeneralToolInstallDetached(t *testing.T) {
	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "install.pid")
	outFile := filepath.Join(tmp, "install.out")
	t.Setenv("TEST_INSTALL_PID", pidFile)
	t.Setenv("TEST_INSTALL_OUT", outFile)
	t.Setenv("GENERAL_KEEP", "kept")
	t.Setenv("YARDR_SESSION", "pane-1")
	t.Setenv("YARDR_BEAD", "yardr-other")
	t.Setenv("YARDR_ACTOR", "yardmaster")
	t.Setenv("YARDR_HOME", "/elsewhere")
	t.Setenv("YARDR_CREW", "yardmaster")
	generalScript(t, `case " $* " in
  *" --tool "*)
    echo $$ > "$TEST_INSTALL_PID"
    sleep 5
    printf 'args:%s\ndir:%s\nkeep:%s\nsession:%s bead:%s actor:%s home:%s crew:%s card:%s\n' \
      "$*" "$(pwd)" "${GENERAL_KEEP-}" "${YARDR_SESSION-UNSET}" "${YARDR_BEAD-UNSET}" \
      "${YARDR_ACTOR-UNSET}" "${YARDR_HOME-UNSET}" "${YARDR_CREW-UNSET}" "${YARDR_GENERAL_CARD-UNSET}" \
      > "$TEST_INSTALL_OUT.tmp"
    mv "$TEST_INSTALL_OUT.tmp" "$TEST_INSTALL_OUT"
    ;;
  *)
    printf 'ordinary:%s\n' "$*"
    ;;
esac
`)
	t.Cleanup(func() {
		if _, err := os.Stat(outFile); err == nil {
			return
		}
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}
	})

	seq1, err := st.AddAsk(ctx, bead, generalAskOf("pack", "install", "l", "--tool", "t", "--", "sh", "-c", "x"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq1, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	c := testCarrier(st, home, "boot-one")
	began := time.Now()
	if did, err := c.carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	if took := time.Since(began); took >= time.Second {
		t.Fatalf("detached tool install carry took %v, want under 1s", took)
	}
	if _, err := os.Stat(outFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install output appeared before sleep finished: %v", err)
	}
	rec1 := carryState(t, st, bead, seq1)
	wantResult := fmt.Sprintf("started yardr pack install l --tool t -- sh -c x; the result is noted on %s within 15 minutes", bead)
	if rec1.State != store.CarryDone || rec1.Result != wantResult {
		t.Fatalf("tool install carry = %+v, want done with %q", rec1, wantResult)
	}

	// A second, ordinary general record approved after it is carried
	// while the first stand-in still sleeps.
	seq2, err := st.AddAsk(ctx, bead, generalAskOf("bead", "create", "While", "sleeping"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq2, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	if did, err := c.carryNext(ctx); err != nil || !did {
		t.Fatalf("second carryNext = %v, %v", did, err)
	}
	if _, err := os.Stat(outFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install output appeared before second carry finished: %v", err)
	}
	rec2 := carryState(t, st, bead, seq2)
	if rec2.State != store.CarryDone || !strings.Contains(rec2.Result, "ordinary:--home "+home+" --actor mckean bead create While sleeping") {
		t.Fatalf("ordinary carry = %+v", rec2)
	}

	// The detached install finishes afterwards and wrote what it got.
	deadline := time.Now().Add(testload.Scale(10 * time.Second))
	var got []byte
	for {
		b, err := os.ReadFile(outFile)
		if err == nil {
			got = b
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("detached install never wrote %s: %v", outFile, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	dir, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	for _, want := range []string{
		"args:--home " + home + " --actor mckean pack install l --tool t --bead " + bead + " -- sh -c x",
		"dir:" + dir,
		"keep:kept",
		"session:UNSET bead:UNSET actor:UNSET home:UNSET crew:UNSET card:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detached install did not hold %q:\n%s", want, out)
		}
	}
}

// isToolInstall matches only `pack install ... --tool ...` with --tool
// before any --, withBead puts --bead just before the first --, and a
// pack install without --tool (or with --tool only after --) still runs
// in the lane under generalTimeout.
func TestIsToolInstallAndNonToolStayInLane(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want bool
	}{
		{[]string{"pack", "install", "l", "--tool", "t", "--", "sh", "-c", "x"}, true},
		{[]string{"pack", "install", "l", "--tool=t", "--", "sh", "-c", "x"}, true},
		{[]string{"pack", "install", "--tool", "t", "l", "--", "sh", "-c", "x"}, true},
		{[]string{"pack", "install", "l", "--tool", "t"}, true},
		{[]string{"pack", "install", "l"}, false},
		{[]string{"pack", "install", "l", "--", "--tool", "t"}, false},
		{[]string{"pack", "install", "l", "--", "--tool=t"}, false},
		{[]string{"pack", "install", "l", "--toolkit", "t", "--", "sh"}, false},
		{[]string{"pack", "apply", "l", "--tool", "t", "--", "sh"}, false},
		{[]string{"pack", "install"}, false},
		{nil, false},
	} {
		if got := isToolInstall(tc.argv); got != tc.want {
			t.Errorf("isToolInstall(%q) = %v, want %v", tc.argv, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in, want []string
	}{
		{
			[]string{"pack", "install", "l", "--tool", "t", "--", "sh", "-c", "x"},
			[]string{"pack", "install", "l", "--tool", "t", "--bead", "yardr-1234", "--", "sh", "-c", "x"},
		},
		{
			[]string{"pack", "install", "l", "--tool=t", "--", "sh", "--", "x"},
			[]string{"pack", "install", "l", "--tool=t", "--bead", "yardr-1234", "--", "sh", "--", "x"},
		},
		{
			[]string{"pack", "install", "l", "--tool", "t"},
			[]string{"pack", "install", "l", "--tool", "t", "--bead", "yardr-1234"},
		},
	} {
		if got := withBead(tc.in, "yardr-1234"); !slices.Equal(got, tc.want) {
			t.Errorf("withBead(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	ctx := context.Background()
	st, home, bead := generalCarrierYard(t)
	generalScript(t, "sleep 30\n")
	old := generalTimeout
	generalTimeout = 200 * time.Millisecond
	t.Cleanup(func() { generalTimeout = old })
	c := testCarrier(st, home, "boot-one")

	// --tool only after -- runs in the lane and hits generalTimeout.
	seq, err := st.AddAsk(ctx, bead, generalAskOf("pack", "install", "l", "--", "--tool", "t"), "yardmaster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveAsk(ctx, bead, seq, "mckean", true, nil); err != nil {
		t.Fatal(err)
	}
	if did, err := c.carryNext(ctx); err != nil || !did {
		t.Fatalf("carryNext = %v, %v", did, err)
	}
	if rec := carryState(t, st, bead, seq); rec.State != store.CarryFailed || !strings.Contains(rec.Result, "no answer within") {
		t.Fatalf("pack install with --tool after -- = %+v, want in-lane timeout", rec)
	}

	// pack install without --tool also runs in the lane under generalTimeout.
	res, failure := c.carryGeneral(ctx, &store.Carry{
		Bead: bead, Ask: seq + 1, Actor: "mckean", General: true,
		Argv: []string{"pack", "install", "l"},
	})
	if res != "" || !strings.Contains(failure, "no answer within") {
		t.Fatalf("pack install without --tool = (%q, %q), want in-lane timeout", res, failure)
	}
}
