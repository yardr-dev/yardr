package web_test

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/eta"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// stocked gives the yard a depot with a flow of outcomes, a running session,
// a train, and more beads on one platform than stand there, and a depot with
// no flow:
//
//	shop  code beads: backlog 12 (two a train's), new 2 (one running, the
//	      train), decide 2; bug beads: none on triage
//	loose one bead in stage inbox
func stocked(t *testing.T, f *fixture) (run, w1, w2, train, six *store.Bead) {
	t.Helper()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "shop", Base: "origin/main"}, f.depotActor(ctx)))
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "loose", Base: "origin/main"}, f.depotActor(ctx)))
	flow := func(name, typ, text string) {
		t.Helper()
		fl, err := store.ParseFlow(text)
		must(t, err)
		fl.Name = name
		_, err = f.st.PutFlow(ctx, *fl, "me")
		must(t, err)
		must(t, f.st.SetFlowPointer(ctx, "shop", typ, name, "me"))
	}
	flow("code", "", "backlog -> new | decide\n"+
		"new -> review on done | decide on question | backlog\n"+
		"review -> approved on done | new on changes | decide on question\n"+
		"approved -> merged on done | new on failed | decide on question\n"+
		"decide -> backlog | new\n"+
		"merged terminal\n")
	flow("bugs", "bug", "triage -> fixed\nfixed terminal\n")
	must(t, f.st.PutGroup(ctx, store.Group{Name: "muse-builders", Runner: "herdr", Limit: 5}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Limit: 5}, "me"))
	for _, rt := range []store.Route{{Stage: "new", Group: "muse-builders"}, {Stage: "backlog", Group: "decisions"}, {Stage: "decide", Group: "decisions"}} {
		_, err := f.st.AddRoute(ctx, rt, "me")
		must(t, err)
	}
	bead := func(title, typ, stage string) *store.Bead {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Type: typ, Stage: stage, Depot: "shop"}, "me")
		must(t, err)
		return b
	}
	for i := 1; i <= 10; i++ {
		bead(fmt.Sprintf("stock %d", i), "task", "backlog")
	}
	// The train's children are not the newest two: standing together is
	// then the row's doing, not the order's.
	w1 = bead("wagon one", "task", "backlog")
	run = bead("at work", "task", "new")
	bead("pick one", "task", "decide")
	bead("pick two", "task", "decide")
	w2 = bead("wagon two", "task", "backlog")
	six, err := f.st.CreateBead(ctx, store.NewBead{Title: "six", Type: "task", Stage: "inbox", Depot: "loose"}, "me")
	must(t, err)
	train, err = f.st.CreateBead(ctx, store.NewBead{Title: "the shed", Type: store.TypeTrain, Stage: "new", Depot: "shop"}, "me")
	must(t, err)
	must(t, f.st.AddDep(ctx, train.ID, w1.ID, store.DepParent, "", "", "me"))
	must(t, f.st.AddDep(ctx, train.ID, w2.ID, store.DepParent, "", "", "me"))
	sess, err := f.st.Claim(ctx, run.ID, "muse-builders", "new", "dispatcher")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "at work"}))
	return run, w1, w2, train, six
}

// The yard panel, ?panel=1, is the Lines list beside the stage now, and
// /line's home mode, ?home=1, is the home page's own column: both answer as
// the page without the parameter, in HTML and as JSON, and a parameter that
// said no panel is no panel.
func TestPanelAndHomeParametersAnswerAsThePage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	stocked(t, f)
	for _, pair := range [][2]string{{"/flows?panel=1", "/flows"}, {"/flows?panel=0", "/flows"}, {"/line?home=1", "/line"}} {
		with, plain := f.get(pair[0]), f.get(pair[1])
		if with.status != 200 || plain.status != 200 {
			t.Fatalf("%s: status %d, %s: %d", pair[0], with.status, pair[1], plain.status)
		}
		// The leaf of a live page reads the address it was asked at, the
		// parameter with it, so it is the one difference.
		leaf := regexp.MustCompile(`<i class="wv-live"[^>]*></i>`)
		if a, b := sameWhole(leaf.ReplaceAllString(with.body, "")), sameWhole(leaf.ReplaceAllString(plain.body, "")); a != b {
			t.Errorf("%s does not answer as %s:\n%s\nwant\n%s", pair[0], pair[1], a, b)
		}
		wj, pj := f.get(pair[0], "Accept", "application/json"), f.get(pair[1], "Accept", "application/json")
		if wj.status != 200 || wj.body != pj.body {
			t.Errorf("the JSON of %s is not the JSON of %s", pair[0], pair[1])
		}
	}
	var a, b web.Flows
	f.getJSON("/flows", &a)
	f.getJSON("/flows?panel=1", &b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("?panel=1 decodes as %+v, /flows as %+v", b, a)
	}
}

// /flows is the page of cards: the stream moves each bead's card, a row of
// the flow list, between the stages, so it has no leaf, and Flows is the row of
// the Lines list that stands open. The yard panel's markup is gone.
func TestFlowsIsTheCardPage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, _, _, _, six := stocked(t, f)
	r := f.get("/flows")
	for _, want := range []string{
		`format=card`,
		`<a class="wv-row" href="/flows" aria-current="page">`,
		`<h3 class="wv-group__head"><span>off the flow</span></h3>`,
		`<a class="wv-srow" href="/beads/` + six.ID + `"`,
	} {
		if !strings.Contains(r.body, want) {
			t.Errorf("/flows lacks %s", want)
		}
	}
	for _, not := range []string{"panel-svg", `id="panel"`, "off the line:", "format=seq", "shell wide", "wv-live"} {
		if strings.Contains(r.body, not) {
			t.Errorf("/flows has %q", not)
		}
	}
	// The flows page still stages every bead: backlog holds twelve.
	var flows web.Flows
	f.getJSON("/flows", &flows)
	var codeFlow web.FlowColumn
	for _, depot := range flows.Depots {
		if depot.Name == "shop" {
			codeFlow = depot.Flows[0]
		}
	}
	if codeFlow.Name != "code" {
		t.Fatalf("shop's flow is %+v, want code", codeFlow)
	}
	for _, st := range codeFlow.Stages {
		if st.Stage == "backlog" && len(st.Beads) != 12 {
			t.Fatalf("backlog holds %d beads, want 12", len(st.Beads))
		}
	}
}

// kindsYard holds one of every blocked kind section 3.1 names: held,
// gave up, stalled, blocked on a prompt, waiting on a dependency, back on
// failed or changes, and a stranded bead off the line.
func kindsYard(t *testing.T, f *fixture) (held, gave, stall, block, dep, back, holder *store.Bead) {
	t.Helper()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "shop", Base: "origin/main"}, f.depotActor(ctx)))
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "loose", Base: "origin/main"}, f.depotActor(ctx)))
	fl, err := store.ParseFlow("backlog -> new | decide\n" +
		"new -> review on done | decide on question | backlog\n" +
		"review -> approved on done | new on changes | decide on question\n" +
		"approved -> merged on done | new on failed | decide on question\n" +
		"decide -> backlog | new\n" +
		"merged terminal\n")
	must(t, err)
	fl.Name = "code"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "shop", "", "code", "me"))
	// Off the line: loose has no flow, so limbo stands there with its
	// stage named, as a stranded bead's does.
	_, err = f.st.CreateBead(ctx, store.NewBead{Title: "stranded", Type: "task", Stage: "limbo", Depot: "loose"}, "me")
	must(t, err)
	must(t, f.st.PutGroup(ctx, store.Group{Name: "muse-builders", Runner: "herdr", Limit: 5}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Limit: 5}, "me"))
	for _, rt := range []store.Route{{Stage: "new", Group: "muse-builders"}, {Stage: "backlog", Group: "decisions"}, {Stage: "decide", Group: "decisions"}} {
		_, err := f.st.AddRoute(ctx, rt, "me")
		must(t, err)
	}
	bead := func(title, stage string) *store.Bead {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Type: "task", Stage: stage, Depot: "shop"}, "me")
		must(t, err)
		return b
	}
	claim := func(b *store.Bead, stage string) *store.Session {
		t.Helper()
		sess, err := f.st.Claim(ctx, b.ID, "muse-builders", stage, "dispatcher")
		must(t, err)
		must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": b.ID + "-new"}))
		return sess
	}
	// New holds a dependency-blocked bead beside the running and
	// stalled beads there.
	back = bead("back on changes", "review")
	_, err = f.st.Done(ctx, back.ID, "changes", "", store.Expect{}, "me")
	must(t, err)
	run := bead("running", "new")
	claim(run, "new")
	stall = bead("stalled", "new")
	stalled := claim(stall, "new")
	must(t, f.st.LogDispatch(ctx, stall.ID, "session_stalled", "yard", map[string]any{"session": stalled.ID}))
	holder = bead("holding", "backlog")
	dep, err = f.st.CreateBead(ctx, store.NewBead{Title: "waiting", Type: "task", Stage: "new", Depot: "shop", BlockedBy: []string{holder.ID}}, "me")
	must(t, err)
	// A held bead there is a person's turn, which the work hides.
	heldNew := bead("held while built", "new")
	_, err = f.st.SetHold(ctx, heldNew.ID, true, store.Expect{}, "me")
	must(t, err)
	// Review is unrouted: held, gave up and blocked stand there, one
	// fault with two turns.
	held = bead("held", "review")
	_, err = f.st.SetHold(ctx, held.ID, true, store.Expect{}, "me")
	must(t, err)
	gave = bead("gave up", "review")
	_, err = f.st.SetHold(ctx, gave.ID, true, store.Expect{}, "yard")
	must(t, err)
	must(t, f.st.LogDispatch(ctx, gave.ID, "gave_up", "yard", map[string]any{"stage": "review", "failures": 3}))
	block = bead("blocked", "review")
	blocked := claim(block, "review")
	must(t, f.st.LogDispatch(ctx, block.ID, "session_blocked", "yard", map[string]any{"session": blocked.ID}))
	// Decide holds a parked bead; approved is unrouted and holds a bead
	// that only waits behind another.
	bead("parked", "decide")
	_, err = f.st.CreateBead(ctx, store.NewBead{Title: "waits alone", Type: "task", Stage: "approved", Depot: "shop", BlockedBy: []string{holder.ID}}, "me")
	must(t, err)
	return held, gave, stall, block, dep, back, holder
}

// The served flows view is /flows as served: the same depots each with
// its flows, each flow its stages in travel order with the same group and
// beads, the same Elsewhere rows, and the travelling totals over the same
// beads.
func TestFlowsCallMatchesFlowsPage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	kindsYard(t, f)
	ctx := context.Background()
	var page web.Flows
	f.getJSON("/flows", &page)
	board, err := eta.FlowsViaStore(ctx, f.st, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Depots) != len(page.Depots) {
		t.Fatalf("%d depots served, page has %d", len(board.Depots), len(page.Depots))
	}
	travelling, unassigned := 0, []string{}
	beads, err := f.st.ListBeads(ctx, store.BeadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range beads {
		if b.Stage == "backlog" {
			continue
		}
		travelling++
		if b.Depot == nil {
			unassigned = append(unassigned, b.ID)
		}
	}
	if board.Travelling != travelling {
		t.Errorf("travelling = %d, want %d open beads outside backlog", board.Travelling, travelling)
	}
	if len(board.Unassigned) != len(unassigned) {
		t.Errorf("%d unassigned rows, want %d beads with no depot", len(board.Unassigned), len(unassigned))
	}
	ids := func(cards []web.Card) []string {
		var out []string
		for _, c := range cards {
			out = append(out, c.ID)
		}
		return out
	}
	rows := func(beads []store.FlowBead) []string {
		var out []string
		for _, b := range beads {
			out = append(out, b.Bead)
		}
		return out
	}
	for i, depot := range page.Depots {
		served := board.Depots[i]
		if served.Name != depot.Name || served.Kind != depot.Kind || served.Error != depot.Error {
			t.Errorf("depot %d = %+v, page has %+v", i, served, depot)
			continue
		}
		want := 0
		for _, b := range beads {
			if b.Depot != nil && *b.Depot == depot.Name && b.Stage != "backlog" {
				want++
			}
		}
		if served.Travelling != want {
			t.Errorf("%s travelling = %d, want %d", depot.Name, served.Travelling, want)
		}
		if len(served.Flows) != len(depot.Flows) {
			t.Errorf("%s: %d flows served, page has %d", depot.Name, len(served.Flows), len(depot.Flows))
			continue
		}
		for j, col := range depot.Flows {
			got := served.Flows[j]
			if got.Name != col.Name || got.Type != col.Type || got.Source != col.Source {
				t.Errorf("%s flow %d = %+v, page has %+v", depot.Name, j, got, col)
				continue
			}
			if len(got.Stages) != len(col.Stages) {
				t.Errorf("%s %s: %d stages served, page has %d", depot.Name, col.Name, len(got.Stages), len(col.Stages))
				continue
			}
			for k, st := range col.Stages {
				stage := got.Stages[k]
				if stage.Stage != st.Stage || stage.Group != st.Group || stage.Human != st.Human {
					t.Errorf("%s %s %s served as %+v, page has %+v", depot.Name, col.Name, st.Stage, stage, st)
				}
				if !reflect.DeepEqual(rows(stage.Beads), ids(st.Beads)) {
					t.Errorf("%s %s %s beads = %v, page has %v", depot.Name, col.Name, st.Stage, rows(stage.Beads), ids(st.Beads))
				}
				for l, b := range stage.Beads {
					if b.Title != st.Beads[l].Title || b.Working != st.Beads[l].Working {
						t.Errorf("%s served as %+v, page card %+v", b.Bead, b, st.Beads[l])
					}
				}
			}
		}
		if !reflect.DeepEqual(rows(served.Elsewhere), ids(depot.Elsewhere)) {
			t.Errorf("%s elsewhere = %v, page has %v", depot.Name, rows(served.Elsewhere), ids(depot.Elsewhere))
		}
	}
}
