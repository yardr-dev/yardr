package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/config"
)

// An up-to-date import of an embedded pack makes doctor print nothing about
// it. A differing script is named as a plain note with its --replace command,
// other differing files are counted with pack check --diff, and missing files
// keep the pack apply advice. The note is never quieted and is no fault, and
// an equal script is not named.
func TestDoctorNamesStaleEmbeddedPackCopy(t *testing.T) {
	t.Parallel()
	a := packApp(t)
	requireNonInteractive(t)
	args := append([]string{"apply", "starter", "--depot", "web", "--yes"}, starterFlags...)
	if out, err := run(t, a.packCmd(), args...); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	// Up to date: nothing about packs differing.
	if rep := doctorPackJSON(t, a); len(rep.State.StalePacks) != 0 {
		t.Fatalf("an up-to-date import is named: %v", rep.State.StalePacks)
	}

	// A copied role alone differing counts it with pack check --diff and
	// names no equal script.
	role := filepath.Join(config.At(a.cfg.Home).Pack("starter"), "roles", "builder.md")
	roleOrig, err := os.ReadFile(role)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(role, append(roleOrig, "\nStale test edit.\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, a.doctorCmd())
	for _, want := range []string{"note", "packs", "pack starter in depot web", "1 file differs from yardr",
		"pack check starter --depot web --diff"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "scripts/merge-local") {
		t.Errorf("equal script named by doctor:\n%s", out)
	}
	if strings.Contains(out, "pack apply starter") {
		t.Errorf("differing role advises pack apply:\n%s", out)
	}

	// Editing the script too names the script with --replace and counts the
	// other differing file, as a note and never a fault.
	script := filepath.Join(config.At(a.cfg.Home).Pack("starter"), "scripts", "merge-local")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n# yard edit\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for runNo := 1; runNo <= 2; runNo++ {
		out, _ = run(t, a.doctorCmd())
		for _, want := range []string{
			"note", "packs", "pack starter in depot web",
			"scripts/merge-local differs from yardr",
			"pack apply starter --depot web --replace scripts/merge-local",
			"1 other file differs",
			"pack check starter --depot web --diff",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("run %d: doctor lacks %q:\n%s", runNo, want, out)
			}
		}
	}
	rep := doctorPackJSON(t, a)
	if len(rep.State.StalePacks) != 1 || !strings.Contains(rep.State.StalePacks[0], "--replace scripts/merge-local") {
		t.Errorf("stale_packs = %v", rep.State.StalePacks)
	}
	for _, f := range rep.Faults {
		if strings.Contains(f, "pack starter") {
			t.Errorf("the stale pack is a fault: %q", f)
		}
	}

	// Removing a copied file (action create) keeps the pack apply advice.
	reviewer := filepath.Join(config.At(a.cfg.Home).Pack("starter"), "roles", "reviewer.md")
	if err := os.Remove(reviewer); err != nil {
		t.Fatal(err)
	}
	out, _ = run(t, a.doctorCmd())
	for _, want := range []string{
		"scripts/merge-local differs from yardr",
		"1 other file differs",
		"1 file not yet copied (roles/reviewer.md)",
		"pack apply starter --depot web",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor with missing file lacks %q:\n%s", want, out)
		}
	}

	// Replacing the script and restoring the role quiets the script naming.
	a.json = false
	replaceArgs := append([]string{"--actor", testOperator(t, a.st), "pack"}, args...)
	replaceArgs = append(replaceArgs, "--replace", "scripts/merge-local")
	if out, err := run(t, a.rootCmd(), replaceArgs...); err != nil {
		t.Fatalf("apply --replace: %v\n%s", err, out)
	}
	out, _ = run(t, a.doctorCmd())
	if strings.Contains(out, "scripts/merge-local") {
		t.Errorf("replaced script still named by doctor:\n%s", out)
	}
	if !strings.Contains(out, "1 file differs from yardr") {
		t.Errorf("remaining differing role not counted:\n%s", out)
	}
}

// Only embedded sources are compared: a pack imported from a directory
// whose copy changed is not named.
func TestDoctorIgnoresStaleDirectoryPackCopy(t *testing.T) {
	t.Parallel()
	a := packApp(t)
	requireNonInteractive(t)
	args := append([]string{"apply", "../../packs/starter", "--depot", "web", "--yes"}, starterFlags...)
	if out, err := run(t, a.packCmd(), args...); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	role := filepath.Join(config.At(a.cfg.Home).Pack("starter"), "roles", "builder.md")
	text, err := os.ReadFile(role)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(role, append(text, "\nStale test edit.\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := doctorPackJSON(t, a)
	if len(rep.State.StalePacks) != 0 {
		t.Errorf("a directory import is named as behind: %v", rep.State.StalePacks)
	}
	if out, _ := run(t, a.doctorCmd()); strings.Contains(out, "pack starter in depot web") {
		t.Errorf("a directory import is named:\n%s", out)
	}
}

func doctorPackJSON(t *testing.T, a *app) doctorReport {
	t.Helper()
	out, _ := run(t, a.rootCmd(), "doctor", "--json")
	var rep doctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	if rep.State == nil {
		t.Fatalf("doctor --json has no state:\n%s", out)
	}
	return rep
}
