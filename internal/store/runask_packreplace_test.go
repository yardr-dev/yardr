package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func replaceSum(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func replaceFile(name, old, new string) AskPlanObject {
	return AskPlanObject{
		Kind: "file", Name: name, Action: "change",
		FileOldSHA: replaceSum(old), FileNewSHA: replaceSum(new),
		OldText: &old, Text: new, Diff: Unified(old, new),
	}
}

func replacePlan() *AskPlan {
	script := replaceFile("scripts/merge-local", "#!/bin/sh\necho old\n", "#!/bin/sh\necho new\n")
	script.Changes = []AskChange{{Field: "mode", Old: "0644", New: "0755"}}
	return &AskPlan{Objects: []AskPlanObject{
		script,
		replaceFile("roles/b.md", "role by hand\n\n", "role by the pack\n\n"),
		{Kind: "flow", Name: "starter.default", Action: "differs",
			Changes: []AskChange{{Field: "stages", Old: "in, work, done", New: "in, done"}}},
		{Kind: "file", Name: "roles/c.md", Action: "unchanged"},
		replaceFile("roles/empty.md", "", ""),
	}}
}

// A card with --replace shows each replaced file whole (both texts and diff),
// says approval keeps no other copy, weighs code, is never summarized,
// and sorts the --replace arguments in the stored command.
func packApplyReplaceShowsWhole(t *testing.T) {
	t.Helper()
	y, _ := runYard(t)
	y.firstInstallDepot(t)
	y.setPack(&fakePack{label: "embedded in yardr test1", kind: "git", depot: "web", plan: replacePlan(), noSources: true})

	ask := packAsk()
	ask.Run.Argv = []string{"pack apply starter --depot web --replace scripts/merge-local --replace roles/b.md --replace roles/empty.md"}
	seq := y.ask(t, ask, "yardmaster")
	got := y.asked(t, seq)
	run := got.Ask.Run
	if want := "pack apply starter --replace roles/b.md --replace roles/empty.md --replace scripts/merge-local --depot web"; strings.Join(run.Argv, " ") != want {
		t.Errorf("stored argv = %q, want %q", run.Argv, want)
	}
	if run.Pack == nil || !slices.Equal(run.Pack.Replace, []string{"roles/b.md", "roles/empty.md", "scripts/merge-local"}) || run.Pack.Summary {
		t.Fatalf("stored pack = %+v, want sorted Replace and Summary=false", run.Pack)
	}
	joined := strings.Join(run.Does, "\n")
	for _, want := range []string{
		"file roles/empty.md: approval overwrites the yard's copy with the line's bytes and keeps no other copy of it.",
		"file scripts/merge-local: change, sha256 " + replaceSum("#!/bin/sh\necho old\n") + " -> sha256 " + replaceSum("#!/bin/sh\necho new\n"),
		"file scripts/merge-local: approval overwrites the yard's copy with the line's bytes and keeps no other copy of it.",
		"file roles/b.md: approval overwrites the yard's copy with the line's bytes and keeps no other copy of it.",
		"flow starter.default: differs, left as the yard has it.",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("card lacks %q:\n%s", want, joined)
		}
	}
	lines := strings.Join(got.Lines(testCatalogue()), "\n")
	for _, want := range []string{
		"the file scripts/merge-local as it stands in the yard, whole as carried:",
		"the file scripts/merge-local mode changes 0644 -> 0755.",
		"the change to file scripts/merge-local",
		"-echo old",
		"+echo new",
		"the file scripts/merge-local as the pack would install it, whole as carried:",
		"#!/bin/sh",
		"echo new",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("rendered card lacks %q:\n%s", want, lines)
		}
	}
	if !strings.Contains(lines, "echo old") || !strings.Contains(lines, "roles/b.md as it stands in the yard, whole as carried") {
		t.Fatalf("the card omits the yard's full text:\n%s", lines)
	}
	if !strings.Contains(lines, "role by hand\n      \n") || !strings.Contains(lines, "role by the pack\n      \n") {
		t.Fatalf("the card omits a trailing blank line from a full text:\n%s", lines)
	}
	if strings.Count(lines, "(empty file)") != 2 {
		t.Fatalf("the card must show both empty file texts:\n%s", lines)
	}
	ordinary := replaceFile("scripts/ordinary", "old\n", "new\n")
	ordinary.OldText = nil
	if lines := strings.Join(packPlanFileLines(ordinary), "\n"); strings.Contains(lines, "as it stands in the yard") {
		t.Fatalf("a non-replacement card claims to carry the yard's full text:\n%s", lines)
	}
	ws := got.Ask.Weight
	i := slices.IndexFunc(ws, func(a AskWeight) bool { return a.Class == WeightCode })
	if i < 0 || !strings.Contains(ws[i].Line, "roles/b.md") || !strings.Contains(ws[i].Line, "scripts/merge-local") {
		t.Errorf("weight = %+v, want code naming replaced files", ws)
	}
	if !StrongWeight(ws) {
		t.Errorf("weight = %+v, want strong", ws)
	}
	done, err := y.ApproveAsk(y.ctx, y.bead, seq, "mckean", true, nil)
	if err != nil {
		t.Fatalf("ApproveAsk = %v", err)
	}
	if done.Result != "approved, to be carried out" {
		t.Fatalf("approval recorded %q", done.Result)
	}
}

// A replaced file that changed on disk between the ask and approval makes the
// card stale and runs nothing.
func packApplyReplaceStale(t *testing.T) {
	t.Helper()
	y, _ := runYard(t)
	y.firstInstallDepot(t)
	f := &fakePack{label: "embedded in yardr test1", kind: "git", depot: "web", plan: replacePlan(), noSources: true}
	y.setPack(f)
	ask := packAsk()
	ask.Run.Argv = []string{"pack apply starter --depot web --replace scripts/merge-local"}
	seq := y.ask(t, ask, "yardmaster")
	f.plan.Objects[0].FileOldSHA = replaceSum("#!/bin/sh\necho moved\n")
	if _, err := y.ApproveAsk(y.ctx, y.bead, seq, "mckean", true, nil); err == nil ||
		!strings.Contains(err.Error(), "scripts/merge-local") || !strings.Contains(err.Error(), nothingRan) {
		t.Fatalf("moved replaced file: ApproveAsk = %v", err)
	}
}

// --replace refusals: missing payload, non-replaceable file, over payload cap,
// and terminal-only flags.
func packApplyReplaceRefusals(t *testing.T) {
	t.Helper()
	y, _ := runYard(t)
	y.firstInstallDepot(t)
	ask := packAsk()
	ask.Run.Argv = []string{"pack apply starter --depot web --replace scripts/merge-local"}
	old := "old\n"
	for _, c := range []struct {
		name string
		plan AskPlan
		want string
	}{
		{"unchanged file", AskPlan{Objects: []AskPlanObject{{Kind: "file", Name: "scripts/merge-local", Action: "unchanged"}}}, "does not differ in the plan"},
		{"missing file", AskPlan{Objects: []AskPlanObject{{Kind: "file", Name: "roles/b.md", Action: "unchanged"}}}, "does not differ in the plan"},
		{"missing carried diff/text", AskPlan{Objects: []AskPlanObject{{Kind: "file", Name: "scripts/merge-local", Action: "change"}}}, "carries nothing of it to show whole"},
		{"missing old text", AskPlan{Objects: []AskPlanObject{{
			Kind: "file", Name: "scripts/merge-local", Action: "change",
			FileOldSHA: replaceSum("old"), FileNewSHA: replaceSum("new"),
			Text: "new\n", Diff: "-old\n+new\n",
		}}}, "carries nothing of it to show whole"},
		{"over payload cap", AskPlan{Objects: []AskPlanObject{{
			Kind: "file", Name: "scripts/merge-local", Action: "change",
			FileOldSHA: replaceSum("old"), FileNewSHA: replaceSum("new"),
			OldText: &old,
			Text:    strings.Repeat("a", MaxRunPackReplace+1), Diff: "-old\n+new\n",
		}}}, "a card holds at most 262144"},
	} {
		y.setPack(&fakePack{label: "embedded in yardr test1", kind: "git", depot: "web", plan: &c.plan, noSources: true})
		if _, err := y.AddAsk(y.ctx, y.bead, ask, "yardmaster"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: AddAsk = %v, want %q", c.name, err, c.want)
		}
	}
	// A --replace card carrying both sides of a large script fits above
	// MaxRunPackFiles (128 KiB) up to MaxRunPackReplace (256 KiB), whole.
	largeOld := strings.Repeat("a", 70<<10) + "\nold\n"
	largeNew := strings.Repeat("a", 70<<10) + "\nnew\n"
	largePlan := AskPlan{Objects: []AskPlanObject{replaceFile("scripts/merge-local", largeOld, largeNew)}}
	y.setPack(&fakePack{label: "embedded in yardr test1", kind: "git", depot: "web", plan: &largePlan, noSources: true})
	largeSeq := y.ask(t, ask, "yardmaster")
	if got := y.asked(t, largeSeq); got.Ask.Run == nil || got.Ask.Run.Pack == nil || got.Ask.Run.Pack.Summary {
		t.Fatalf("large --replace card = %+v, want whole (non-summary) card", got.Ask.Run)
	}
	y.setPack(&fakePack{label: "embedded in yardr test1", kind: "git", depot: "web", plan: &AskPlan{}, noSources: true})
	for _, argv := range []string{
		"pack apply starter --prune",
		"pack apply starter --yes",
		"pack apply starter --take flow/starter.default",
		"pack apply starter --depot-roles",
		"pack apply starter --pack-roles",
		"pack apply starter --auth x",
	} {
		a := packAsk()
		a.Run.Argv = []string{argv}
		if _, err := y.AddAsk(y.ctx, y.bead, a, "yardmaster"); err == nil || !strings.Contains(err.Error(), "terminal") {
			t.Errorf("%s: AddAsk = %v, want refusal naming a terminal", argv, err)
		}
	}
}

// An old pack record written before copy-once still holds sha256 sums on its
// file and flow objects on disk. normalPack strips non-source sums on both
// sides of the equality check so the first no-op apply over that record
// writes nothing and logs pack_unchanged, while a source sum change still
// writes the record.
func TestOldPackRecordWithSumsIsUnchangedOnNoOpApply(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "yard.db"), testCatalogue())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	ctx := t.Context()
	if err := s.PutDepot(ctx, Depot{Name: "web"}, "me"); err != nil {
		t.Fatal(err)
	}
	old := PackImport{
		Pack: "starter", Depot: "web", Source: "embedded", Flow: "default",
		Objects: []PackObject{
			{Kind: "file", Name: "/home/packs/starter/roles/builder.md", SHA256: strings.Repeat("a", 64)},
			{Kind: "flow", Name: "starter.default", SHA256: strings.Repeat("b", 64)},
			{Kind: "source", Name: "linear", SHA256: strings.Repeat("c", 64)},
		},
	}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bPacks).Put([]byte("web\x00starter"), raw)
	}); err != nil {
		t.Fatal(err)
	}

	// New no-op apply carries no sums on file/flow and the same sum on source.
	next := PackImport{
		Pack: "starter", Depot: "web", Source: "embedded", Flow: "default",
		Objects: []PackObject{
			{Kind: "file", Name: "/home/packs/starter/roles/builder.md"},
			{Kind: "flow", Name: "starter.default"},
			{Kind: "source", Name: "linear", SHA256: strings.Repeat("c", 64)},
		},
	}
	wrote, err := s.RecordPack(ctx, next, false, "me")
	if err != nil || wrote {
		t.Fatalf("no-op apply over old record with sums: wrote=%v, err=%v; want false, nil", wrote, err)
	}
	evs, err := s.Events(ctx, "", 1)
	if err != nil || len(evs) != 1 || evs[0].Kind != "pack_unchanged" {
		t.Fatalf("events = %+v (%v), want [pack_unchanged]", evs, err)
	}

	// A source declaration sum change still writes a new record, and that
	// write strips the old non-source sums.
	next.Objects[2].SHA256 = strings.Repeat("d", 64)
	wrote, err = s.RecordPack(ctx, next, false, "me")
	if err != nil || !wrote {
		t.Fatalf("source sum change: wrote=%v, err=%v; want true, nil", wrote, err)
	}
	recs, err := s.Packs(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatalf("packs = %+v (%v)", recs, err)
	}
	for _, po := range recs[0].Objects {
		if po.Kind == "source" && po.SHA256 != strings.Repeat("d", 64) {
			t.Errorf("source sha256 = %q, want %q", po.SHA256, strings.Repeat("d", 64))
		}
		if po.Kind != "source" && po.SHA256 != "" {
			t.Errorf("non-source %s %s still has sha256 %q", po.Kind, po.Name, po.SHA256)
		}
	}
}
