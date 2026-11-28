package pack_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/agents"
	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// kinds are the kinds of the events logged after seq.
func kinds(t *testing.T, st store.API, after uint64) []string {
	t.Helper()
	evs, err := st.EventsSince(ctx, after, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func head(t *testing.T, st store.API) uint64 {
	t.Helper()
	h, err := st.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// edit rewrites the copied pack's pack.yaml, each old text by its new.
func edit(t *testing.T, dir string, pairs ...string) {
	t.Helper()
	path := filepath.Join(dir, "pack.yaml")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(text)
	for i := 0; i < len(pairs); i += 2 {
		if !strings.Contains(s, pairs[i]) {
			t.Fatalf("pack.yaml lacks %q", pairs[i])
		}
		s = strings.Replace(s, pairs[i], pairs[i+1], 1)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The starter pack into an empty yard makes everything it names, with the
// bindings answered, and records the import. Applied again it changes
// nothing: no object is written, the record stays, and the log gains the
// one event saying so.
func TestApplyToAnEmptyYardThenAgain(t *testing.T) {
	st, o := yard(t)
	withLinear(t, st)
	o.Crew = map[string]string{"yardmaster": "boss"}
	plan := build(t, st, load(t, starter), o)
	wantGroupEvents := 0
	for _, obj := range plan.Objects {
		if obj.Action == pack.Create && obj.Kind == "group" {
			wantGroupEvents++
		}
	}
	beforeApply := head(t, st)
	res := apply(t, st, plan, answers)
	if res.Created != 39 || res.Changed != 0 || res.Unchanged != 0 || !res.Recorded || !res.Wrote() {
		t.Fatalf("first import = %+v", res)
	}
	groupEvents := 0
	for _, kind := range kinds(t, st, beforeApply) {
		if kind == "group_set" {
			groupEvents++
		}
	}
	if groupEvents != wantGroupEvents {
		t.Fatalf("first import logged %d group_set events, want one for each of its %d groups", groupEvents, wantGroupEvents)
	}
	g, err := st.GetGroup(ctx, "web-builders")
	if err != nil {
		t.Fatal(err)
	}
	if g.Runner != "herdr" || g.Limit != 5 || g.Config["kind"] != "claude" || g.Config["model"] != "opus" ||
		g.Config["prompt"] != filepath.Join(config.At(o.Home).PackRoles("starter"), "builder.md") {
		t.Fatalf("web-builders = %+v", g)
	}
	if g, _ := st.GetGroup(ctx, "backlog"); g == nil || !slices.Equal(g.Members, []string{"ann", "yardmaster"}) {
		t.Fatalf("backlog = %+v", g)
	}
	// The starter names no limit for its people groups: they start with the
	// default of people, so a bead parked with them holds back no other.
	for _, name := range []string{"backlog", "decisions"} {
		if g, _ := st.GetGroup(ctx, name); g == nil || g.Limit != 50 {
			t.Fatalf("%s = %+v, want limit 50", name, g)
		}
	}
	if info, err := os.Stat(filepath.Join(config.At(o.Home).Pack("starter"), "scripts", "merge-local")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("merge-local: %v %v", info, err)
	}
	// The assembly lands by squash from the start: its cmd begins with the
	// pack's env, and the second import below finds it as the pack would
	// make it.
	if g, err := st.GetGroup(ctx, "web-assembly"); err != nil || g.Config["cmd"] != "YARDR_LAND=squash "+filepath.Join(config.At(o.Home).Pack("starter"), "scripts", "merge-local") {
		t.Fatalf("web-assembly = %+v, %v", g, err)
	}
	// The yard keeps the flow with the limits on the rounds back to new, one
	// for each way back: the import carries them as the pack writes them.
	kept, err := st.GetFlow(ctx, "starter.default")
	if err != nil {
		t.Fatal(err)
	}
	for stage, outcome := range map[string]string{"review": "changes", "review-security": "changes", "clear": "changes", "approved": "failed"} {
		if lim, err := kept.Outcome(stage, outcome); err != nil || lim.To != "new" || lim.Max != 3 || lim.Then != "decide" {
			t.Errorf("stored flow, %s %s = %+v, %v; want back to new, max 3, then decide", stage, outcome, lim, err)
		}
	}
	boss, err := st.GetCrew(ctx, "boss")
	if err != nil || !slices.Equal(boss.On, []string{store.KindAttention}) || len(boss.Subscriptions) != 4 || boss.Config["kind"] != "claude" || boss.Dir == "" || boss.Config["prompt"] != nil {
		t.Fatalf("boss = %+v, %v", boss, err)
	}
	if _, err := st.GetCrew(ctx, "yardmaster"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a crew member yardmaster exists beside boss: %v", err)
	}

	recs, err := st.Packs(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatalf("records = %+v, %v", recs, err)
	}
	rec := recs[0]
	if rec.Section != "" {
		t.Fatalf("record section = %q, want none: the starter carries none", rec.Section)
	}
	abs, _ := filepath.Abs(starter)
	if rec.Pack != "starter" || rec.Depot != "web" || rec.Source != abs || rec.By != "me" || rec.At.IsZero() || rec.Flow != "default" ||
		rec.Groups["builders"] != "web-builders" || rec.Groups["backlog"] != "backlog" || !reflect.DeepEqual(rec.Crew, map[string]string{"yardmaster": "boss"}) {
		t.Fatalf("record = %+v", rec)
	}
	// The starter pack is in this repository, so its commit is known.
	if len(rec.Commit) != 40 {
		t.Fatalf("commit = %q", rec.Commit)
	}
	// Everything but the crew member, which stays the yard's.
	if len(rec.Objects) != 38 {
		t.Fatalf("%d objects recorded: %+v", len(rec.Objects), rec.Objects)
	}

	// Again, with no flag: the record says who took yardmaster.
	before := head(t, st)
	o.Crew = nil
	plan = build(t, st, load(t, starter), o)
	if len(plan.Questions) != 0 || len(actions(plan, pack.Unchanged)) != 39 {
		t.Fatalf("second plan: questions %v, unchanged %v", plan.Questions, actions(plan, pack.Unchanged))
	}
	res = apply(t, st, plan, nil)
	if res.Wrote() || res.Recorded || res.Unchanged != 39 {
		t.Fatalf("second import = %+v", res)
	}
	if got := kinds(t, st, before); !slices.Equal(got, []string{"pack_unchanged"}) {
		t.Fatalf("the second import logged %v, want pack_unchanged alone", got)
	}
	again, _ := st.Packs(ctx)
	if !reflect.DeepEqual(again, recs) {
		t.Fatalf("the record changed:\n%+v\n%+v", recs, again)
	}
	routes, _ := st.Routes(ctx)
	if len(routes) != 11 {
		t.Fatalf("%d routes, want 11", len(routes))
	}
}

// A yard set up by hand under the pack's names is overwritten in what the
// pack carries and keeps the rest: the agent, model, arguments, env_file and
// nice of a group, the variables and the nice of a script group's command
// (the assembly and the other gates), the members of a people group, a crew
// member's identity and directory. No route is added beside the one a stage
// has.
func TestApplyOverAYardSetUpByHand(t *testing.T) {
	st, o := yard(t)
	withLinear(t, st)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	depot := "web"
	must(st.PutGroup(ctx, store.Group{Name: "web-builders", Runner: "herdr", Limit: 2,
		Config: map[string]any{"kind": "codex", "model": "luna", "args": []any{"--yolo"}, "env_file": "/secrets.env", "idle": "20m", "nice": 10, "prompt": "/old/builder.md"}}, "me"))
	must(st.PutGroup(ctx, store.Group{Name: "web-assembly", Runner: "exec", Limit: 1, Config: map[string]any{"cmd": "YARDR_PUSH=origin /old/merge-local --fast", "nice": 10}}, "me"))
	must(st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Members: []string{"ann"}}, "me"))
	for stage, group := range map[string]string{"new": "web-builders", "approved": "web-assembly", "decide": "decisions"} {
		_, err := st.AddRoute(ctx, store.Route{Stage: stage, Depot: &depot, Group: group}, "me")
		must(err)
	}
	_, err := st.PutCrew(ctx, store.CrewMember{Name: "boss", Runner: "herdr", Dir: "/home/boss", MaxAge: "6h",
		Config: map[string]any{"kind": "codex", "prompt": "/old/yardmaster.md", "env_file": "/boss.env"}, On: []string{"held"}}, "me")
	must(err)
	must(st.SetIdentity(ctx, "boss", "linear", store.Identity{ID: "u1", Key: "boss"}, "me"))
	must(st.AddSubscription(ctx, "boss", store.Subscription{Source: "linear", Scope: "all"}, "me"))
	// The same source and scope with a filter is another subscription, and
	// replacing the unfiltered one must leave it.
	must(st.AddSubscription(ctx, "boss", store.Subscription{Source: "linear", Scope: "all", Where: map[string]string{"team": "ENG"}}, "me"))

	o.Crew = map[string]string{"yardmaster": "boss"}
	res := apply(t, st, build(t, st, load(t, starter), o), nil)
	if !res.Wrote() {
		t.Fatalf("result = %+v", res)
	}

	g, err := st.GetGroup(ctx, "web-builders")
	must(err)
	want := map[string]any{"kind": "codex", "model": "luna", "args": []any{"--yolo"}, "env_file": "/secrets.env", "idle": "20m", "nice": float64(10), "prompt": "/old/builder.md"}
	if !reflect.DeepEqual(g.Config, want) || g.Limit != 2 {
		t.Fatalf("web-builders = %+v, want the yard's group left whole (limit 2, prompt /old/builder.md)", g)
	}
	g, err = st.GetGroup(ctx, "web-assembly")
	must(err)
	if got, want := g.Config["cmd"], "YARDR_PUSH=origin /old/merge-local --fast"; got != want {
		t.Fatalf("cmd = %q, want the hand's kept %q", got, want)
	}
	if g.Nice() != 10 {
		t.Fatalf("web-assembly nice = %v, want the yard's 10 kept: %+v", g.Config["nice"], g)
	}
	g, err = st.GetGroup(ctx, "decisions")
	must(err)
	if !slices.Equal(g.Members, []string{"ann"}) {
		t.Fatalf("decisions members = %v", g.Members)
	}
	routes, err := st.Routes(ctx)
	must(err)
	stages := map[string]int{}
	for _, r := range routes {
		stages[r.Stage]++
	}
	if want := map[string]int{"backlog": 1, "new": 1, "review": 1, "review-security": 1, "clear": 1, "approved": 1, "decide": 1, "open": 1, "in-pr": 1, "land": 1, "merge": 1}; !reflect.DeepEqual(stages, want) {
		t.Fatalf("routes per stage = %v, want %v", stages, want)
	}
	boss, err := st.GetCrew(ctx, "boss")
	must(err)
	// Existing crew member is left whole: prompt and event kinds stay as the
	// yard has them, and an existing linear subscription keeps its scope.
	if boss.Dir != "/home/boss" || boss.MaxAge != "6h" || boss.Config["kind"] != "codex" || boss.Config["env_file"] != "/boss.env" ||
		boss.Identities["linear"].ID != "u1" || boss.Config["prompt"] != "/old/yardmaster.md" || !slices.Equal(boss.On, []string{"held"}) {
		t.Fatalf("boss = %+v", boss)
	}
	var subs []string
	for _, s := range boss.Subscriptions {
		subs = append(subs, s.String())
	}
	slices.Sort(subs)
	if want := []string{"linear all", "linear all team=ENG", "yardr all depot=web stage=clear", "yardr all depot=web stage=decide", "yardr all depot=web state=done"}; !slices.Equal(subs, want) {
		t.Fatalf("subscriptions = %v, want %v", subs, want)
	}
}

// An import subscribes the invoker itself, and is left unrecorded: only
// the operator records an import, so a crew member's ends at the record
// with the run card named, what it wrote kept.
func TestApplySubscribingTheInvokerItselfPassesUnrecorded(t *testing.T) {
	st, o := yard(t)
	withLinear(t, st)
	o.Crew = map[string]string{"yardmaster": "boss"}
	plan := build(t, st, load(t, starter), o)
	seedGroups(t, st, plan, answered(t, plan, answers))
	res, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: answered(t, plan, answers), Actor: "boss"})
	if err == nil || !strings.Contains(err.Error(), "only the operator (me) may record the import of pack starter; this is boss") ||
		!strings.Contains(err.Error(), "applying again records nothing") ||
		!strings.Contains(err.Error(), "yardr bead ask --new - with Q: Apply pack starter to depot web?, C: the operator must approve the pack's import and R: pack apply starter --depot web on stdin") {
		t.Fatalf("apply as boss = %+v, %v; want the import written and the record refused with the run card", res, err)
	}
	if res == nil || !res.Wrote() || res.Recorded {
		t.Fatalf("result = %+v, want it written and unrecorded", res)
	}
	boss, err := st.GetCrew(ctx, "boss")
	if err != nil || len(boss.Subscriptions) != 4 {
		t.Fatalf("boss = %+v, %v", boss, err)
	}
	if recs, _ := st.Packs(ctx); len(recs) != 0 {
		t.Fatalf("a crew member's import was recorded: %+v", recs)
	}
	// The record is the one place a digest enters the yard, so a crew member
	// who writes one of its own, to have the yard's flow read as what an
	// import wrote, is refused as the operator's gate refuses the import.
	forged := store.PackImport{Pack: "starter", Depot: "web", Objects: []store.PackObject{{Kind: "flow", Name: "starter.default", SHA256: strings.Repeat("0", 64)}}}
	if _, err := st.RecordPack(ctx, forged, true, "boss"); err == nil || !strings.Contains(err.Error(), "only the operator (me) may record the import of pack starter; this is boss") {
		t.Fatalf("a crew member's record of a flow digest = %v", err)
	}
	if recs, _ := st.Packs(ctx); len(recs) != 0 {
		t.Fatalf("a crew member's record was written: %+v", recs)
	}
}

// An import creating or changing another crew member's subscription fails
// loud: no silent skip, which would leave the yard quieter than the pack
// says without saying so. Nothing is recorded.
func TestApplyFailsLoudSubscribingAnotherCrew(t *testing.T) {
	const want = "only boss or the operator (me) changes what boss is told; this is clerk"

	st, o := yard(t)
	withLinear(t, st)
	if _, err := st.PutCrew(ctx, store.CrewMember{Name: "clerk", Runner: "herdr"}, "me"); err != nil {
		t.Fatal(err)
	}
	o.Crew = map[string]string{"yardmaster": "boss"}
	plan := build(t, st, load(t, starter), o)
	seedGroups(t, st, plan, answered(t, plan, answers))
	res, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: answered(t, plan, answers), Actor: "clerk"})
	if res != nil || err == nil || !strings.Contains(err.Error(), "subscription boss ") ||
		!strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "the import stopped here") {
		t.Fatalf("create as another crew = %+v, %v; want the refusal loud", res, err)
	}
	if recs, _ := st.Packs(ctx); len(recs) != 0 {
		t.Fatalf("a failed import was recorded: %+v", recs)
	}

	// A subscription the crew member already holds by source and Where is
	// unchanged whatever its scope: rescoping in the pack writes nothing.
	dir := copyStarter(t)
	st, o = yard(t)
	withLinear(t, st)
	o.Crew = map[string]string{"yardmaster": "boss"}
	apply(t, st, build(t, st, load(t, dir), o), answers)
	edit(t, dir, "linear: involved", "linear: all")
	plan = build(t, st, load(t, dir), o)
	if so := find(t, plan, "subscription", "boss linear all"); so.Action != pack.Unchanged {
		t.Fatalf("the rescoped subscription = %+v, want unchanged", so)
	}
}

// A line that drops a group, route or subscription lists nothing as dropped
// and leaves the yard's objects standing.
func TestDroppedObjectsAreReportedAndPrunedOnlyWhenAsked(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	apply(t, st, build(t, st, load(t, dir), o), answers)
	if err := st.PutGroup(ctx, store.Group{Name: "web-extra", Runner: "manual"}, "me"); err != nil {
		t.Fatal(err)
	}
	// The pack drops the second review, and the group that works it.
	edit(t, dir,
		"outcomes: { done: review-security, done-safe: clear, changes: { to: new, max: 3, then: decide }, question: decide }", "outcomes: { done: clear, changes: { to: new, max: 3, then: decide }, question: decide }",
		"      review-security: { group: security-reviewers, file: flows/default/review-security.md, outcomes: { done: clear, changes: { to: new, max: 3, then: decide }, question: decide } }\n", "",
		"outcomes: { done: review-security, changes: open, question: decide }", "outcomes: { done: approved, changes: open, question: decide }",
		"      review-security: { group: security-reviewers, outcomes: { done: approved, changes: open, question: decide } }\n", "",
		"  security-reviewers: { agent: roles/review-security.md, limit: 2, binds_as: reviewers }\n", "",
		// The stage map names the stage too, and a pack is refused for that.
		"review: review, review-security: review, merged: done", "review: review, merged: done",
		"stage=review stage=review-security type=train\"", "stage=review type=train\"")

	plan := build(t, st, load(t, dir), o)
	if got := actions(plan, pack.Dropped); len(got) != 0 {
		t.Fatalf("dropped = %v, want none", got)
	}
	res := apply(t, st, plan, nil)
	if len(res.Pruned) != 0 || len(res.Left) != 0 {
		t.Fatalf("apply = %+v, want nothing pruned or left", res)
	}
	role := filepath.Join(config.At(o.Home).PackRoles("starter"), "review-security.md")
	if _, err := st.GetGroup(ctx, "web-security-reviewers"); err != nil {
		t.Fatalf("web-security-reviewers was removed: %v", err)
	}
	stage := filepath.Join(o.Home, "flows", "starter.default", "review-security.md")
	for _, f := range []string{role, stage} {
		if _, err := os.Stat(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.GetGroup(ctx, "web-extra"); err != nil {
		t.Fatalf("web-extra was removed: %v", err)
	}
	routes, _ := st.Routes(ctx)
	if !slices.ContainsFunc(routes, func(r store.Route) bool { return r.Stage == "review-security" }) {
		t.Fatalf("review-security route was removed: %+v", routes)
	}
}

// An import that would strand an open bead is refused before anything is
// written: no file, no group, no event.
func TestApplyRefusesStrandedBeadsChangingNothing(t *testing.T) {
	st, o := yard(t)
	b, err := st.CreateBead(ctx, store.NewBead{Title: "stray", Depot: "web", Stage: "triage"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	plan := build(t, st, load(t, starter), o)
	before := head(t, st)
	_, err = pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: pack.Answers(answers), Actor: "me"})
	if err == nil || !strings.Contains(err.Error(), b.ID+" (triage)") {
		t.Fatalf("err = %v, want a refusal naming %s", err, b.ID)
	}
	if got := kinds(t, st, before); len(got) != 0 {
		t.Fatalf("a refused import logged %v", got)
	}
	groups, _ := st.Groups(ctx)
	recs, _ := st.Packs(ctx)
	if _, serr := os.Stat(plan.Install); len(groups) != 0 || len(recs) != 0 || !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("a refused import left groups %v, records %v, files %v", groups, recs, serr)
	}
}

// A question without an answer, an answer that cannot be used, and a pack
// file changed since the plan was made each stop the import before its
// first write.
func TestApplyChecksBeforeItWrites(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	plan := build(t, st, load(t, dir), o)
	before := head(t, st)
	fails := func(a pack.Answers, want string) {
		t.Helper()
		_, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: a, Actor: "me"})
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "nothing was changed") {
			t.Fatalf("err = %v, want %q", err, want)
		}
		if got := kinds(t, st, before); len(got) != 0 {
			t.Fatalf("a failed check logged %v", got)
		}
		if _, err := os.Stat(plan.Install); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a failed check wrote files: %v", err)
		}
	}
	fails(pack.Answers{}, "group.web-builders.kind has no answer")
	full := pack.Answers{}
	for _, q := range plan.Questions {
		full[q.ID] = ""
	}
	fails(full, "group web-builders needs an agent kind")
	for _, q := range plan.Questions {
		if q.Field == "kind" {
			full[q.ID] = "claude"
		}
	}
	fails(full, "crew member yardmaster needs a directory")
	full["crew.yardmaster.dir"] = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "roles", "builder.md"), []byte("changed under the plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fails(full, "builder.md changed since the plan was made")
}

// failing is a store that refuses the nth route added.
type failing struct {
	store.API
	left int
}

func (f *failing) AddRoute(ctx context.Context, r store.Route, actor string) (*store.Route, error) {
	if f.left--; f.left == 0 {
		return nil, errors.New("the disk is full")
	}
	return f.API.AddRoute(ctx, r, actor)
}

// A write that fails halfway stops the import: what was written stays,
// nothing is recorded, and applying again finishes it without doing
// anything twice.
func TestApplyAgainFinishesAnImportThatFailedHalfway(t *testing.T) {
	st, o := yard(t)
	withLinear(t, st)
	plan := build(t, st, load(t, starter), o)
	a := pack.Answers{}
	for _, q := range plan.Questions {
		a[q.ID] = map[string]string{"kind": "claude", "dir": o.Home}[q.Field]
	}
	_, err := pack.Apply(ctx, &failing{API: st, left: 3}, plan, pack.ApplyOptions{Answers: a, Actor: "me"})
	if err == nil || !strings.Contains(err.Error(), "the disk is full") || !strings.Contains(err.Error(), "applying again finishes it") {
		t.Fatalf("err = %v", err)
	}
	routes, _ := st.Routes(ctx)
	groups, _ := st.Groups(ctx)
	recs, _ := st.Packs(ctx)
	if len(routes) != 2 || len(groups) != 6 || len(recs) != 0 {
		t.Fatalf("halfway: %d routes, %d groups, %d records; want 2, 6, 0", len(routes), len(groups), len(recs))
	}

	plan = build(t, st, load(t, starter), o)
	// Left to do: nine routes, the crew member and its four subscriptions.
	if got := actions(plan, pack.Create); len(got) != 14 {
		t.Fatalf("to create after the failure: %v", got)
	}
	res := apply(t, st, plan, nil)
	if !res.Recorded {
		t.Fatalf("result = %+v", res)
	}
	routes, _ = st.Routes(ctx)
	groups, _ = st.Groups(ctx)
	if len(routes) != 11 || len(groups) != 6 {
		t.Fatalf("after applying again: %d routes, %d groups", len(routes), len(groups))
	}
	if got := actions(build(t, st, load(t, starter), o), pack.Unchanged); len(got) != 39 {
		t.Fatalf("%d unchanged, want 39", len(got))
	}
}

// A running crew member whose role or subscriptions change keeps running: the
// plan notes it and the result names it. One the import leaves as it is
// needs no restart.
func TestRunningCrewIsNotRestarted(t *testing.T) {
	st, o := yard(t)
	if _, err := st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Config: map[string]any{"kind": "claude"}, On: []string{"held"}}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.CrewStarted(ctx, "yardmaster", map[string]any{"pane": "p1"}); err != nil {
		t.Fatal(err)
	}
	plan := build(t, st, load(t, starter), o)
	if !find(t, plan, "crew", "yardmaster").CrewMember.Restart ||
		!slices.ContainsFunc(plan.Notes, func(n string) bool { return strings.Contains(n, "yardr crew restart yardmaster") }) {
		t.Fatalf("plan: %+v, notes %v", find(t, plan, "crew", "yardmaster").CrewMember, plan.Notes)
	}
	res := apply(t, st, plan, answers)
	if !slices.Equal(res.Restart, []string{"yardmaster"}) {
		t.Fatalf("restart = %v", res.Restart)
	}
	r, err := st.GetCrew(ctx, "yardmaster")
	if err != nil || !r.Live() || r.RestartRequested != "" {
		t.Fatalf("yardmaster = %+v, %v: the import must not restart it", r, err)
	}
	res = apply(t, st, build(t, st, load(t, starter), o), nil)
	if len(res.Restart) != 0 {
		t.Fatalf("restart after an import that changed nothing = %v", res.Restart)
	}
}

// depotWithRoles is the yard's depot web as a directory that has its own
// builder.md and yardmaster.md, a builders group and a yardmaster prompted as
// a yard set up by hand has them.
func depotWithRoles(t *testing.T, st store.API) {
	t.Helper()
	depotDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(depotDir, ".yardr", "roles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"builder.md", "yardmaster.md"} {
		if err := os.WriteFile(filepath.Join(depotDir, ".yardr", "roles", name), []byte("the depot's own\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutDepot(ctx, store.Depot{Name: "web", Path: depotDir, Kind: store.DepotDir}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutGroup(ctx, store.Group{Name: "web-builders", Runner: "herdr", Config: map[string]any{"kind": "claude", "prompt": "roles/builder.md"}, Limit: 5}, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Config: map[string]any{"kind": "claude", "prompt": "/home/roles/yardmaster.md"}}, "me"); err != nil {
		t.Fatal(err)
	}
}

// record is the yard's record of the starter pack in depot web.
func record(t *testing.T, st store.API) store.PackImport {
	t.Helper()
	recs, err := st.Packs(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatalf("records = %+v, %v", recs, err)
	}
	return recs[0]
}

// The pack's roles win by default, as its flow does: a group is pointed at
// the pack's copied role though the depot has its own
// .yardr/roles/<name>.md, the plan shows the change of prompt and names the
// New groups get the pack's roles by default (and name the depot's file they
// shadow), while an existing group using the depot's role stays unchanged.
// The yardmaster is the exception: its prompt is the yard's own, so the
// pack sets none for it and the depot's yardmaster role shadows nothing.
func TestPacksRolesWinOverTheDepotsOwn(t *testing.T) {
	st, o := yard(t)
	depotWithRoles(t, st)
	install := config.At(o.Home).Pack("starter")

	plan := build(t, st, load(t, starter), o)
	// Existing web-builders already uses the depot's role (roles/builder.md),
	// which reads as the line says where the depot has its own role.
	b := find(t, plan, "group", "web-builders")
	if b.Action != pack.Unchanged || len(b.Changes) != 0 || len(b.Keeps) != 0 || len(b.Shadows) != 0 || b.Group.Config["prompt"] != "roles/builder.md" {
		t.Fatalf("existing web-builders = %+v", b)
	}
	// Delete web-builders so the pack creates it anew: the pack's role wins
	// and shadows .yardr/roles/builder.md.
	if _, err := st.DeleteGroup(ctx, "web-builders", "me"); err != nil {
		t.Fatal(err)
	}
	plan = build(t, st, load(t, starter), o)
	b = find(t, plan, "group", "web-builders")
	if b.Action != pack.Create || len(b.Keeps) != 0 || !slices.Equal(b.Shadows, []string{".yardr/roles/builder.md"}) || b.Group.Config["prompt"] != filepath.Join(install, "roles", "builder.md") {
		t.Fatalf("new web-builders = %+v", b)
	}
	// The depot has no reviewer.md of its own: nothing is shadowed there.
	if r := find(t, plan, "group", "web-reviewers"); len(r.Shadows) != 0 || r.Group.Config["prompt"] != filepath.Join(install, "roles", "reviewer.md") {
		t.Fatalf("web-reviewers = %+v", r)
	}
	m := find(t, plan, "crew", "yardmaster")
	if m.CrewMember.Prompt != "" || len(m.Shadows) != 0 || len(m.Keeps) != 0 ||
		slices.ContainsFunc(m.Changes, func(d pack.Diff) bool { return d.Field == "prompt" }) {
		t.Fatalf("yardmaster = %+v", m)
	}
	apply(t, st, plan, nil)
	g, _ := st.GetGroup(ctx, "web-builders")
	r, _ := st.GetCrew(ctx, "yardmaster")
	if g.Config["prompt"] != filepath.Join(install, "roles", "builder.md") || r.Config["prompt"] != "/home/roles/yardmaster.md" {
		t.Fatalf("after the import: builders %v, yardmaster %v", g.Config["prompt"], r.Config["prompt"])
	}
	if rec := record(t, st); rec.DepotRoles {
		t.Fatalf("the record says the depot's roles were kept: %+v", rec)
	}

	// Again, with no flag: nothing to write, and no record written.
	plan = build(t, st, load(t, starter), o)
	if got := append(actions(plan, pack.Change), actions(plan, pack.Create)...); len(got) != 0 {
		t.Fatalf("a second import would write %v", got)
	}
	if res := apply(t, st, plan, nil); res.Wrote() || res.Recorded {
		t.Fatalf("the second import: %+v", res)
	}
}

// DepotRoles is the opposite choice: a new group whose depot has its own role
// gets the depot's role by name. The record remembers the choice, and an
// existing group is left whole under either flag.
func TestDepotRolesKeepsTheDepotsOwnAndIsRemembered(t *testing.T) {
	st, o := yard(t)
	depotWithRoles(t, st)
	// The builders group is new here: it gets the role by the depot's name for it.
	if _, err := st.DeleteGroup(ctx, "web-builders", "me"); err != nil {
		t.Fatal(err)
	}
	install := config.At(o.Home).Pack("starter")

	o.DepotRoles = true
	plan := build(t, st, load(t, starter), o)
	b := find(t, plan, "group", "web-builders")
	if !plan.DepotRoles || b.Action != pack.Create || b.Group.Config["prompt"] != "roles/builder.md" || len(b.Shadows) != 0 ||
		!slices.Equal(b.Keeps, []string{"prompt roles/builder.md: the depot has its own .yardr/roles/builder.md"}) {
		t.Fatalf("web-builders = %+v, prompt %v", b, b.Group.Config["prompt"])
	}
	// The depot has no reviewer.md of its own, so that group gets the pack's.
	if r := find(t, plan, "group", "web-reviewers"); len(r.Keeps) != 0 || r.Group.Config["prompt"] != filepath.Join(install, "roles", "reviewer.md") {
		t.Fatalf("web-reviewers = %+v", r)
	}
	// The yardmaster's prompt is not the depot's or the pack's to set: it
	// stays as the yard has it, with no keep to say so.
	m := find(t, plan, "crew", "yardmaster")
	if m.CrewMember.Prompt != "" || len(m.Keeps) != 0 || len(m.Shadows) != 0 || slices.ContainsFunc(m.Changes, func(d pack.Diff) bool { return d.Field == "prompt" }) {
		t.Fatalf("yardmaster = %+v", m)
	}
	// Installed all the same, for depots without their own.
	if f := find(t, plan, "file", "roles/builder.md"); f.Action != pack.Create {
		t.Fatalf("builder.md = %+v", f)
	}
	apply(t, st, plan, nil)
	g, _ := st.GetGroup(ctx, "web-builders")
	r, _ := st.GetCrew(ctx, "yardmaster")
	if g.Config["prompt"] != "roles/builder.md" || r.Config["prompt"] != "/home/roles/yardmaster.md" {
		t.Fatalf("after the import: builders %v, yardmaster %v", g.Config["prompt"], r.Config["prompt"])
	}
	if rec := record(t, st); !rec.DepotRoles {
		t.Fatalf("the record does not say the depot's roles were kept: %+v", rec)
	}

	// No flag: the recorded choice, so nothing changes.
	o.DepotRoles = false
	plan = build(t, st, load(t, starter), o)
	if got := append(actions(plan, pack.Change), actions(plan, pack.Create)...); !plan.DepotRoles || len(got) != 0 {
		t.Fatalf("a second import, depot roles %v, would write %v", plan.DepotRoles, got)
	}
	if res := apply(t, st, plan, nil); res.Wrote() || res.Recorded {
		t.Fatalf("the second import: %+v", res)
	}

	// Told PackRoles: existing web-builders keeps its depot role untouched,
	// while the record remembers the flag for future new groups.
	o.PackRoles = true
	plan = build(t, st, load(t, starter), o)
	b = find(t, plan, "group", "web-builders")
	if plan.DepotRoles || b.Action != pack.Unchanged || len(b.Keeps) != 0 || len(b.Shadows) != 0 {
		t.Fatalf("with PackRoles: %+v", b)
	}
	apply(t, st, plan, nil)
	if g, _ := st.GetGroup(ctx, "web-builders"); g.Config["prompt"] != "roles/builder.md" {
		t.Fatalf("web-builders after PackRoles: %+v", g)
	}
	if rec := record(t, st); rec.DepotRoles {
		t.Fatalf("the record after PackRoles: %+v", rec)
	}

	o.DepotRoles = true
	if _, err := pack.Build(ctx, st, load(t, starter), o); err == nil || !strings.Contains(err.Error(), "opposite choices") {
		t.Fatalf("both choices at once: %v", err)
	}
}

// Of several flows a depot could follow, the plan asks which; the import
// remembers the answer, so the next one need not be told.
func TestFlowChoiceIsAskedAndRemembered(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	edit(t, dir, "      merged:   { end: true }\n",
		"      merged:   { end: true }\n  short:\n    stages:\n      new:    { group: builders, outcomes: { done: merged } }\n      merged: { end: true }\n")
	plan := build(t, st, load(t, dir), o)
	if !slices.ContainsFunc(plan.Questions, func(q pack.Question) bool { return q.ID == pack.FlowQuestion && q.Default == "default" }) {
		t.Fatalf("questions = %+v", plan.Questions)
	}
	a := pack.Answers{}
	for _, q := range plan.Questions {
		a[q.ID] = map[string]string{"kind": "claude", "dir": o.Home}[q.Field]
	}
	a[pack.FlowQuestion] = "short"
	if _, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: a, Actor: "me"}); err == nil || !strings.Contains(err.Error(), "the plan is for flow default, not short") {
		t.Fatalf("err = %v", err)
	}

	o.Flow = "short"
	apply(t, st, build(t, st, load(t, dir), o), answers)
	o.Flow = ""
	plan = build(t, st, load(t, dir), o)
	if plan.Flow != "short" || len(plan.Questions) != 0 || len(actions(plan, pack.Create))+len(actions(plan, pack.Change)) != 0 {
		t.Fatalf("flow %q, questions %v, create %v", plan.Flow, plan.Questions, actions(plan, pack.Create))
	}

	o.Flow = "default"
	plan = build(t, st, load(t, dir), o)
	// Existing pointer is not overwritten, and nothing is dropped.
	if ptr := find(t, plan, "pointer", "depot web -> starter.short"); ptr.Action != pack.Differs {
		t.Fatalf("pointer = %+v, want differs", ptr)
	}
	if got := actions(plan, pack.Dropped); len(got) != 0 {
		t.Fatalf("dropped = %v, want none", got)
	}
	apply(t, st, plan, answers)
	if _, err := st.GetFlow(ctx, "starter.short"); err != nil {
		t.Fatalf("starter.short was removed: %v", err)
	}
}

// A new agent group and a new crew member get the permission arguments the
// import was answered with; an empty answer is none. One the yard has keeps
// its own: the plan does not even ask.
func TestApplyGivesNewAgentsTheirArguments(t *testing.T) {
	st, o := yard(t)
	if err := st.PutGroup(ctx, store.Group{Name: "web-reviewers", Runner: "herdr", Limit: 1,
		Config: map[string]any{"kind": "codex", "args": []any{"--mine"}}}, "me"); err != nil {
		t.Fatal(err)
	}
	plan := build(t, st, load(t, copyStarter(t)), o)
	for _, q := range plan.Questions {
		if strings.HasPrefix(q.ID, "group.web-reviewers.") {
			t.Fatalf("asks %s of a group the yard has", q.ID)
		}
	}
	apply(t, st, plan, map[string]string{"group.web-builders.args": "--permission-mode  auto", "crew.yardmaster.args": ""})
	args := func(cfg map[string]any) string {
		if cfg["args"] == nil {
			return ""
		}
		return fmt.Sprint(cfg["args"])
	}
	if g, err := st.GetGroup(ctx, "web-builders"); err != nil || args(g.Config) != "[--permission-mode auto]" {
		t.Fatalf("web-builders = %+v, %v", g, err)
	}
	if g, err := st.GetGroup(ctx, "web-reviewers"); err != nil || args(g.Config) != "[--mine]" {
		t.Fatalf("web-reviewers = %+v, %v", g, err)
	}
	if r, err := st.GetCrew(ctx, "yardmaster"); err != nil || args(r.Config) != "" {
		t.Fatalf("yardmaster = %+v, %v", r, err)
	}
	// Twice is once: the arguments are the yard's now.
	plan = build(t, st, load(t, copyStarter(t)), o)
	if res := apply(t, st, plan, map[string]string{"group.web-builders.args": "--other"}); res.Wrote() {
		t.Fatalf("a second apply wrote: %+v", res)
	}
	if g, _ := st.GetGroup(ctx, "web-builders"); args(g.Config) != "[--permission-mode auto]" {
		t.Fatalf("web-builders after a second apply = %+v", g)
	}
}

// stageHistory is what the stage history of the yard in home holds, each kept
// text by the digest it is kept under, and nil where the directory does not
// stand. A kept text is a plain file, read-only.
func stageHistory(t *testing.T, home string) map[string]string {
	t.Helper()
	dir := store.StageHistoryDir(home)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if info, err := os.Lstat(path); err != nil || info.Mode() != 0o400 {
			t.Errorf("the kept text %s is %v, %v, want a plain file at -r--------", e.Name(), info, err)
		}
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(text)
	}
	return out
}

// A pack's stage files are copied to where the yard reads a store flow's,
// <home>/flows/<pack>.<flow>/<stage>.md, so after the import every stage a
// bead is worked in has its file. They are recorded like the pack's other
// files: a second import changes nothing, and a copy edited by hand is not
// overwritten unasked. A stage file the import writes over is kept first, in
// the stage history a save of it keeps its text in: the yard's text where the
// import is told to take the pack's, and the text the last import wrote where
// the pack's own change goes over it; a text that stands already, or no file,
// leaves nothing to keep; and a link where the file stands is refused with
// nothing written.
func TestStageFilesGoWhereTheYardReadsThem(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	plan := build(t, st, load(t, dir), o)
	to := filepath.Join(o.Home, "flows", "starter.default", "new.md")
	f := find(t, plan, "file", "flows/default/new.md")
	if want := (pack.FileOp{From: filepath.Join(dir, "flows", "default", "new.md"), To: to, Mode: "0644", SHA256: f.File.SHA256, Fill: true, Flow: "starter.default", Stage: "new"}); f.Action != pack.Create || *f.File != want {
		t.Fatalf("new.md = %+v, file %+v\nwant %+v", f, f.File, want)
	}
	apply(t, st, plan, answers)
	// A file that did not stand replaces nothing: nothing is kept, and the
	// directory is not made.
	if got := stageHistory(t, o.Home); got != nil {
		t.Fatalf("creating the stage files made the stage history %v", got)
	}

	flow, err := st.GetFlow(ctx, "starter.default")
	if err != nil {
		t.Fatal(err)
	}
	have, stray, err := store.HomeStageFiles(o.Home, flow)
	if err != nil || len(stray) != 0 {
		t.Fatalf("stage files: %v, stray %v", err, stray)
	}
	for _, s := range flow.Stages {
		if (have[s] != "") == flow.IsTerminal(s) {
			t.Errorf("stage %s (terminal %v) has the stage file %q", s, flow.IsTerminal(s), have[s])
		}
	}
	want, _ := os.ReadFile(filepath.Join(dir, "flows", "default", "new.md"))
	if sf, err := store.HomeStageFile(o.Home, "starter.default", "new"); err != nil || sf == nil || sf.Path != to || sf.Text != string(want) {
		t.Fatalf("the yard reads %+v, %v for stage new", sf, err)
	}
	if rec := record(t, st); !slices.ContainsFunc(rec.Objects, func(po store.PackObject) bool {
		return po.Kind == "file" && po.Name == to && po.SHA256 == ""
	}) {
		t.Fatalf("the record lacks %s: %+v", to, rec.Objects)
	}
	_ = f
	plan = build(t, st, load(t, dir), o)
	if got := len(actions(plan, pack.Unchanged)); got != len(plan.Objects) {
		t.Fatalf("a second import would create %v, change %v", actions(plan, pack.Create), actions(plan, pack.Change))
	}

	// A stage file written by hand beside them is left alone, and an edited
	// stage file reports differs and is never overwritten by apply.
	own := filepath.Join(o.Home, "flows", "starter.default", "notes.md")
	if err := os.WriteFile(own, []byte("ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("edited in the yard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan = build(t, st, load(t, dir), o)
	if f := find(t, plan, "file", "flows/default/new.md"); f.Action != pack.Differs {
		t.Fatalf("new.md after a hand edit = %+v, want differs", f)
	}
	res, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Actor: "me"})
	if err != nil || res.Wrote() || res.Differs != 1 {
		t.Fatalf("applying over a differing stage file: %+v, %v", res, err)
	}
	if text, _ := os.ReadFile(to); string(text) != "edited in the yard\n" {
		t.Fatalf("apply touched the stage file: %q", text)
	}
	if text, _ := os.ReadFile(own); string(text) != "ours\n" {
		t.Fatalf("apply touched a stage file it did not bring: %q", text)
	}
	// --replace refuses stage files.
	if _, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Actor: "me", Replace: []string{"flows/default/new.md"}}); err == nil || !strings.Contains(err.Error(), "stage files are changed with yardr flow set-stage") {
		t.Fatalf("replacing a stage file: %v, want refusal", err)
	}
	_ = want
	t.Run("nested stage name", nestedStageFileDiffers)
}

// nestedStageFileDiffers checks that a nested stage file installs under the
// flow's directory, reports differs once edited, and is refused by --replace.
func nestedStageFileDiffers(t *testing.T) {
	t.Helper()
	const nested = `name: nest
flows:
  default:
    stages:
      new:      { group: people, file: flows/default/new.md, manual: [qa/smoke] }
      qa/smoke: { group: people, file: flows/default/qa/smoke.md, manual: [done] }
      done:     { end: true }
groups:
  people: { people: true }
`
	st, o := yard(t)
	dir := writePack(t, map[string]string{"pack.yaml": nested, "flows/default/new.md": "new\n", "flows/default/qa/smoke.md": "smoke\n"})
	apply(t, st, build(t, st, load(t, dir), o), nil)
	to := filepath.Join(o.Home, "flows", "nest.default", "qa", "smoke.md")
	if text, _ := os.ReadFile(to); string(text) != "smoke\n" {
		t.Fatalf("the nested stage's file after the import: %q", text)
	}
	if err := os.WriteFile(to, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := build(t, st, load(t, dir), o)
	f := find(t, plan, "file", "flows/default/qa/smoke.md")
	if f.Action != pack.Differs || f.File.Flow != "nest.default" || f.File.Stage != "qa/smoke" || !f.File.StageFile() {
		t.Fatalf("the nested stage's file = %+v, file %+v, want differs", f, f.File)
	}
	if res := apply(t, st, plan, nil); res.Wrote() || res.Differs != 1 {
		t.Fatalf("applying over differing nested stage file: %+v", res)
	}
	if text, _ := os.ReadFile(to); string(text) != "edited\n" {
		t.Fatalf("apply touched nested stage file: %q", text)
	}
}

// A pack that names no stage file brings none: the plan has only its roles
// and scripts, and nothing is made under the home's flows.
func TestPackWithoutStageFilesBringsNone(t *testing.T) {
	st, o := yard(t)
	plan := build(t, st, load(t, writePack(t, smallFiles(small))), o)
	var files []string
	for _, obj := range plan.Objects {
		if obj.Kind == "file" {
			files = append(files, obj.Name)
		}
	}
	if want := []string{"roles/builder.md", "scripts/land"}; !slices.Equal(files, want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	apply(t, st, plan, nil)
	if _, err := os.Stat(filepath.Join(o.Home, "flows")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the import made %s: %v", filepath.Join(o.Home, "flows"), err)
	}
}

// A group the pack gives no limit starts with the default of its kind: one
// session for a script, as for an agent, and fifty for people, who read what
// waits for them as a list. A group the yard has keeps the limit it has, so
// a yard set up before is not moved by importing again.
func TestPackGroupWithoutLimitStartsWithTheDefaultOfItsKind(t *testing.T) {
	want := map[string]int{"web-builders": 2, "web-assembly": 1, "people": 50}
	st, o := yard(t)
	plan := build(t, st, load(t, writePack(t, smallFiles(small))), o)
	for name, limit := range want {
		if g := find(t, plan, "group", name).Group; g.Limit != limit {
			t.Errorf("the plan makes %s with limit %d, want %d", name, g.Limit, limit)
		}
	}
	apply(t, st, plan, nil)
	for name, limit := range want {
		if g := groupOf(t, st, name); g.Limit != limit {
			t.Errorf("%s has limit %d after the import, want %d", name, g.Limit, limit)
		}
	}

	st, o = yard(t)
	if err := st.PutGroup(ctx, store.Group{Name: "people", Runner: "manual", Limit: 1}, "me"); err != nil {
		t.Fatal(err)
	}
	apply(t, st, build(t, st, load(t, writePack(t, smallFiles(small))), o), nil)
	if g := groupOf(t, st, "people"); g.Limit != 1 {
		t.Errorf("people has limit %d after the import, want the yard's 1", g.Limit)
	}
}

// An import fills the placeholders of the pack's roles and stage files with
// the yard's names: the operator, and for each crew member of the pack the
// yard's crew member that takes it. A script is copied as it is. The record
// holds the filled text, so a second import changes nothing and only a real
// hand edit of the filled copy is refused.
func TestImportFillsThePlaceholders(t *testing.T) {
	st, o := yard(t)
	o.Operator, o.Crew = "Jo", map[string]string{"yardmaster": "boss"}
	dir := copyStarter(t)
	script := filepath.Join(dir, "scripts", "merge-local")
	text, _ := os.ReadFile(script)
	if err := os.WriteFile(script, append(text, "# {{operator}} {{anything}}\n"...), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := build(t, st, load(t, dir), o)
	if want := map[string]string{"operator": "Jo", "yardmaster": "boss"}; !reflect.DeepEqual(plan.Fill, want) {
		t.Fatalf("fill = %v, want %v", plan.Fill, want)
	}
	if f := find(t, plan, "file", "roles/builder.md"); !f.File.Fill || find(t, plan, "file", "scripts/merge-local").File.Fill {
		t.Fatalf("a role is filled and a script is not: builder.md %+v", f.File)
	}
	apply(t, st, plan, answers)

	builder := filepath.Join(config.At(o.Home).PackRoles("starter"), "builder.md")
	got, _ := os.ReadFile(builder)
	for _, want := range []string{"never Jo's own session", "`yardr mail boss - <<'MAIL'`"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the installed builder.md lacks %q", want)
		}
	}
	stage, _ := os.ReadFile(filepath.Join(o.Home, "flows", "starter.default", "decide.md"))
	if !strings.Contains(string(stage), "Jo decides") {
		t.Errorf("the installed decide.md does not name the operator:\n%s", stage)
	}
	for _, f := range plan.Objects {
		if f.Kind != "file" || !f.File.Fill {
			continue
		}
		if text, _ := os.ReadFile(f.File.To); strings.Contains(string(text), "{{") {
			t.Errorf("%s is installed with a placeholder left", f.File.To)
		}
	}
	if text, _ := os.ReadFile(filepath.Join(config.At(o.Home).Pack("starter"), "scripts", "merge-local")); !strings.HasSuffix(string(text), "# {{operator}} {{anything}}\n") {
		t.Error("the script was not copied as it is")
	}
	// The record holds the file's name and no sha256.
	for _, po := range record(t, st).Objects {
		if po.Kind == "file" && po.Name == builder && po.SHA256 != "" {
			t.Errorf("the record of builder.md has sha256 %s, want empty", po.SHA256)
		}
	}

	// Again: nothing to write, and nothing differs.
	again := build(t, st, load(t, dir), pack.Options{Depot: o.Depot, Home: o.Home, Operator: "Jo"})
	if got := append(actions(again, pack.Create), actions(again, pack.Change)...); len(got) != 0 || len(actions(again, pack.Differs)) != 0 {
		t.Fatalf("a second import would write %v, differs %v", got, actions(again, pack.Differs))
	}
	if res := apply(t, st, again, nil); res.Wrote() {
		t.Fatalf("the second import wrote: %+v", res)
	}

	// Another crew member takes yardmaster: the files that name it differ.
	o.Crew = map[string]string{"yardmaster": "chief"}
	moved := build(t, st, load(t, dir), o)
	if f := find(t, moved, "file", "roles/builder.md"); f.Action != pack.Differs {
		t.Fatalf("builder.md under another crew member = %+v, want differs", f)
	}
	if f := find(t, moved, "file", "roles/review-security.md"); f.Action != pack.Unchanged {
		t.Fatalf("review-security.md names no crew member, yet = %+v", f)
	}

	// A hand edit of the filled copy differs and is left alone by apply.
	if err := os.WriteFile(builder, append(got, "edited in the yard\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := build(t, st, load(t, dir), pack.Options{Depot: o.Depot, Home: o.Home, Operator: "Jo"})
	if f := find(t, edited, "file", "roles/builder.md"); f.Action != pack.Differs {
		t.Fatalf("builder.md after a hand edit = %+v, want differs", f)
	}
	if res := apply(t, st, edited, nil); res.Wrote() || res.Differs != 1 {
		t.Fatalf("applying over a hand edit of a filled copy: %+v", res)
	}
	if text, _ := os.ReadFile(builder); !strings.HasSuffix(string(text), "edited in the yard\n") {
		t.Fatalf("the differing copy was written: %q", text)
	}
}

// Without an operator given, the yard's operator setting is who the files
// name; a yard without one gets "the operator".
func TestOperatorIsTheYardsSetting(t *testing.T) {
	// Its own yard, with no operator: yard names one.
	st := storetest.New(t)
	if err := st.PutDepot(ctx, store.Depot{Name: "web", Base: "main"}, "me"); err != nil {
		t.Fatal(err)
	}
	o := pack.Options{Depot: "web", Home: t.TempDir()}
	if got := build(t, st, load(t, starter), o).Fill["operator"]; got != "the operator" {
		t.Fatalf("operator in a yard without the setting = %q", got)
	}
	if err := st.SetSetting(ctx, "operator", "Jo", "me"); err != nil {
		t.Fatal(err)
	}
	if got := build(t, st, load(t, starter), o).Fill["operator"]; got != "Jo" {
		t.Fatalf("operator = %q, want the setting", got)
	}
}

// What is installed is what the plan was made from: a source that changed
// since is refused, compared as filled.
func TestFilledFileChangedSinceThePlanIsRefused(t *testing.T) {
	st, o := yard(t)
	dir := copyStarter(t)
	plan := build(t, st, load(t, dir), o)
	from := filepath.Join(dir, "roles", "builder.md")
	if err := os.WriteFile(from, []byte("another {{operator}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := pack.Answers{}
	for _, q := range plan.Questions {
		a[q.ID] = "claude"
	}
	if _, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: a, Actor: "me"}); err == nil || !strings.Contains(err.Error(), from+" changed since the plan was made") {
		t.Fatalf("applying a plan whose source changed: %v", err)
	}
}

// A pack's import writes its groups with PutGroup, with no check of the
// CLI's before it, so it meets the store's (yardr-3wt9): a group object
// with a config key nothing reads is refused there. A key of that kind
// that the yard's group holds already, from before the store refused one,
// is carried over as the yard has it and does not stop the import.
func TestApplyMeetsTheStoresConfigCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yard.db")
	old, err := store.Open(path, agents.Catalogue())
	if err != nil {
		t.Fatal(err)
	}
	if err := old.PutDepot(ctx, store.Depot{Name: "web", Base: "main"}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := old.PutGroup(ctx, store.Group{Name: "web-builders", Runner: "herdr", Config: map[string]any{"kind": "codex", "prompt": "/old/builder.md"}}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	storetest.OldConfig(t, path, "groups", "web-builders", "idel", "5m")
	st := storetest.Open(t, path)
	o := pack.Options{Depot: "web", Home: t.TempDir()}

	plan := build(t, st, load(t, starter), o)
	refused := false
	for i := range plan.Objects {
		if g := plan.Objects[i].Group; g != nil && g.Name != "web-builders" && g.Runner == "herdr" {
			g.Config["max-age"] = "6h"
			a := pack.Answers{}
			for _, q := range plan.Questions {
				a[q.ID] = map[string]string{"kind": "claude", "dir": t.TempDir()}[q.Field]
			}
			if _, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: a, Actor: "yardmaster"}); err == nil ||
				!strings.Contains(err.Error(), `group `+g.Name+`: unknown config key "max-age": nothing reads it. A group of the herdr runner takes:`) {
				t.Fatalf("an import of a group with a key nothing reads: %v", err)
			}
			delete(g.Config, "max-age")
			refused = true
			break
		}
	}
	if !refused {
		t.Fatal("the starter pack plans no herdr group but web-builders to try the refusal on")
	}

	plan = build(t, st, load(t, starter), o)
	res, err := pack.Apply(ctx, st, plan, pack.ApplyOptions{Answers: answered(t, plan, nil), Actor: "yardmaster"})
	if err == nil || !strings.Contains(err.Error(), "may record the import of pack starter") ||
		!strings.Contains(err.Error(), "applying again records nothing") {
		t.Fatalf("the import as yardmaster = %+v, %v; want it written and the record refused", res, err)
	}
	if res == nil || res.Recorded {
		t.Fatalf("result = %+v, want it unrecorded", res)
	}
	g, err := st.GetGroup(ctx, "web-builders")
	if err != nil || g.Config["idel"] != "5m" || g.Config["prompt"] != "/old/builder.md" {
		t.Fatalf("the group the import kept beside its old key: %+v, %v", g, err)
	}
}
