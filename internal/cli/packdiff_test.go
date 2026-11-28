package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
)

// pack check --diff prints both texts and the unified diff for each differing
// object, and --json carries yard_text, original_text and diff for differing
// objects alone. Without --diff, check prints a hint pointing at --diff.
func TestPackCheckDiffEmbedded(t *testing.T) {
	t.Parallel()
	a := packApp(t)
	requireNonInteractive(t)
	args := slices.Concat([]string{"apply", "starter", "--depot", "web", "--yes"}, starterFlags)
	if out, err := run(t, a.packCmd(), args...); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	stage := filepath.Join(store.HomeStageDir(a.cfg.Home, "starter.default"), "new.md")
	origBytes, err := os.ReadFile(stage)
	if err != nil {
		t.Fatal(err)
	}
	yardText := string(origBytes) + "\nYard custom stage instruction.\nTerminal marker: \x1b[2K\n"
	if err := os.WriteFile(stage, []byte(yardText), 0o644); err != nil {
		t.Fatal(err)
	}

	// Plain check prints the --diff hint and no full unified diff.
	plain, err := run(t, a.packCmd(), "check", "starter", "--depot", "web")
	if err != nil {
		t.Fatalf("check: %v\n%s", err, plain)
	}
	if !strings.Contains(plain, "1 differ and are left as the yard has them: yardr pack check starter --depot web --diff shows both texts of each") {
		t.Errorf("plain check lacks --diff hint:\n%s", plain)
	}
	if strings.Contains(plain, "--- was\n+++ is\n") {
		t.Errorf("plain check printed unified diff without --diff:\n%s", plain)
	}

	// check --diff prints both texts and the unified diff.
	diffOut, err := run(t, a.packCmd(), "check", "starter", "--depot", "web", "--diff")
	if err != nil {
		t.Fatalf("check --diff: %v\n%s", err, diffOut)
	}
	for _, want := range []string{
		"=== file flows/default/new.md ===",
		"--- yard (" + stage + ") ---",
		"Yard custom stage instruction.",
		"--- original (",
		"--- diff ---",
		"--- was\n+++ is\n",
		"-Yard custom stage instruction.",
		`Terminal marker: \x1b[2K`,
	} {
		if !strings.Contains(diffOut, want) {
			t.Errorf("check --diff lacks %q:\n%s", want, diffOut)
		}
	}
	if strings.ContainsRune(diffOut, '\x1b') {
		t.Errorf("check --diff printed a terminal control character:\n%s", diffOut)
	}

	// --json carries yard_text, original_text and diff for the differing stage
	// file and omits all three for every unchanged object.
	for _, flag := range [][]string{{"--json"}, {"--diff", "--json"}} {
		cmdArgs := append([]string{"pack", "check", "starter", "--depot", "web"}, flag...)
		out, err := run(t, a.rootCmd(), cmdArgs...)
		if err != nil {
			t.Fatalf("check %v: %v\n%s", flag, err, out)
		}
		var plan struct {
			Objects []map[string]any `json:"objects"`
		}
		if err := json.Unmarshal([]byte(out), &plan); err != nil {
			t.Fatalf("unmarshal %v: %v\n%s", flag, err, out)
		}
		sawDiff := false
		for _, obj := range plan.Objects {
			act, _ := obj["action"].(string)
			_, hasYard := obj["yard_text"]
			_, hasOrig := obj["original_text"]
			_, hasDiff := obj["diff"]
			if act == "differs" {
				sawDiff = true
				if !hasYard || !hasOrig || !hasDiff {
					t.Errorf("%v: differing %s %s missing compare fields: %+v", flag, obj["kind"], obj["name"], obj)
				}
				if got, _ := obj["yard_text"].(string); got != yardText {
					t.Errorf("%v: yard_text mismatch", flag)
				}
				if got, _ := obj["original_text"].(string); got != string(origBytes) {
					t.Errorf("%v: original_text mismatch", flag)
				}
				if got, _ := obj["diff"].(string); !strings.Contains(got, "-Yard custom stage instruction.") {
					t.Errorf("%v: diff = %q", flag, got)
				}
			} else if hasYard || hasOrig || hasDiff {
				t.Errorf("%v: %s %s %s should not carry compare fields", flag, act, obj["kind"], obj["name"])
			}
		}
		if !sawDiff {
			t.Fatalf("%v: no differs object found", flag)
		}
	}

	// A mode-only difference has equal text and an empty unified diff, but
	// JSON still carries the diff field because the object differs.
	if err := os.WriteFile(stage, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, a.rootCmd(), "pack", "check", "starter", "--depot", "web", "--json")
	if err != nil {
		t.Fatalf("check mode-only difference: %v\n%s", err, out)
	}
	var modePlan struct {
		Objects []map[string]any `json:"objects"`
	}
	if err := json.Unmarshal([]byte(out), &modePlan); err != nil {
		t.Fatalf("unmarshal mode-only plan: %v\n%s", err, out)
	}
	for _, obj := range modePlan.Objects {
		if obj["kind"] == "file" && obj["name"] == "flows/default/new.md" {
			if obj["action"] != "differs" || obj["yard_text"] != string(origBytes) || obj["original_text"] != string(origBytes) {
				t.Fatalf("mode-only file = %+v", obj)
			}
			if diff, ok := obj["diff"].(string); !ok || diff != "" {
				t.Fatalf("mode-only diff = (%q, %v), want a present empty string", diff, ok)
			}
			return
		}
	}
	t.Fatalf("mode-only plan has no stage file: %+v", modePlan.Objects)
}

// pack check --diff reads the original where the label names it: a folder as
// it stands, or a URL with the credential the label names. When that place is
// gone, pack check refuses with the same reason pack apply gives.
func TestPackCheckDiffReadsOriginalFromLabel(t *testing.T) {
	t.Parallel()
	requireNonInteractive(t)

	t.Run("folder", func(t *testing.T) {
		t.Parallel()
		a := packApp(t)
		dir := t.TempDir()
		if err := os.CopyFS(dir, os.DirFS("../../packs/starter")); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, "scripts", "merge-local"), 0o755); err != nil {
			t.Fatal(err)
		}
		args := slices.Concat([]string{"apply", dir, "--depot", "web", "--yes"}, starterFlags)
		if out, err := run(t, a.packCmd(), args...); err != nil {
			t.Fatalf("apply folder: %v\n%s", err, out)
		}
		// Change the original folder after apply: check by bare name reads
		// the folder as it stands now.
		origStage := filepath.Join(dir, "flows", "default", "new.md")
		text, err := os.ReadFile(origStage)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(origStage, append(text, "\nUpstream folder addition.\n"...), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := run(t, a.packCmd(), "check", "starter", "--depot", "web", "--diff")
		if err != nil {
			t.Fatalf("check folder --diff: %v\n%s", err, out)
		}
		if !strings.Contains(out, "+Upstream folder addition.") {
			t.Errorf("check --diff did not read updated folder:\n%s", out)
		}
		// Once the folder is gone, check refuses with the same reason as apply.
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		_, checkErr := run(t, a.packCmd(), "check", "starter", "--depot", "web", "--diff")
		_, applyErr := run(t, a.packCmd(), "apply", "starter", "--depot", "web", "--yes")
		if checkErr == nil || applyErr == nil || checkErr.Error() != applyErr.Error() || !strings.Contains(checkErr.Error(), "which is gone") {
			t.Fatalf("gone folder errors: check=%v apply=%v", checkErr, applyErr)
		}
	})

	t.Run("url with credential", func(t *testing.T) {
		t.Parallel()
		a := packApp(t)
		const secret = "Bearer compare-token-42"
		authFile := filepath.Join(config.At(a.cfg.Home).Packs(), "corp.auth")
		if err := os.MkdirAll(filepath.Dir(authFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(authFile, []byte("Authorization: "+secret+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var gone atomic.Bool
		var updated atomic.Bool
		site := http.StripPrefix("/starter/", http.FileServer(http.FS(os.DirFS("../../packs/starter"))))
		origNew, err := os.ReadFile("../../packs/starter/flows/default/new.md")
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != secret {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if gone.Load() {
				http.NotFound(w, r)
				return
			}
			if updated.Load() && r.URL.Path == "/starter/flows/default/new.md" {
				_, _ = w.Write(append(origNew, "\nUpstream URL addition.\n"...))
				return
			}
			site.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		base := srv.URL + "/starter/"

		args := slices.Concat([]string{"apply", base, "--depot", "web", "--auth", "corp", "--yes"}, starterFlags)
		if out, err := run(t, a.packCmd(), args...); err != nil {
			t.Fatalf("apply url: %v\n%s", err, out)
		}
		updated.Store(true)
		// Bare name reads the URL with the recorded credential.
		out, err := run(t, a.packCmd(), "check", "starter", "--depot", "web", "--diff")
		if err != nil {
			t.Fatalf("check url --diff: %v\n%s", err, out)
		}
		if !strings.Contains(out, "+Upstream URL addition.") {
			t.Errorf("check --diff did not read updated URL with recorded auth:\n%s", out)
		}
		// Once the URL is gone, check refuses with the same reason as apply.
		gone.Store(true)
		_, checkErr := run(t, a.packCmd(), "check", "starter", "--depot", "web", "--diff")
		_, applyErr := run(t, a.packCmd(), "apply", "starter", "--depot", "web", "--yes")
		if checkErr == nil || applyErr == nil || checkErr.Error() != applyErr.Error() || !strings.Contains(checkErr.Error(), "which is gone") {
			t.Fatalf("gone url errors: check=%v apply=%v", checkErr, applyErr)
		}
	})
}
