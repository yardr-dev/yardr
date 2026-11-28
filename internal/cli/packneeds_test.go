package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

func writeNeedPack(t *testing.T, packYAML string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"pack.yaml":        packYAML,
		"roles/builder.md": "# Builder\n",
		"flows/new.md":     "# New\n",
	}
	for rel, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeTestTool(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

// pack check and pack show print "its check runs when the line is applied"
// without executing any check command, while pack apply runs the check after
// confirmation and prints missing needs on their own lines.
func TestPackNeedsCheckAndShowRunNothing(t *testing.T) {
	a := packApp(t)
	requireNonInteractive(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	marker := filepath.Join(t.TempDir(), "check-ran")
	writeTestTool(t, filepath.Join(bin, "mytool"), "#!/bin/sh\necho ran > "+marker+"\necho 2.4.1\nexit 0\n")

	dir := writeNeedPack(t, `name: needline
api: 9
kind: git
needs:
  - name: mytool
    check: [mytool, --version]
  - missing-tool
  - connector: github
flows:
  default:
    stages:
      new: { group: builders, file: flows/new.md, outcomes: { done: done } }
      done: { end: true }
groups:
  builders: { agent: roles/builder.md }
`)

	out, err := run(t, a.packCmd(), "check", dir, "--depot", "web")
	if err != nil {
		t.Fatalf("pack check: %v\n%s", err, out)
	}
	if !strings.Contains(out, pack.CheckNotRunDetail) {
		t.Errorf("pack check output lacks %q:\n%s", pack.CheckNotRunDetail, out)
	}
	// Whoever applies the line sees the command the apply will run.
	if !strings.Contains(out, "check command: mytool --version") {
		t.Errorf("pack check output does not show the check command:\n%s", out)
	}
	if !strings.Contains(out, "missing connector github") || !strings.Contains(out, "yardr pack apply github") {
		t.Errorf("pack check output lacks missing connector advice:\n%s", out)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check command ran during pack check")
	}

	out, err = run(t, a.packCmd(), "show", dir)
	if err != nil {
		t.Fatalf("pack show: %v\n%s", err, out)
	}
	if !strings.Contains(out, pack.CheckNotRunDetail) {
		t.Errorf("pack show output lacks %q:\n%s", pack.CheckNotRunDetail, out)
	}
	if !strings.Contains(out, "check command: mytool --version") {
		t.Errorf("pack show output does not show the check command:\n%s", out)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check command ran during pack show")
	}

	// Applying runs the check and prints each missing need.
	out, err = run(t, a.packCmd(), "apply", dir, "--depot", "web", "--yes", "--bind", "builders=claude")
	if err != nil {
		t.Fatalf("pack apply: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("check command did not run during pack apply: %v", err)
	}
	if !strings.Contains(out, "missing-tool: missing") {
		t.Errorf("pack apply output lacks missing-tool:\n%s", out)
	}
	if !strings.Contains(out, "missing connector github") || !strings.Contains(out, "yardr pack apply github") {
		t.Errorf("pack apply output lacks missing connector github:\n%s", out)
	}
}

// yardr pack needs and yardr doctor produce the exact same wording for
// missing needs, both on a yard with no declared sources and on one with
// declared sources, as notes (never faults).
func TestPackNeedsAndDoctorMatchWords(t *testing.T) {
	requireNonInteractive(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	writeTestTool(t, filepath.Join(bin, "oktool"), "#!/bin/sh\necho 3.11.4\nexit 0\n")

	dir := writeNeedPack(t, `name: docline
api: 9
kind: git
needs:
  - name: oktool
    check: [oktool]
  - missing-tool
  - connector: github
flows:
  default:
    stages:
      new: { group: builders, file: flows/new.md, outcomes: { done: done } }
      done: { end: true }
groups:
  builders: { agent: roles/builder.md }
`)

	for _, withSource := range []bool{false, true} {
		name := "without declared sources"
		if withSource {
			name = "with declared sources"
		}
		t.Run(name, func(t *testing.T) {
			st := storetest.New(t)
			ctx := context.Background()
			if err := st.PutDepot(ctx, store.Depot{Name: "web", Base: "main"}, "me"); err != nil {
				t.Fatal(err)
			}
			if err := st.SetSetting(ctx, store.OperatorSetting, "me", "me"); err != nil {
				t.Fatal(err)
			}
			if withSource {
				if _, err := st.PutSource(ctx, "linear.yaml", linearPackDecl, "me"); err != nil {
					t.Fatal(err)
				}
			}
			a := &app{st: st, actor: "me", cfg: &config.Config{Home: t.TempDir()}}
			if out, err := run(t, a.packCmd(), "apply", dir, "--depot", "web", "--yes", "--bind", "builders=claude"); err != nil {
				t.Fatalf("apply: %v\n%s", err, out)
			}

			needsOut, err := run(t, a.packCmd(), "needs")
			if err != nil {
				t.Fatalf("pack needs: %v\n%s", err, needsOut)
			}
			if !strings.Contains(needsOut, "oktool: ok 3.11.4") {
				t.Errorf("pack needs lacks oktool version:\n%s", needsOut)
			}
			part, err := sourcesPart(ctx, a.cfg, st, false)
			if err != nil {
				t.Fatalf("sourcesPart: %v", err)
			}
			var missingFromNeeds []string
			for _, line := range strings.Split(strings.TrimSpace(needsOut), "\n") {
				if strings.Contains(line, "missing") {
					missingFromNeeds = append(missingFromNeeds, line)
				}
			}
			if len(missingFromNeeds) != 2 {
				t.Fatalf("missing lines from pack needs = %v, want 2", missingFromNeeds)
			}
			for _, want := range missingFromNeeds {
				if !slices.Contains(part.Notes, want) {
					t.Errorf("doctor notes = %v, missing exact pack needs line %q", part.Notes, want)
				}
			}
			for _, note := range part.Notes {
				if strings.Contains(note, "oktool") {
					t.Errorf("doctor included ok tool in notes: %q", note)
				}
			}
		})
	}
}

// pack needs <line> add and rm edit the record, handle duplicates/absent words,
// support removing a tool with options, scope with --depot, and persist across
// a second apply of the line.
func TestPackNeedsAddRmAndReapply(t *testing.T) {
	a := packApp(t)
	requireNonInteractive(t)
	ctx := context.Background()
	if err := a.st.PutDepot(ctx, store.Depot{Name: "other", Base: "main"}, "me"); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	writeTestTool(t, filepath.Join(bin, "pytool"), "#!/bin/sh\necho 3.12.0\nexit 0\n")

	dir := writeNeedPack(t, `name: editline
api: 9
kind: git
needs:
  - name: pytool
    env: PYTOOL_BIN
    check: [pytool]
flows:
  default:
    stages:
      new: { group: builders, file: flows/new.md, outcomes: { done: done } }
      done: { end: true }
groups:
  builders: { agent: roles/builder.md }
`)
	if out, err := run(t, a.packCmd(), "apply", dir, "--depot", "web", "--yes", "--bind", "builders=claude"); err != nil {
		t.Fatalf("apply web: %v\n%s", err, out)
	}
	if out, err := run(t, a.packCmd(), "apply", dir, "--depot", "other", "--yes", "--bind", "builders=claude"); err != nil {
		t.Fatalf("apply other: %v\n%s", err, out)
	}

	// Add ffmpeg to web only.
	out, err := run(t, a.packCmd(), "needs", "editline", "add", "ffmpeg", "--depot", "web")
	if err != nil {
		t.Fatalf("add ffmpeg: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added ffmpeg") {
		t.Errorf("add output = %s", out)
	}
	// Adding again says nothing changed.
	out, err = run(t, a.packCmd(), "needs", "editline", "add", "ffmpeg", "--depot", "web")
	if err != nil {
		t.Fatalf("add ffmpeg twice: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing changed") {
		t.Errorf("duplicate add output = %s", out)
	}
	// Add connector:github and remove pytool (which had env + check) on web.
	if out, err := run(t, a.packCmd(), "needs", "editline", "add", "connector:github", "--depot", "web"); err != nil {
		t.Fatalf("add connector: %v\n%s", err, out)
	}
	if out, err := run(t, a.packCmd(), "needs", "editline", "rm", "pytool", "--depot", "web"); err != nil {
		t.Fatalf("rm pytool: %v\n%s", err, out)
	}
	if out, err := run(t, a.packCmd(), "needs", "editline", "rm", "ffmpeg", "--depot", "web"); err != nil {
		t.Fatalf("rm ffmpeg: %v\n%s", err, out)
	}
	// Removing absent word says nothing changed.
	out, err = run(t, a.packCmd(), "needs", "editline", "rm", "ffmpeg", "--depot", "web")
	if err != nil {
		t.Fatalf("rm absent: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing changed") {
		t.Errorf("absent rm output = %s", out)
	}
	// The plan follows the recorded list, which may differ from pack.yaml.
	out, err = run(t, a.packCmd(), "check", dir, "--depot", "web")
	if err != nil {
		t.Fatalf("check edited needs: %v\n%s", err, out)
	}
	if strings.Contains(out, "pytool:") || !strings.Contains(out, "missing connector github") {
		t.Fatalf("plan needs do not match edited record:\n%s", out)
	}

	// Re-apply keeps the edited list on web (only connector:github) and leaves other untouched (pytool).
	if out, err := run(t, a.packCmd(), "apply", dir, "--depot", "web", "--yes"); err != nil {
		t.Fatalf("re-apply web: %v\n%s", err, out)
	}
	a.json = true
	jsonOut, err := run(t, a.packCmd(), "needs", "editline", "--depot", "web")
	a.json = false
	if err != nil {
		t.Fatalf("needs --json: %v\n%s", err, jsonOut)
	}
	var entries []packNeedsEntry
	if err := json.Unmarshal([]byte(jsonOut), &entries); err != nil {
		t.Fatalf("unmarshal %s: %v", jsonOut, err)
	}
	if len(entries) != 1 || len(entries[0].Needs) != 1 || entries[0].Needs[0].Need != "github" || entries[0].Needs[0].Kind != "connector" {
		t.Fatalf("web needs after re-apply = %+v, want [connector github] alone", entries)
	}
	// other depot still has pytool untouched.
	otherOut, err := run(t, a.packCmd(), "needs", "editline", "--depot", "other")
	if err != nil {
		t.Fatalf("needs other: %v\n%s", err, otherOut)
	}
	if !strings.Contains(otherOut, "pytool: ok 3.12.0") || strings.Contains(otherOut, "github") {
		t.Fatalf("other depot needs = %s", otherOut)
	}
	// Removing the last need leaves an intentional empty list through another apply.
	if out, err := run(t, a.packCmd(), "needs", "editline", "rm", "connector:github", "--depot", "web"); err != nil {
		t.Fatalf("rm last need: %v\n%s", err, out)
	}
	out, err = run(t, a.packCmd(), "check", dir, "--depot", "web")
	if err != nil {
		t.Fatalf("check empty needs: %v\n%s", err, out)
	}
	if strings.Contains(out, "pytool:") || strings.Contains(out, "connector github:") {
		t.Fatalf("plan restored needs after removing the last one:\n%s", out)
	}
	if out, err := run(t, a.packCmd(), "apply", dir, "--depot", "web", "--yes"); err != nil {
		t.Fatalf("re-apply empty needs: %v\n%s", err, out)
	}
	emptyOut, err := run(t, a.packCmd(), "needs", "editline", "--depot", "web")
	if err != nil || !strings.Contains(emptyOut, "no needs recorded") {
		t.Fatalf("needs after re-applying empty list = %v\n%s", err, emptyOut)
	}
}

// A line whose every group is worked by the peer runner reports "not checked
// here" in pack needs, runs no check command, and adds no doctor note.
func TestPackNeedsPeerOnly(t *testing.T) {
	a := packApp(t)
	requireNonInteractive(t)
	ctx := context.Background()
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	marker := filepath.Join(t.TempDir(), "peer-ran")
	writeTestTool(t, filepath.Join(bin, "mytool"), "#!/bin/sh\necho ran > "+marker+"\nexit 0\n")

	linkedTo(t, a, "other")
	if _, err := a.st.AllowPeer(ctx, "other", store.PeerAllow{Send: []string{"bead"}}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := a.st.PutGroup(ctx, store.Group{Name: "web-builders", Runner: store.PeerRunner, Config: map[string]any{store.GroupPeer: "other"}}, "me"); err != nil {
		t.Fatal(err)
	}
	dir := writeNeedPack(t, `name: peerline
api: 9
kind: git
needs:
  - name: mytool
    check: [mytool]
  - connector: github
flows:
  default:
    stages:
      new: { group: builders, file: flows/new.md, outcomes: { done: done } }
      done: { end: true }
groups:
  builders: { agent: roles/builder.md }
`)
	if out, err := run(t, a.packCmd(), "apply", dir, "--depot", "web", "--yes"); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	out, err := run(t, a.packCmd(), "needs", "peerline")
	if err != nil {
		t.Fatalf("pack needs: %v\n%s", err, out)
	}
	if !strings.Contains(out, "mytool: not checked here") || !strings.Contains(out, "connector github: not checked here") {
		t.Fatalf("pack needs peer-only =\n%s", out)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check command ran for peer-only line")
	}
	part, err := sourcesPart(ctx, a.cfg, a.st, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, note := range part.Notes {
		if strings.Contains(note, "peerline") {
			t.Errorf("doctor emitted note for peer-only line: %q", note)
		}
	}
}
