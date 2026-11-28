package pack_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
)

// storedDigest is the digest of the flow the store holds under name.
func storedDigest(t *testing.T, st store.API, name string) string {
	t.Helper()
	f, err := st.GetFlow(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// yardEdit changes the flow the store holds under name, as a person does
// with yardr flow set, and says nothing to any import.
func yardEdit(t *testing.T, st store.API, name string, change func(f *store.Flow)) {
	t.Helper()
	f, err := st.GetFlow(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	change(f)
	if _, err := st.PutFlow(ctx, *f, "me"); err != nil {
		t.Fatal(err)
	}
}

// movePack changes what the starter's decide stage may move a bead to.
func movePack(t *testing.T, dir string) {
	t.Helper()
	edit(t, dir, "manual: [backlog, new] }", "manual: [backlog] }")
}

// A line copies once: editing a stage file, role, script or flow in the yard
// reports differs and writes nothing on the second apply, the third apply,
// and an apply after an upgrade changed the pack's own copy.
func TestEditedObjectsDifferAndAreNotOverwritten(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	first := build(t, st, load(t, dir), o)
	apply(t, st, first, answers)

	// A line's record holds names and no sums on files or flows.
	for _, po := range record(t, st).Objects {
		if (po.Kind == "file" || po.Kind == "flow") && po.SHA256 != "" {
			t.Fatalf("line record carries sha256 on %s %s: %q", po.Kind, po.Name, po.SHA256)
		}
	}

	// Edit a stage file, a role, a script and a flow in the yard.
	stagePath := find(t, first, "file", "flows/default/new.md").File.To
	rolePath := find(t, first, "file", "roles/builder.md").File.To
	scriptPath := find(t, first, "file", "scripts/merge-local").File.To
	if err := os.WriteFile(stagePath, []byte("yard stage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rolePath, []byte("yard role\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho yard\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	yardEdit(t, st, "starter.default", func(f *store.Flow) {
		f.Transitions["backlog"] = append(f.Transitions["backlog"], "triage")
		f.Transitions["triage"] = []string{"new"}
	})
	yardFlow := storedDigest(t, st, "starter.default")

	// Second apply and third apply both report differs and write nothing.
	for attempt := 1; attempt <= 2; attempt++ {
		plan := build(t, st, load(t, dir), o)
		for _, want := range [][2]string{
			{"file", "flows/default/new.md"},
			{"file", "roles/builder.md"},
			{"file", "scripts/merge-local"},
			{"flow", "starter.default"},
		} {
			obj := find(t, plan, want[0], want[1])
			if obj.Action != pack.Differs || len(obj.Changes) == 0 {
				t.Fatalf("attempt %d: %s %s = %+v, want differs with changes", attempt, want[0], want[1], obj)
			}
			if obj.YardText == nil || obj.OriginalText == nil || obj.Diff == nil || *obj.Diff == "" {
				t.Fatalf("attempt %d: %s %s lacks compare texts/diff: yard=%v orig=%v diff=%v", attempt, want[0], want[1], obj.YardText != nil, obj.OriginalText != nil, obj.Diff)
			}
		}
		for _, obj := range plan.Objects {
			if obj.Action == pack.Unchanged && (obj.YardText != nil || obj.OriginalText != nil || obj.Diff != nil) {
				t.Fatalf("attempt %d: unchanged %s %s carries compare texts/diff", attempt, obj.Kind, obj.Name)
			}
		}
		before := head(t, st)
		res := apply(t, st, plan, nil)
		if res.Wrote() || res.Differs != 4 {
			t.Fatalf("attempt %d: apply = %+v, want Wrote=false Differs=4", attempt, res)
		}
		if got := kinds(t, st, before); !slices.Equal(got, []string{"pack_unchanged"}) {
			t.Fatalf("attempt %d: events = %v, want [pack_unchanged]", attempt, got)
		}
	}

	// Simulate an upgrade that changed the pack's flow and stage file: the
	// yard's copies still stand and still report differs.
	movePack(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "flows", "default", "new.md"), []byte("upgraded pack stage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	upgraded := build(t, st, load(t, dir), o)
	if f := find(t, upgraded, "file", "flows/default/new.md"); f.Action != pack.Differs {
		t.Fatalf("after upgrade stage file = %+v, want differs", f)
	}
	if f := find(t, upgraded, "flow", "starter.default"); f.Action != pack.Differs {
		t.Fatalf("after upgrade flow = %+v, want differs", f)
	}
	res := apply(t, st, upgraded, nil)
	if res.Wrote() || res.Differs != 4 {
		t.Fatalf("apply after upgrade = %+v, want Wrote=false Differs=4", res)
	}
	if got, _ := os.ReadFile(stagePath); string(got) != "yard stage\n" {
		t.Fatalf("stage file overwritten: %q", got)
	}
	if got, _ := os.ReadFile(rolePath); string(got) != "yard role\n" {
		t.Fatalf("role file overwritten: %q", got)
	}
	if got, _ := os.ReadFile(scriptPath); string(got) != "#!/bin/sh\necho yard\n" {
		t.Fatalf("script overwritten: %q", got)
	}
	if got := storedDigest(t, st, "starter.default"); got != yardFlow {
		t.Fatalf("flow overwritten: got %s, want %s", got, yardFlow)
	}

	// A group whose prompt points to a card-written role file shows that
	// file's text beside the line's filled role; a group differing only in
	// limit shows its setting diff.
	customRole := filepath.Join(o.Home, "roles", "web-builders.custom.md")
	if err := os.MkdirAll(filepath.Dir(customRole), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customRole, []byte("# Custom builder\n\nYard-specific rule.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bg, err := st.GetGroup(ctx, "web-builders")
	if err != nil {
		t.Fatal(err)
	}
	bg.Config["prompt"] = customRole
	if err := st.PutGroup(ctx, *bg, "me"); err != nil {
		t.Fatal(err)
	}
	rg, err := st.GetGroup(ctx, "web-reviewers")
	if err != nil {
		t.Fatal(err)
	}
	rg.Limit = 7
	if err := st.PutGroup(ctx, *rg, "me"); err != nil {
		t.Fatal(err)
	}
	gp := build(t, st, load(t, dir), o)
	bObj := find(t, gp, "group", "web-builders")
	if bObj.Action != pack.Differs || bObj.YardText == nil || *bObj.YardText != "# Custom builder\n\nYard-specific rule.\n" || bObj.OriginalText == nil || !strings.Contains(*bObj.OriginalText, "# Builder") || bObj.Diff == nil || !strings.Contains(*bObj.Diff, "-# Custom builder") {
		t.Fatalf("differing group prompt compare = %+v", bObj)
	}
	rObj := find(t, gp, "group", "web-reviewers")
	if rObj.Action != pack.Differs || rObj.YardText == nil || *rObj.YardText != "limit: 7\n" || rObj.OriginalText == nil || !strings.Contains(*rObj.OriginalText, "limit:") || rObj.Diff == nil || !strings.Contains(*rObj.Diff, "-limit: 7") {
		t.Fatalf("differing group limit compare = %+v", rObj)
	}
}

// Removing a flow, stage file, role or script causes the next apply to plan
// create for it and write it back.
func TestCutObjectsAreRecreated(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	first := build(t, st, load(t, dir), o)
	apply(t, st, first, answers)

	stagePath := find(t, first, "file", "flows/default/new.md").File.To
	rolePath := find(t, first, "file", "roles/builder.md").File.To
	scriptPath := find(t, first, "file", "scripts/merge-local").File.To
	for _, p := range []string{stagePath, rolePath, scriptPath} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetFlowPointer(ctx, "web", "", "", "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteFlow(ctx, "starter.default", "me"); err != nil {
		t.Fatal(err)
	}

	plan := build(t, st, load(t, dir), o)
	for _, want := range [][2]string{
		{"file", "flows/default/new.md"},
		{"file", "roles/builder.md"},
		{"file", "scripts/merge-local"},
		{"flow", "starter.default"},
		{"pointer", "depot web -> starter.default"},
	} {
		if obj := find(t, plan, want[0], want[1]); obj.Action != pack.Create {
			t.Fatalf("%s %s = %s, want create", want[0], want[1], obj.Action)
		}
	}
	res := apply(t, st, plan, nil)
	if res.Created != 5 {
		t.Fatalf("created = %d, want 5 (%+v)", res.Created, res)
	}
	for _, p := range []string{stagePath, rolePath, scriptPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing recreated %s: %v", p, err)
		}
	}
	if _, err := st.GetFlow(ctx, "starter.default"); err != nil {
		t.Fatalf("missing recreated flow: %v", err)
	}
}

// --replace overwrites a differing role or script under <home>/packs/<pack>/
// with the pack's bytes, records the apply, and refuses stage files, unchanged
// files, missing files and invalid paths before writing anything.
func TestReplaceOverwritesDifferingPackFilesAndRefusesTheRest(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	first := build(t, st, load(t, dir), o)
	apply(t, st, first, answers)

	scriptPath := find(t, first, "file", "scripts/merge-local").File.To
	wantScript, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	rolePath := find(t, first, "file", "roles/builder.md").File.To
	wantRole, err := os.ReadFile(rolePath)
	if err != nil {
		t.Fatal(err)
	}
	stagePath := find(t, first, "file", "flows/default/new.md").File.To

	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rolePath, []byte("custom role\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagePath, []byte("custom stage\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := build(t, st, load(t, dir), o)
	for bad, wantErr := range map[string]string{
		"flows/default/new.md": "stage files are changed with yardr flow set-stage",
		"roles/reviewer.md":    "does not differ in the plan",
		"scripts/no-such":      "the plan has no such file",
		"../escape":            "name a file under the line's directory",
		"/etc/passwd":          "name a file under the line's directory",
		"scripts/":             "name a file under the line's directory",
	} {
		before := head(t, st)
		_, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{
			Answers: answered(t, plan, nil), Actor: "me", Replace: []string{"scripts/merge-local", bad},
		})
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("--replace %q: err = %v, want %q", bad, err, wantErr)
		}
		if got := kinds(t, st, before); len(got) != 0 {
			t.Errorf("--replace %q: refused apply logged %v", bad, got)
		}
	}

	// Replace the script and role; the stage file stays as the yard has it.
	res, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{
		Answers: answered(t, plan, nil), Actor: "me", Replace: []string{"scripts/merge-local", "roles/builder.md"},
	})
	if err != nil || res.Changed != 2 || res.Differs != 1 || !res.Recorded {
		t.Fatalf("replace apply = %+v, %v; want Changed=2 Differs=1 Recorded=true", res, err)
	}
	if got, _ := os.ReadFile(scriptPath); string(got) != string(wantScript) {
		t.Fatalf("script was not replaced")
	}
	if got, _ := os.ReadFile(rolePath); string(got) != string(wantRole) {
		t.Fatalf("role was not replaced")
	}
	if got, _ := os.ReadFile(stagePath); string(got) != "custom stage\n" {
		t.Fatalf("stage file was touched: %q", got)
	}
}

// A depot that already has a flow pointer keeps it when a line is applied:
// the pointer reports differs and FlowQuestion is skipped.
func TestExistingFlowPointerIsKeptAndSkipsFlowQuestion(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	edit(t, dir, "      merged:   { end: true }\n",
		"      merged:   { end: true }\n  short:\n    stages:\n      new:    { group: builders, outcomes: { done: merged } }\n      merged: { end: true }\n")
	if _, err := st.PutFlow(ctx, store.Flow{
		Name: "custom", Stages: []string{"new", "merged"},
		Transitions: map[string][]string{"new": {"merged"}}, Terminal: []string{"merged"},
	}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFlowPointer(ctx, "web", "", "custom", "me"); err != nil {
		t.Fatal(err)
	}
	plan := build(t, st, load(t, dir), o)
	if slices.ContainsFunc(plan.Questions, func(q pack.Question) bool { return q.ID == pack.FlowQuestion }) {
		t.Fatalf("FlowQuestion asked despite existing pointer: %+v", plan.Questions)
	}
	ptr := find(t, plan, "pointer", "depot web -> custom")
	if ptr.Action != pack.Differs {
		t.Fatalf("pointer = %+v, want differs", ptr)
	}
	apply(t, st, plan, answers)
	ptrs, err := st.FlowPointers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, p := range ptrs {
		if p.Depot == "web" && p.Type == "" {
			got = p.Flow
		}
	}
	if got != "custom" {
		t.Fatalf("pointer after apply = %q, want custom", got)
	}
}
