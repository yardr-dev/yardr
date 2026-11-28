package pack_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestNeedsParsingAndRefusals(t *testing.T) {
	t.Parallel()
	base := "name: line\napi: 9\nkind: git\n"
	validYAML := base + `needs:
  - gh
  - name: chrome
    any:
      - google-chrome
      - "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
    env: SHOT_CHROME
  - name: python3
    check: [python3, -c, "import sys; print(sys.version.split()[0])"]
  - connector: github
`
	p, err := pack.LoadDir(writePack(t, map[string]string{"pack.yaml": validYAML}))
	if err != nil {
		t.Fatalf("valid needs pack: %v", err)
	}
	if len(p.Needs) != 4 {
		t.Fatalf("len(Needs) = %d, want 4", len(p.Needs))
	}
	if p.Needs[0].Kind() != pack.LineNeedTool || p.Needs[0].Name != "gh" {
		t.Errorf("need[0] = %+v", p.Needs[0])
	}
	if p.Needs[1].Env != "SHOT_CHROME" || len(p.Needs[1].Any) != 2 {
		t.Errorf("need[1] = %+v", p.Needs[1])
	}
	if len(p.Needs[2].Check) != 3 {
		t.Errorf("need[2] = %+v", p.Needs[2])
	}
	if p.Needs[3].Kind() != pack.LineNeedConnector || p.Needs[3].Connector != "github" {
		t.Errorf("need[3] = %+v", p.Needs[3])
	}

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"api omitted (API 1)", "name: line\nneeds:\n  - gh\n", "needs requires api: 9"},
		{"api below 9", "name: line\napi: 8\nneeds:\n  - gh\n", "needs requires api: 9"},
		{"kind source", "name: conn\napi: 9\nkind: source\nneeds:\n  - gh\nsources:\n  conn:\n    declaration: conn.yaml\n", "no needs"},
		{"empty list", base + "needs: []\n", "empty"},
		{"not a list", base + "needs: gh\n", "want a list"},
		{"unknown key on tool", base + "needs:\n  - { name: gh, install: [brew] }\n", "unknown key"},
		{"extra key on connector", base + "needs:\n  - { connector: github, env: GH }\n", "unknown key"},
		{"missing name", base + "needs:\n  - { env: SHOT_CHROME }\n", "missing"},
		{"duplicate tool", base + "needs:\n  - gh\n  - name: gh\n", "listed twice"},
		{"duplicate connector", base + "needs:\n  - connector: github\n  - connector: github\n", "listed twice"},
		{"bad env name", base + "needs:\n  - { name: chrome, env: 1BAD }\n", "use letters"},
		{"relative path in any", base + "needs:\n  - { name: chrome, any: [bin/chrome] }\n", "program name on PATH or an absolute path"},
		{"empty any", base + "needs:\n  - { name: chrome, any: [] }\n", "empty"},
		{"empty check", base + "needs:\n  - { name: python3, check: [] }\n", "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"pack.yaml": tc.yaml}
			if strings.Contains(tc.yaml, "conn.yaml") {
				files["conn.yaml"] = "name: conn\nbase: https://conn.example.com\ncalls:\n  ping: { kind: read, request: { method: GET, path: / } }\n"
			}
			_, err := pack.LoadDir(writePack(t, files))
			var errs pack.Errors
			if !errors.As(err, &errs) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadDir = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

// Sequential: modifies PATH and SHOT_CHROME via t.Setenv.
func TestVisualsNeedsPlanAndApply(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.PutDepot(ctx, store.Depot{Name: "studio", Path: t.TempDir(), Kind: "git", Base: "main"}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "operator", "me", "me"); err != nil {
		t.Fatal(err)
	}
	p, err := pack.LoadDir("testdata/visuals-needs")
	if err != nil {
		t.Fatalf("load visuals-needs: %v", err)
	}

	// Stand-in Chrome on PATH, no python3 on PATH.
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("SHOT_CHROME", "")
	writeExec(t, filepath.Join(bin, "google-chrome"), "#!/bin/sh\nexit 0\n")

	home := t.TempDir()
	plan, err := pack.Build(ctx, st, p, pack.Options{Depot: "studio", Home: home})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.LineNeeds) != 2 {
		t.Fatalf("LineNeeds = %+v, want 2", plan.LineNeeds)
	}
	if g := plan.LineNeeds[0]; g.Need != "chrome" || g.Kind != "tool" || g.Status != "ok" || g.Detail != "" {
		t.Errorf("chrome status = %+v, want ok", plan.LineNeeds[0])
	}
	if g := plan.LineNeeds[1]; g.Need != "python3" || g.Kind != "tool" || g.Status != "missing" || g.Detail != "" {
		t.Errorf("python3 status = %+v, want missing", plan.LineNeeds[1])
	}

	// Add a stand-in python3 that writes a marker file if executed and prints 3.11.4.
	marker := filepath.Join(t.TempDir(), "ran")
	writeExec(t, filepath.Join(bin, "python3"), "#!/bin/sh\necho ran > "+marker+"\necho 3.11.4\nexit 0\n")

	plan, err = pack.Build(ctx, st, p, pack.Options{Depot: "studio", Home: home})
	if err != nil {
		t.Fatalf("Build with python3: %v", err)
	}
	if g := plan.LineNeeds[1]; g.Need != "python3" || g.Status != "not run" || g.Detail != pack.CheckNotRunDetail || len(g.Check) == 0 {
		t.Errorf("python3 in plan = %+v, want not run with detail", plan.LineNeeds[1])
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check command ran during Build (marker exists)")
	}

	// Apply runs the check after confirmation and records Needs.
	answers := pack.Answers{
		"group.studio-designers.kind":  "claude",
		"group.studio-designers.model": "",
		"group.studio-designers.args":  "",
	}
	res, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: answers, Actor: "me"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("check command did not run during Apply: %v", err)
	}
	if len(res.LineNeeds) != 2 || res.LineNeeds[1].Status != "ok" || res.LineNeeds[1].Detail != "3.11.4" {
		t.Fatalf("res.LineNeeds = %+v, want python3 ok 3.11.4", res.LineNeeds)
	}
	// Verify JSON layout of LineNeeds.
	raw, err := json.Marshal(plan.LineNeeds[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"need":"python3"`) || !strings.Contains(string(raw), `"kind":"tool"`) || !strings.Contains(string(raw), `"status":"not run"`) {
		t.Errorf("json = %s", raw)
	}

	// Failing check reports missing with first line of stderr.
	writeExec(t, filepath.Join(bin, "python3"), "#!/bin/sh\necho 'need python 3.9+' >&2\necho 'second line' >&2\nexit 1\n")
	plan2, err := pack.Build(ctx, st, p, pack.Options{Depot: "studio", Home: home})
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}
	res2, err := pack.Apply(ctx, st, plan2, pack.ApplyOptions{Actor: "me"})
	if err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	if len(res2.LineNeeds) != 2 || res2.LineNeeds[1].Status != pack.NeedMissing || res2.LineNeeds[1].Detail != "need python 3.9+" {
		t.Fatalf("failing check result = %+v", res2.LineNeeds)
	}

	// Editing the recorded Needs is kept across a subsequent Apply.
	recs, err := st.Packs(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatalf("Packs = %+v, %v", recs, err)
	}
	edited, changed, err := pack.AddLineNeed(recs[0].Needs, pack.LineNeed{Name: "ffmpeg"})
	if err != nil || !changed {
		t.Fatalf("AddLineNeed = %v, %v", changed, err)
	}
	recs[0].Needs = edited
	if _, err := st.RecordPack(ctx, recs[0], false, "me"); err != nil {
		t.Fatal(err)
	}
	plan3, err := pack.Build(ctx, st, p, pack.Options{Depot: "studio", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pack.Apply(ctx, st, plan3, pack.ApplyOptions{Actor: "me"}); err != nil {
		t.Fatal(err)
	}
	recs, err = st.Packs(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatal(err)
	}
	kept, err := pack.UnmarshalLineNeeds(recs[0].Needs)
	if err != nil || len(kept) != 3 || kept[2].Name != "ffmpeg" {
		t.Fatalf("after second Apply, recorded needs = %+v, want ffmpeg kept", kept)
	}
}

// Sequential: modifies PATH via t.Setenv.
func TestLineNeedsConnectorAndPeerRunner(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.PutDepot(ctx, store.Depot{Name: "web", Path: t.TempDir(), Kind: "git", Base: "main"}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "operator", "me", "me"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	dir := writePack(t, map[string]string{
		"pack.yaml": `name: connline
api: 9
kind: git
needs:
  - gh
  - connector: github
flows:
  default:
    stages:
      new: { group: builders, outcomes: { done: done } }
      done: { end: true }
groups:
  builders: { agent: roles/builder.md }
`,
		"roles/builder.md": "# Builder\n",
	})
	p, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	// Without github declared: missing connector github + yardr pack apply github.
	plan, err := pack.Build(ctx, st, p, pack.Options{Depot: "web", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.LineNeeds) != 2 {
		t.Fatalf("LineNeeds = %+v", plan.LineNeeds)
	}
	connLine := pack.FormatLineNeed(plan.LineNeeds[1], "yardr")
	if !strings.Contains(connLine, "missing connector github") || !strings.Contains(connLine, "yardr pack apply github") {
		t.Fatalf("connector line = %q", connLine)
	}

	// Declare github -> connector status becomes ok.
	decl := "name: github\nbase: https://api.github.com\ncalls:\n  ping: { kind: read, request: { method: GET, path: / } }\n"
	if _, err := st.PutSource(ctx, "github.yaml", decl, "me"); err != nil {
		t.Fatal(err)
	}
	plan, err = pack.Build(ctx, st, p, pack.Options{Depot: "web", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if plan.LineNeeds[1].Status != pack.NeedOK {
		t.Fatalf("connector status with source declared = %+v, want ok", plan.LineNeeds[1])
	}

	// When every group of the line maps to a peer group -> not checked here.
	linked(t, st, "other")
	if _, err := st.AllowPeer(ctx, "other", store.PeerAllow{Send: []string{"bead"}}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutGroup(ctx, store.Group{Name: "web-builders", Runner: store.PeerRunner, Config: map[string]any{store.GroupPeer: "other"}}, "me"); err != nil {
		t.Fatal(err)
	}
	plan, err = pack.Build(ctx, st, p, pack.Options{Depot: "web", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.LineNeeds {
		if s.Status != pack.NeedNotCheckedHere {
			t.Errorf("peer-only group need %+v, want %q", s, pack.NeedNotCheckedHere)
		}
	}
}
