package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

func lineJSON(t *testing.T, f *fixture) web.Line {
	t.Helper()
	r := f.get("/line", "Accept", "application/json")
	var l web.Line
	if err := json.Unmarshal([]byte(r.body), &l); err != nil || r.status != 200 {
		t.Fatalf("/line: status %d, %v: %s", r.status, err, r.body)
	}
	return l
}

// waiting is what waits without its times, which no test can name.
func waiting(l web.Line) []web.Waiting { return timeless(l.Waits) }

// parked is what is parked, likewise.
func parked(l web.Line) []web.Waiting { return timeless(l.Parked) }

func timeless(ws []web.Waiting) []web.Waiting {
	out := []web.Waiting{}
	for _, w := range ws {
		if w.Since.IsZero() {
			w.Title += " (no since)"
		}
		w.Since = time.Time{}
		out = append(out, w)
	}
	return out
}

// listRowsOf are the rows of the list's section on the page at path, as the
// list says them, none where the page has no such section: Decide's list holds
// every wait in its Waits for you and the beads parked where they start, or the
// reader answered, in its folded Parked.
func listRowsOf(t *testing.T, f *fixture, path, section string) []testRow {
	t.Helper()
	for _, sec := range listSections(t, f.get(path).body) {
		if sec.title == section {
			return sec.rows
		}
	}
	return nil
}

// rowOf is the row of rows that opens the bead's page.
func rowOf(t *testing.T, rows []testRow, bead string) testRow {
	t.Helper()
	for _, r := range rows {
		if r.href == "/beads/"+bead {
			return r
		}
	}
	t.Fatalf("the list has no row for %s: %+v", bead, rows)
	return testRow{}
}

// reasonLine is the line of a wait's row that says why it waits, which stands
// after the bead's own line.
func reasonLine(r testRow) string {
	if len(r.lines) < 2 {
		return ""
	}
	return r.lines[1]
}

// A held bead waits for a person, with who held it and the reason they
// gave; unholding it takes it off again. The line shows it, home's aside
// the line alone, and every other page counts it in its header.
func TestWaitsHeld(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	must(t, f.st.SetSetting(ctx, "operator", "mckean", "me"))
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.CreateBead(ctx, store.NewBead{Title: "a window", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	if got := waiting(lineJSON(t, f)); len(got) != 0 {
		t.Fatalf("nothing is held, and these wait: %+v", got)
	}
	if !strings.Contains(listOf(t, f.get("/line").body), `<li class="wv-list__none">Nothing waits for you.</li>`) {
		t.Error("the list of a yard where nothing waits does not say so")
	}

	must(t, f.st.AddNote(ctx, b.ID, "the hinge is <missing>", "builder"))
	_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "builder")
	must(t, err)
	want := []web.Waiting{{Bead: b.ID, Title: "a door", Depot: "line", Stage: "new",
		Why: []web.Reason{{Kind: "held", Text: "held by builder", Note: "the hinge is <missing>"}}}}
	if got := waiting(lineJSON(t, f)); !reflect.DeepEqual(got, want) {
		t.Errorf("held\n got %+v\nwant %+v", got, want)
	}
	// The list's row names the bead by its title, the hold in red and the note
	// it was given with, whole, and the answer counts one held.
	row := rowOf(t, listRowsOf(t, f, "/line", "Waits for you"), b.ID)
	if row.name != "a door" || row.dot != "wait" || len(row.lines) != 2 || row.lines[0] != "held by builder" || row.lines[1] != "the hinge is <missing>" {
		t.Errorf("/line's row is %+v", row)
	}
	if sub := answerSection(t, f.get("/line").body); !strings.Contains(sub, "1 held.") {
		t.Errorf("/line's answer lacks the held bead: %s", sub)
	}
	// Every other page counts it in the rail's badge, and keeps no aside.
	for _, path := range []string{"/", "/sessions", "/crew", "/beads"} {
		body := f.get(path).body
		if tile := tileOf(t, body, "/"); !strings.Contains(tile, `title="1 waits for you"`) {
			t.Errorf("%s counts no held bead in its rail: %s", path, tile)
		}
		if strings.Contains(body, "<aside") {
			t.Errorf("%s keeps a column the window has no place for", path)
		}
	}
	// Home lists the bead in its centre, and Decide's list holds it as a
	// wait, by its title and its reason.
	home := f.get("/").body
	waits := section2(t, listSections(t, home), "Waits for you")
	if waits.count != "1" || len(waits.rows) != 1 || waits.rows[0].name != "a door" || waits.rows[0].lines[0] != "held by builder" ||
		waits.rows[0].lines[1] != "the hinge is <missing>" {
		t.Errorf("Decide's list does not hold the held bead: %+v", waits)
	}
	if strings.Contains(squash(stageOf(t, home)), `<article class="wait">`) {
		t.Error("/ has the waits panel in its stage")
	}

	// A hold with no note of its holder right before it has no reason to
	// show: someone else's note is not one.
	_, err = f.st.SetHold(ctx, b.ID, false, store.Expect{}, "me")
	must(t, err)
	if got := waiting(lineJSON(t, f)); len(got) != 0 {
		t.Errorf("the bead is no longer held, and these wait: %+v", got)
	}
	_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me")
	must(t, err)
	want[0].Why = []web.Reason{{Kind: "held", Text: "held by me"}}
	if got := waiting(lineJSON(t, f)); !reflect.DeepEqual(got, want) {
		t.Errorf("held again\n got %+v\nwant %+v", got, want)
	}

	// An ask and its answer are JSON in their notes. When one is the note
	// right before a hold, the aside says the ask by its first line.
	for _, c := range []struct {
		title, state, holder string
		answer               bool
	}{
		{"ask reason", "open", "yardmaster", false},
		{"answer reason", "answered", "mckean", true},
	} {
		asked, err := f.st.CreateBead(ctx, store.NewBead{Title: c.title, Stage: "new", Depot: "line"}, "me")
		must(t, err)
		seq, err := f.st.AddAsk(ctx, asked.ID, pickAsk(), "yardmaster")
		must(t, err)
		if c.answer {
			must(t, f.st.AddAnswer(ctx, asked.ID, store.Answer{Ask: seq, Option: 1}, "mckean"))
		}
		_, err = f.st.SetHold(ctx, asked.ID, true, store.Expect{}, c.holder)
		must(t, err)
		wantHead := fmt.Sprintf("ask %d · %s · to mckean · asked by yardmaster · ", seq, c.state)
		var reason *web.Reason
		for _, w := range waiting(lineJSON(t, f)) {
			if w.Bead == asked.ID && len(w.Why) > 0 {
				reason = &w.Why[0]
				break
			}
		}
		if reason == nil || reason.Kind != "held" || reason.Text != "held by "+c.holder ||
			!strings.HasPrefix(reason.Note, wantHead) || strings.ContainsAny(reason.Note, "\n{") {
			t.Errorf("%s: the hold reason is %+v, want its ask's first line from %q", asked.ID, reason, wantHead)
		}
		// The row stands once: the hold, then what was said with it, which is the
		// ask's first line; an ask still open puts its question there instead.
		row := rowOf(t, listRowsOf(t, f, "/line", "Waits for you"), asked.ID)
		wantSaid := wantHead
		if !c.answer {
			wantSaid = pickAsk().Question
		}
		if len(row.lines) < 2 || row.lines[0] != "held by "+c.holder || !strings.HasPrefix(row.lines[1], wantSaid) {
			t.Errorf("/line's row for %s is %+v, want the hold and %q", asked.ID, row, wantSaid)
		}
		var homeReason []web.Reason
		for _, row := range homeJSON(t, f, "/").Attention {
			if row.Bead == asked.ID {
				homeReason = row.Why
				break
			}
		}
		if reason == nil || len(homeReason) == 0 || !sameReason(*reason, homeReason[0]) {
			t.Errorf("home says the hold reason %+v, /line %+v, want the same", homeReason, reason)
		}
	}
}

// roundLoop makes a depot whose flow limits the way back from review to new
// at max rounds and sends the next to decide, which a manual group of the
// members works, and a bead in new; report reports an outcome for it.
func roundLoop(t *testing.T, f *fixture, max int, members ...string) (b *store.Bead, report func(outcome string)) {
	t.Helper()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "loop", Base: "origin/main"}, f.depotActor(ctx)))
	flow, err := store.ParseFlow(fmt.Sprintf("new -> review on done\nreview -> merged on done | new on changes max %d then decide | decide on question\ndecide -> new\nmerged terminal\n", max))
	must(t, err)
	flow.Name = "loop"
	_, err = f.st.PutFlow(ctx, *flow, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "loop", "", "loop", "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Members: members}, "me"))
	routes, err := f.st.Routes(ctx)
	must(t, err)
	hasDecisionRoute := false
	for _, route := range routes {
		if route.Stage == "decide" && route.Worker() == "decisions" {
			hasDecisionRoute = true
			break
		}
	}
	if !hasDecisionRoute {
		_, err = f.st.AddRoute(ctx, store.Route{Stage: "decide", Group: "decisions"}, "me")
		must(t, err)
	}
	b, err = f.st.CreateBead(ctx, store.NewBead{Title: "round and round", Stage: "new", Depot: "loop"}, "me")
	must(t, err)
	return b, func(outcome string) {
		t.Helper()
		_, err := f.st.Done(ctx, b.ID, outcome, "", store.Expect{}, "anna")
		must(t, err)
	}
}

// A bead in a stage routed to a manual group is parked for that group,
// claimed or not: nothing went wrong with it, so it is listed apart from
// what waits, and its claim is no session and no work in progress. A limit
// that sends it there says why beside the wait. One routed to an agent's
// group is not listed at all.
func TestWaitsManual(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Members: []string{"anna", "ben"}}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake"}, "me"))
	_, err := f.st.AddRoute(ctx, store.Route{Stage: "decide", Group: "decisions"}, "me")
	must(t, err)
	_, err = f.st.AddRoute(ctx, store.Route{Stage: "new", Group: "builders"}, "me")
	must(t, err)
	_, err = f.st.CreateBead(ctx, store.NewBead{Title: "built by an agent", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	unclaimed, err := f.st.CreateBead(ctx, store.NewBead{Title: "which colour", Stage: "decide", Depot: "line"}, "me")
	must(t, err)
	claimed, err := f.st.CreateBead(ctx, store.NewBead{Title: "which size", Stage: "decide", Depot: "line"}, "me")
	must(t, err)
	sess, err := f.st.Claim(ctx, claimed.ID, "decisions", "decide", "yard")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"manual": true}))

	why := []web.Reason{{Kind: "manual", Text: "waits for decisions (anna, ben) in decide"}}
	// The one that began to wait last comes first.
	want := []web.Waiting{
		{Bead: claimed.ID, Title: "which size", Depot: "line", Stage: "decide", Why: why},
		{Bead: unclaimed.ID, Title: "which colour", Depot: "line", Stage: "decide", Why: why},
	}
	l := lineJSON(t, f)
	if got := parked(l); !reflect.DeepEqual(got, want) {
		t.Errorf("parked\n got %+v\nwant %+v", got, want)
	}
	if got := waiting(l); len(got) != 0 {
		t.Errorf("nothing is held, blocked or stranded, and these wait: %+v", got)
	}
	if l.Sessions != 0 || l.Working != 0 {
		t.Errorf("the line has %d sessions and %d working, want none: a manual claim runs nothing", l.Sessions, l.Working)
	}
	// No flow tells where these beads start, so both stand among the waits,
	// each with the group it waits for; the numbers are the subline's and the
	// per-stage sums head Lines' list.
	waits := listRowsOf(t, f, "/line", "Waits for you")
	for _, b := range []*store.Bead{claimed, unclaimed} {
		if got := reasonLine(rowOf(t, waits, b.ID)); got != "waits for decisions (anna, ben) in decide" {
			t.Errorf("%s's row says %q", b.ID, got)
		}
	}
	if sub := squash(stageOf(t, f.get("/line").body)); !strings.Contains(sub, `<a href="/flows">3 open</a>`) {
		t.Errorf("/line's subline lacks the 3 open beads: %s", sub)
	}
	if head := regionOf(t, f.get("/flows").body, `<header class="wv-list__head" id="list-head">`, `</header>`); !strings.Contains(head, "decide 2") {
		t.Errorf("Lines' head lacks the open per stage: %s", head)
	}
	// Home has no aside: the waits stand in the list, and none of the line's
	// panels on the stage.
	home := squash(stageOf(t, f.get("/").body))
	for _, not := range []string{`<dt>open</dt>`, `Parked · 2`, `<article class="wait">`, `<aside`} {
		if strings.Contains(home, not) {
			t.Errorf("home has %q", not)
		}
	}
	// The subline prints no zero: nothing working and no session running is
	// shown by the absence of their parts.
	text := squash(stageOf(t, f.get("/line").body))
	for _, not := range []string{`0 moving`, ">0<"} {
		if strings.Contains(text, not) {
			t.Errorf("the page has %q", not)
		}
	}

	// A bead whose last move was a round limit sending it to the manual stage
	// says why beside the wait. Once it is moved on, or comes to the stage
	// another way, the line is gone.
	b, report := roundLoop(t, f, 3, "anna", "ben")
	manual := web.Reason{Kind: "manual", Text: "waits for decisions (anna, ben) in decide"}
	row := func() web.Waiting {
		t.Helper()
		for _, w := range parked(lineJSON(t, f)) {
			if w.Bead == b.ID {
				return w
			}
		}
		t.Fatalf("the line has no parked row for %s", b.ID)
		return web.Waiting{}
	}
	// Three rounds back to new are allowed; the fourth changes is sent on.
	report("done")
	for range 3 {
		report("changes")
		report("done")
	}
	for _, w := range parked(lineJSON(t, f)) {
		if w.Bead == b.ID {
			t.Fatalf("in review, and the bead has a parked row: %+v", w)
		}
	}
	report("changes")
	sent := web.Reason{Kind: "manual", Text: "sent to decide after 3 rounds back to new (changes from review)"}
	if got, want := row(), []web.Reason{manual, sent}; !reflect.DeepEqual(got.Why, want) {
		t.Errorf("sent on by the limit\n got %+v\nwant %+v", got.Why, want)
	}
	if got := waiting(lineJSON(t, f)); len(got) != 0 {
		t.Errorf("a bead in decide is a person's by design, and these wait: %+v", got)
	}
	if got, want := reasonLine(rowOf(t, listRowsOf(t, f, "/line", "Waits for you"), b.ID)), "waits for decisions (anna, ben) in decide · sent to decide after 3 rounds back to new (changes from review)"; got != want {
		t.Errorf("/line's row says %q, want %q", got, want)
	}

	// A person sends it back, it goes round again and the limit sends it on a
	// second time: the later move is the one the line explains.
	_, err = f.st.Advance(ctx, b.ID, "new", store.Expect{}, "anna")
	must(t, err)
	report("done")
	for range 3 {
		report("changes")
		report("done")
	}
	report("changes")
	if got, want := row(), []web.Reason{manual, sent}; !reflect.DeepEqual(got.Why, want) {
		t.Errorf("sent on a second time\n got %+v\nwant %+v", got.Why, want)
	}

	// A person moves it on and it asks a question to decide: it is in decide
	// again, but the limit did not send it.
	_, err = f.st.Advance(ctx, b.ID, "new", store.Expect{}, "anna")
	must(t, err)
	report("done")
	report("question")
	if got, want := row(), []web.Reason{manual}; !reflect.DeepEqual(got.Why, want) {
		t.Errorf("in decide by a question\n got %+v\nwant %+v", got.Why, want)
	}
}

// A bead parked in a manual stage that the viewer answered, declined or put
// aside an ask on leaves that viewer's home: its /line Parked row says the
// answer is in and names who moves it next. A round limit's reason stays
// beside it. Anyone else still counts it, with the old row, as does a bead
// with an open ask, one never asked, one whose asks their asker took back
// (nobody's answer), one in a sole-member or memberless group, and one the
// viewer answered but may not move.
func TestWaitsManualAnswered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	for _, u := range []string{"anna", "ben", "cid"} {
		must(t, func() error { _, err := f.st.AddUser(ctx, u, "mckean"); return err }())
	}
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Members: []string{"anna", "ben"}}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "solo", Runner: "manual", Members: []string{"anna"}}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "open", Runner: "manual"}, "me"))
	for _, rt := range []store.Route{{Stage: "decide", Group: "decisions"}, {Stage: "review", Group: "solo"}, {Stage: "queue", Group: "open"}} {
		_, err := f.st.AddRoute(ctx, rt, "me")
		must(t, err)
	}
	askTo := func(to string) store.Ask {
		a := pickAsk()
		a.To = to
		return a
	}
	answer := func(id string, to string, ans store.Answer) {
		t.Helper()
		seq := askOn(t, f, id, askTo(to))
		ans.Ask = seq
		must(t, f.st.AddAnswer(ctx, id, ans, to))
	}
	answered := askBead(t, f, "answered", "decide")
	answer(answered.ID, "anna", store.Answer{Option: 1})
	declined := askBead(t, f, "declined", "decide")
	answer(declined.ID, "anna", store.Answer{Decline: true, Words: "not mine"})
	aside := askBead(t, f, "aside", "decide")
	answer(aside.ID, "anna", store.Answer{Later: true})
	// A plain ask to each member, which its asker took back: nobody answered.
	takenBack := askBead(t, f, "taken back", "decide")
	for _, to := range []string{"anna", "ben"} {
		must(t, f.st.TakeBackAsk(ctx, takenBack.ID, askOn(t, f, takenBack.ID, askTo(to)), "asked in error", "yardmaster"))
	}
	open := askBead(t, f, "open", "decide")
	askOn(t, f, open.ID, askTo("anna"))
	quiet := askBead(t, f, "quiet", "decide")
	lone := askBead(t, f, "lone", "review")
	answer(lone.ID, "anna", store.Answer{Option: 1})
	free := askBead(t, f, "free", "queue")
	answer(free.ID, "anna", store.Answer{Option: 1})
	other := askBead(t, f, "other", "decide")
	answer(other.ID, "cid", store.Answer{Option: 1})

	anna, ben, cid := f.as(t, "anna"), f.as(t, "ben"), f.as(t, "cid")
	attn := func(v *fixture) map[string]bool {
		t.Helper()
		out := map[string]bool{}
		for _, w := range homeJSON(t, v, "/").Attention {
			out[w.Bead] = true
		}
		return out
	}
	parkedText := func(who string, v *fixture, id string) string {
		t.Helper()
		for _, w := range parked(lineJSON(t, v)) {
			if w.Bead == id {
				if len(w.Why) != 1 {
					t.Fatalf("%s's parked row for %s has %+v, want the manual reason alone", who, id, w.Why)
				}
				return w.Why[0].Text
			}
		}
		t.Fatalf("%s parks no %s", who, id)
		return ""
	}

	// anna's home lacks the answered, declined and put-aside beads; the rest
	// count, the one whose asks were taken back among them.
	if got, want := attn(anna), map[string]bool{open.ID: true, quiet.ID: true, lone.ID: true, free.ID: true, other.ID: true, takenBack.ID: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("anna's attention %v, want %v", got, want)
	}
	if h := homeJSON(t, anna, "/"); h.Answer() != "6 wait for you" {
		t.Errorf("anna's answer %q, want six waits", h.Answer())
	}
	if s := answerSection(t, anna.get("/").body); !strings.Contains(s, `<h1 class="wv-answer">6 wait for you</h1>`) {
		t.Errorf("anna's answer line is %q, want six waits", s)
	}
	// Her Parked rows for them say her answer is in and name ben.
	for _, id := range []string{answered.ID, declined.ID, aside.ID} {
		if got, want := parkedText("anna", anna, id), "you answered; waits for decisions (ben) in decide"; got != want {
			t.Errorf("anna's parked row for %s is %q, want %q", id, got, want)
		}
	}
	// The answered beads stand in the list's Parked, not among her waits, and
	// say whose move it is now.
	parkedRows := listRowsOf(t, anna, "/line", "Parked")
	if got := reasonLine(rowOf(t, parkedRows, answered.ID)); got != "you answered; waits for decisions (ben) in decide" {
		t.Errorf("anna's /line page says %q of the answered bead", got)
	}
	// No new field: the reason's JSON is kind, text and note, as before.
	var l struct {
		Parked []struct {
			Bead string           `json:"bead"`
			Why  []map[string]any `json:"why"`
		} `json:"parked"`
	}
	if err := json.Unmarshal([]byte(anna.get("/line", "Accept", "application/json").body), &l); err != nil {
		t.Fatal(err)
	}
	for _, w := range l.Parked {
		if w.Bead != answered.ID {
			continue
		}
		for k := range w.Why[0] {
			if k != "kind" && k != "text" && k != "note" {
				t.Errorf("the answered reason's JSON has %q", k)
			}
		}
	}

	// ben's home still counts every bead, with the old row.
	if got := attn(ben); len(got) != 9 {
		t.Errorf("ben's attention has %d beads, want nine", len(got))
	}
	if h := homeJSON(t, ben, "/"); h.Answer() != "9 wait for you" {
		t.Errorf("ben's answer %q, want nine waits", h.Answer())
	}
	for _, id := range []string{answered.ID, takenBack.ID} {
		if got, want := parkedText("ben", ben, id), "waits for decisions (anna, ben) in decide"; got != want {
			t.Errorf("ben's parked row for %s is %q, want %q", id, got, want)
		}
	}

	// A bead with an open ask still waits with its question.
	got := waiting(lineJSON(t, anna))
	found := false
	for _, w := range got {
		if w.Bead != open.ID {
			continue
		}
		found = true
		if len(w.Why) != 2 || w.Why[0].Kind != "manual" || w.Why[1].Kind != "ask" {
			t.Errorf("the open bead waits with %+v, want the manual reason and the ask", w.Why)
		}
	}
	if !found {
		t.Errorf("anna's waits %+v, without the open bead", got)
	}

	// A bead never asked, one whose asks were taken back (anna's too: it
	// is no answer of hers), and answered ones in a sole-member or
	// memberless group, keep the old row and count.
	for _, c := range []struct {
		id, want string
	}{
		{quiet.ID, "waits for decisions (anna, ben) in decide"},
		{takenBack.ID, "waits for decisions (anna, ben) in decide"},
		{lone.ID, "waits for solo (anna) in review"},
		{free.ID, "waits for open in queue"},
	} {
		if got := parkedText("anna", anna, c.id); got != c.want {
			t.Errorf("anna's parked row for %s is %q, want %q", c.id, got, c.want)
		}
	}

	// cid answered the other's ask but is no member of decisions: the old
	// row and count stand for them.
	if got := attn(cid); !got[other.ID] {
		t.Errorf("cid's attention %v, without the bead they answered", got)
	}
	if got, want := parkedText("cid", cid, other.ID), "waits for decisions (anna, ben) in decide"; got != want {
		t.Errorf("cid's parked row is %q, want %q", got, want)
	}

	// The Waiting call serves the parked half of that home to the people it
	// parks for, so a client that is not the web counts the same beads: less
	// the bead with an open ask to them, which is among their decisions, and
	// the stage of a group that does not list them (ben's home counts lone,
	// which only anna may move).
	for _, c := range []struct {
		who  string
		want map[string]bool
	}{
		{"anna", map[string]bool{quiet.ID: true, lone.ID: true, free.ID: true, other.ID: true, takenBack.ID: true}},
		{"ben", map[string]bool{answered.ID: true, declined.ID: true, aside.ID: true, open.ID: true, quiet.ID: true, free.ID: true, other.ID: true, takenBack.ID: true}},
		{"cid", map[string]bool{free.ID: true}},
	} {
		rows, err := f.st.Waiting(ctx, c.who, c.who, c.who)
		must(t, err)
		got := map[string]bool{}
		for _, r := range rows {
			got[r.Bead] = true
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s's Waiting %v, want %v", c.who, got, c.want)
		}
	}

	// A round limit's reason is part of the manual wait. The person who
	// answered the bead's ask still sees their answer and it remains out of
	// their home; a one-round limit says "round" in the singular.
	limited, report := roundLoop(t, f, 1, "anna", "ben")
	report("done")
	report("changes")
	report("done")
	report("changes")
	ask := askTo("anna")
	seq := askOn(t, f, limited.ID, ask)
	must(t, f.st.AddAnswer(ctx, limited.ID, store.Answer{Ask: seq, Option: 1}, "anna"))
	sent := web.Reason{Kind: "manual", Text: "sent to decide after 1 round back to new (changes from review)"}
	found = false
	for _, w := range parked(lineJSON(t, anna)) {
		if w.Bead == limited.ID {
			found = true
			want := []web.Reason{{Kind: "manual", Text: "you answered; waits for decisions (ben) in decide"}, sent}
			if !reflect.DeepEqual(w.Why, want) {
				t.Errorf("anna's limited parked row\n got %+v\nwant %+v", w.Why, want)
			}
		}
	}
	if !found {
		t.Errorf("anna's parked rows have no limited bead %s", limited.ID)
	}
	annaHas := false
	for _, w := range homeJSON(t, anna, "/").Attention {
		annaHas = annaHas || w.Bead == limited.ID
	}
	if annaHas {
		t.Error("anna answered the limited bead and it counts in her home")
	}
	benHas := false
	for _, w := range homeJSON(t, ben, "/").Attention {
		benHas = benHas || w.Bead == limited.ID
	}
	if !benHas {
		t.Error("ben answered nothing and the limited bead is left out of his home")
	}
	if got, want := reasonLine(rowOf(t, listRowsOf(t, anna, "/line", "Parked"), limited.ID)), "you answered; waits for decisions (ben) in decide · sent to decide after 1 round back to new (changes from review)"; got != want {
		t.Errorf("anna's /line page says %q of the answered bead, want %q", got, want)
	}
}

// sessionsJSON is the sessions page's data.
func sessionsJSON(t *testing.T, f *fixture) web.Sessions {
	t.Helper()
	r := f.get("/sessions", "Accept", "application/json")
	var s web.Sessions
	if err := json.Unmarshal([]byte(r.body), &s); err != nil || r.status != 200 {
		t.Fatalf("/sessions: status %d, %v: %s", r.status, err, r.body)
	}
	return s
}

// backlogYard has a manual group backlog that claimed n beads of its stage,
// as a dispatcher does for every bead a manual group has room for.
func backlogYard(t *testing.T, f *fixture, n int) []*store.Bead {
	t.Helper()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "backlog", Runner: "manual", Limit: n}, "me"))
	_, err := f.st.AddRoute(ctx, store.Route{Stage: "backlog", Group: "backlog"}, "me")
	must(t, err)
	var beads []*store.Bead
	for i := range n {
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: "idea " + string(rune('a'+i)), Stage: "backlog", Depot: "line"}, "me")
		must(t, err)
		sess, err := f.st.Claim(ctx, b.ID, "backlog", "backlog", "yard")
		must(t, err)
		must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"manual": true}))
		beads = append(beads, b)
	}
	return beads
}

// A yard where nothing is being built, its backlog claimed by a manual
// group, shows no work in progress: no bead working, no session running,
// nothing that waits. The claims are on the sessions page apart from the
// running sessions, and the beads are parked. JSON and page agree.
func TestManualClaimsOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	beads := backlogYard(t, f, 5)

	l := lineJSON(t, f)
	if l.Open != 5 || l.Working != 0 || l.Sessions != 0 || l.Held != 0 {
		t.Errorf("the line: %d open, %d working, %d held, %d sessions, want 5, 0, 0, 0", l.Open, l.Working, l.Held, l.Sessions)
	}
	if len(l.Waits) != 0 || l.Waits == nil {
		t.Errorf("waits %+v, want an empty list", l.Waits)
	}
	if got := parked(l); len(got) != 5 || got[0].Bead != beads[4].ID || got[4].Bead != beads[0].ID ||
		!reflect.DeepEqual(got[0].Why, []web.Reason{{Kind: "manual", Text: "waits for backlog in backlog"}}) {
		t.Errorf("parked %+v, want the five beads, the last claimed first", got)
	}
	if h := homeJSON(t, f, "/"); len(h.Depots) != 1 || h.Depots[0].Open != 5 || h.Depots[0].Working != 0 {
		t.Errorf("home: %+v, want one depot with 5 open and none working", h.Depots)
	}
	s := sessionsJSON(t, f)
	if len(s.Sessions) != 0 || s.Sessions == nil || len(s.Manual) != 5 {
		t.Fatalf("sessions %+v, want no running session and five manual claims", s)
	}
	for i, row := range s.Manual {
		if row.Bead != beads[i].ID || row.State != "claimed" || row.Runner != "manual" || row.Group != "backlog" || row.Blocked {
			t.Errorf("manual claim %d: %+v", i, row)
		}
	}

	// Each of the five stands once in the list, the last claimed first, and
	// the numbers are the subline's.
	rows := listRowsOf(t, f, "/line", "Waits for you")
	if len(rows) != 5 || rows[0].href != "/beads/"+beads[4].ID || rows[4].href != "/beads/"+beads[0].ID {
		t.Errorf("/line lists %+v, want the five parked beads, once each, the last claimed first", rows)
	}
	text := squash(stageOf(t, f.get("/line").body))
	if !strings.Contains(text, `<a href="/flows">5 open</a>`) {
		t.Errorf("/line's subline lacks the 5 open beads: %s", text)
	}
	// The subline prints no zero: no work in progress is shown by the
	// absence of its parts.
	for _, not := range []string{`0 moving`, ">0<"} {
		if strings.Contains(text, not) {
			t.Errorf("/line has %q", not)
		}
	}
	if strings.Contains(listOf(t, f.get("/line").body), "Nothing waits for you.") {
		t.Error("/line says nothing waits, over five parked beads")
	}
	// Every other page counts them in the rail's badge instead, and keeps no
	// aside; Decide's list holds them as waits.
	sessions := f.get("/sessions").body
	if tile := tileOf(t, sessions, "/"); !strings.Contains(tile, `title="5 wait for you"`) {
		t.Errorf("/sessions counts no parked bead in its rail: %s", tile)
	}
	if strings.Contains(sessions, "<aside") {
		t.Error("/sessions keeps a column the window has no place for")
	}
	// Home has no aside: the five parked beads stand in its centre's list
	// of waits and in Decide's, and the line's numbers are not on it.
	home := f.get("/").body
	if waits := section2(t, listSections(t, home), "Waits for you"); waits.count != "5" || len(waits.rows) != 5 {
		t.Errorf("Decide's list holds %+v, want the five parked beads", waits)
	}
	for _, not := range []string{`<dt>open</dt>`, `Parked · 5`, `<article class="wait">`, "<aside"} {
		if strings.Contains(squash(stageOf(t, home)), not) {
			t.Errorf(`/ has %q`, not)
		}
	}
	text = squash(f.get("/sessions").body)
	// No session runs, so the sentence stands where its rows would, and the
	// claims stand under their own head, the first claimed first, in a block
	// whose links leave the mode.
	if part := `<p class="wv-none">No session is running.</p><section class="wv-block"><h2 class="wv-sh"><span>Claimed by a manual group</span><span class="wv-sh__count">5</span></h2><ol hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list:outerHTML"><li><a class="wv-srow" href="/beads/` + beads[0].ID + `">`; !strings.Contains(text, part) {
		t.Errorf("/sessions lacks %q", part)
	}
	// A claim has no dot, says claimed, and shows no runner: it is always
	// manual, as the section it stands in already says.
	row := srowOf(text, beads[0].ID)
	if !strings.HasPrefix(row, `<a class="wv-srow" href="/beads/`+beads[0].ID+`"><span class="wv-dot"></span>`) ||
		!strings.Contains(row, `<span class="wv-id">`+beads[0].ID+`</span><span class="wv-sep">·</span><wbr>claimed<span class="wv-sep">·</span><wbr><span class="wv-nw">stage backlog</span><span class="wv-sep">·</span><wbr><span class="wv-nw">group backlog</span></span><br>`) {
		t.Errorf("/sessions draws a manual claim as %s", row)
	}
	if strings.Contains(text, "runner manual") {
		t.Error("/sessions repeats the manual section in every claim's runner")
	}
	if strings.Contains(text, "wv-dot--run") || strings.Contains(text, "·</span>running") {
		t.Error("/sessions shows a manual claim as running")
	}
}

// In a yard with agents' sessions and manual claims, the numbers count the
// agents' alone, and a parked bead something stops (a hold) waits with the
// rest, with every reason, and is parked no longer.
func TestManualClaimsMixed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	beads := backlogYard(t, f, 3)
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake", Limit: 5}, "me"))
	door, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	sess, err := f.st.Claim(ctx, door.ID, "builders", "new", "yard")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "door-new"}))
	// A session that is still starting counts too.
	window, err := f.st.CreateBead(ctx, store.NewBead{Title: "a window", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.Claim(ctx, window.ID, "builders", "new", "yard")
	must(t, err)
	_, err = f.st.SetHold(ctx, beads[1].ID, true, store.Expect{}, "me")
	must(t, err)

	l := lineJSON(t, f)
	if l.Open != 5 || l.Working != 2 || l.Sessions != 2 || l.Held != 1 {
		t.Errorf("the line: %d open, %d working, %d held, %d sessions, want 5, 2, 1, 2", l.Open, l.Working, l.Held, l.Sessions)
	}
	wantWaits := []web.Waiting{{Bead: beads[1].ID, Title: "idea b", Depot: "line", Stage: "backlog", Why: []web.Reason{
		{Kind: "held", Text: "held by me"}, {Kind: "manual", Text: "waits for backlog in backlog"}}}}
	if got := waiting(l); !reflect.DeepEqual(got, wantWaits) {
		t.Errorf("waits\n got %+v\nwant %+v", got, wantWaits)
	}
	if got := parked(l); len(got) != 2 || got[0].Bead != beads[2].ID || got[1].Bead != beads[0].ID {
		t.Errorf("parked %+v, want the two beads nothing stops", got)
	}
	if h := homeJSON(t, f, "/"); len(h.Depots) != 1 || h.Depots[0].Open != 5 || h.Depots[0].Working != 2 || h.Depots[0].Held != 1 {
		t.Errorf("home: %+v, want one depot with 5 open, 2 working, 1 held", h.Depots)
	}
	s := sessionsJSON(t, f)
	if len(s.Sessions) != 2 || s.Sessions[0].Bead != door.ID || s.Sessions[0].State != "running" ||
		s.Sessions[1].Bead != window.ID || s.Sessions[1].State != "starting" || len(s.Manual) != 2 {
		t.Errorf("sessions %+v, want the two agents' sessions and two manual claims: a hold ends the claim", s)
	}

	// The held bead stands first, with its holder and the group it waits for;
	// the two parked stand after it, and the numbers are the subline's: two
	// moving, five open, one held.
	rows := listRowsOf(t, f, "/line", "Waits for you")
	if len(rows) != 3 || rows[0].href != "/beads/"+beads[1].ID || rows[0].name != "idea b" ||
		rows[0].lines[0] != "held by me" || rows[1].href != "/beads/"+beads[2].ID || rows[2].href != "/beads/"+beads[0].ID {
		t.Errorf("/line lists %+v, want the held bead first and the two parked after it", rows)
	}
	text := squash(stageOf(t, f.get("/line").body))
	for _, part := range []string{`<a href="/sessions">2 moving</a>`, `<a href="/flows">5 open</a>`, "2 to decide, 1 held, both in the list."} {
		if !strings.Contains(text, part) {
			t.Errorf("/line lacks %q", part)
		}
	}
	// Every other page counts what waits in the rail's badge instead: the
	// held bead and the two parked, and no aside.
	sessions := f.get("/sessions").body
	if tile := tileOf(t, sessions, "/"); !strings.Contains(tile, `title="3 wait for you"`) {
		t.Errorf("/sessions counts no waiting bead in its rail: %s", tile)
	}
	if strings.Contains(sessions, "<aside") {
		t.Error("/sessions keeps a column the window has no place for")
	}
	text = squash(sessions)
	// Home has no aside: the line's numbers are the line page's alone.
	home := squash(stageOf(t, f.get("/").body))
	for _, not := range []string{`<dt><span class="dot working"></span>working</dt>`, `<article class="wait">`, "<aside"} {
		if strings.Contains(home, not) {
			t.Errorf("home has %q", not)
		}
	}
	claimed := `<section class="wv-block"><h2 class="wv-sh"><span>Claimed by a manual group</span><span class="wv-sh__count">2</span></h2>`
	if !strings.Contains(text, claimed) || strings.Contains(text, "No session is running.") ||
		strings.Index(text, `<a class="wv-srow" href="/beads/`+door.ID+`">`) > strings.Index(text, claimed) {
		t.Error("/sessions does not list the agents' sessions above the two manual claims")
	}
}

// blockedYard has one bead with a running session of an agent.
func blockedYard(t *testing.T, f *fixture) (*store.Bead, *store.Session) {
	t.Helper()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake", Limit: 5}, "me"))
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	sess, err := f.st.Claim(ctx, b.ID, "builders", "new", "yard")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "door-new"}))
	return b, sess
}

// A bead whose session the dispatcher reported blocked on a prompt waits
// for a person until the dispatcher reports it unblocked. Another
// session's report does not count.
func TestWaitsBlocked(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	b, sess := blockedYard(t, f)
	must(t, f.st.LogDispatch(ctx, b.ID, "session_blocked", "yard", map[string]any{"session": "another"}))
	if got := waiting(lineJSON(t, f)); len(got) != 0 {
		t.Fatalf("a running session waits for nobody, and these wait: %+v", got)
	}
	must(t, f.st.LogDispatch(ctx, b.ID, "session_blocked", "yard", map[string]any{"session": sess.ID}))
	want := []web.Waiting{{Bead: b.ID, Title: "a door", Depot: "line", Stage: "new",
		Why: []web.Reason{{Kind: "blocked", Text: "its session waits on a prompt: door-new"}}}}
	if got := waiting(lineJSON(t, f)); !reflect.DeepEqual(got, want) {
		t.Errorf("blocked\n got %+v\nwant %+v", got, want)
	}
	row := rowOf(t, listRowsOf(t, f, "/line", "Waits for you"), b.ID)
	if len(row.lines) != 1 || row.lines[0] != "its session waits on a prompt: door-new" || !strings.Contains(listOf(t, f.get("/line").body), `<span class="wv-fault">its session waits on a prompt: door-new</span>`) {
		t.Errorf("the list lacks the blocked session, in red: %+v", row)
	}
	must(t, f.st.LogDispatch(ctx, b.ID, "session_unblocked", "yard", map[string]any{"session": sess.ID}))
	if got := waiting(lineJSON(t, f)); len(got) != 0 {
		t.Errorf("the session is unblocked, and these wait: %+v", got)
	}
}

// strandedStore says one bead is stranded, or fails to say.
type strandedStore struct {
	store.API
	bead string
	err  error
}

func (s strandedStore) Stranded(context.Context, string) ([]store.StrandedBead, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []store.StrandedBead{{Bead: s.bead, Stage: "limbo", Flow: "code"}}, nil
}

// A bead stranded in a stage its flow lacks waits for a person. When the
// yard cannot say which are stranded, the column still shows the rest.
func TestWaitsStranded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "limbo", Depot: "line"}, "me")
	must(t, err)
	held, err := f.st.CreateBead(ctx, store.NewBead{Title: "a window", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.SetHold(ctx, held.ID, true, store.Expect{}, "me")
	must(t, err)
	read := func(st store.API) web.Line {
		t.Helper()
		srv, err := web.NewServer(st, filepath.Join(t.TempDir(), ".depot"), nil)
		must(t, err)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://localhost/line", nil)
		req.Header.Set("Accept", "application/json")
		srv.ServeHTTP(rec, req)
		var l web.Line
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil || rec.Code != 200 {
			t.Fatalf("status %d, %v: %s", rec.Code, err, rec.Body)
		}
		return l
	}
	heldWait := web.Waiting{Bead: held.ID, Title: "a window", Depot: "line", Stage: "new",
		Why: []web.Reason{{Kind: "held", Text: "held by me"}}}
	want := []web.Waiting{heldWait, {Bead: b.ID, Title: "a door", Depot: "line", Stage: "limbo",
		Why: []web.Reason{{Kind: "stranded", Text: "stranded in limbo, a stage flow code lacks"}}}}
	if got := waiting(read(strandedStore{API: f.st, bead: b.ID})); !reflect.DeepEqual(got, want) {
		t.Errorf("stranded\n got %+v\nwant %+v", got, want)
	}
	if got := waiting(read(strandedStore{API: f.st, err: errors.New("repository is unavailable")})); !reflect.DeepEqual(got, []web.Waiting{heldWait}) {
		t.Errorf("with no word on the stranded\n got %+v\nwant the held bead alone", got)
	}
}

// A page holds one stream, which the shell opens: a browser allows a host few
// connections. A page says what its stream carries besides the unnamed
// message per event the leaf hears, and an error page opens none.
func TestShellStream(t *testing.T) {
	t.Parallel()
	// Peers are the operator's to read, so the walk is the operator's,
	// made one before the yard is staged and its head read.
	f, _ := talkFixture(t, "ada")
	must(t, f.st.SetSetting(context.Background(), store.OperatorSetting, "ada", "ada"))
	f.populate()
	head, err := f.st.Head(context.Background())
	must(t, err)
	three := "/beads/" + f.byTitle()["three"].ID
	formats := map[string]string{"/": "home", "/flows": "card", "/beads": "seq", three: "seq",
		"/depots": "seq", "/groups": "seq", "/routes": "seq", "/peers": "seq", "/deps": "seq", "/limits": "seq",
		"/line": "", "/sessions": "", "/crew": "", "/config": ""}
	for path, format := range formats {
		body := f.get(path).body
		if want := shellOpen(head, format); !strings.Contains(body, want) {
			t.Errorf("%s lacks %s", path, want)
		}
		if n := strings.Count(body, "sse-connect"); n != 1 {
			t.Errorf("%s opens %d streams, want one", path, n)
		}
	}
	if body := f.get("/nowhere").body; strings.Contains(body, "sse-connect") || !strings.Contains(body, `<div class="wv-shell" hx-boost="true"`) {
		t.Errorf("an error page opens a stream, or lacks the shell's swap attributes: %s", body)
	}
}

// Every page counts what waits for the visitor in the rail's Decide badge,
// which the list's head says too, nothing at zero. The count is home's: a bead
// parked where its flow starts is on /line, not in the count. An error page
// stands bare, with none.
func TestRailCountsWhatWaits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	fl, err := store.ParseFlow("backlog -> new\nnew -> merged\nmerged terminal\n")
	must(t, err)
	fl.Name = "code"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "line", "", "code", "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "triage", Runner: "manual"}, "me"))
	_, err = f.st.AddRoute(ctx, store.Route{Stage: "backlog", Group: "triage"}, "me")
	must(t, err)
	pages := []string{"/", "/line", "/beads", "/sessions", "/crew", "/flows", "/config", "/depots", "/groups", "/routes",
		"/deps", "/limits", "/talk"}
	badge := func(path string) string {
		t.Helper()
		return tileOf(t, f.get(path).body, "/")
	}

	// Nothing waits: no count, and no aside anywhere.
	for _, path := range pages {
		if tile := badge(path); strings.Contains(tile, "wv-badge") {
			t.Errorf("%s counts waits, and nothing waits: %s", path, tile)
		}
	}
	missing := f.get("/beads/no-such-bead")
	if missing.status != http.StatusNotFound || strings.Contains(railOf(t, missing.body), "wv-badge") {
		t.Errorf("missing bead: status %d, want 404 with no count", missing.status)
	}

	// A bead parked where its flow starts waits on no one in particular: it
	// is on /line, not in the count, as home needs no one.
	parked, err := f.st.CreateBead(ctx, store.NewBead{Title: "an idea", Stage: "backlog", Depot: "line"}, "me")
	must(t, err)
	if s := answerSection(t, f.get("/").body); !strings.Contains(s, `<h1 class="wv-answer wv-answer--clear">Nothing needs you.</h1>`) {
		t.Errorf("home needs %q, want nothing: the bead is parked where it starts", s)
	}
	if tile := badge("/beads"); strings.Contains(tile, "wv-badge") {
		t.Errorf("/beads counts the backlog, and home needs no one: %s", tile)
	}
	if rows := listRowsOf(t, f, "/line", "Parked"); len(rows) != 1 || rows[0].href != "/beads/"+parked.ID {
		t.Errorf("/line's list holds %+v of the parked, want the bead parked where it starts", rows)
	}

	// One waits: every page counts it, singular, and the list's head says
	// so where it stands in Decide.
	door, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.SetHold(ctx, door.ID, true, store.Expect{}, "me")
	must(t, err)
	one := `title="1 waits for you" aria-label="Decide, 1 waits for you"`
	for _, path := range append(pages, "/beads/"+door.ID, "/beads/"+parked.ID) {
		if tile := badge(path); !strings.Contains(tile, one) || !strings.Contains(tile, `<span class="wv-badge" aria-hidden="true">1</span>`) {
			t.Errorf("%s counts no waiting bead in its rail: %s", path, tile)
		}
	}
	for _, path := range []string{"/", "/line", "/beads/" + door.ID} {
		if head := regionOf(t, f.get(path).body, `<header class="wv-list__head" id="list-head">`, `</header>`); !strings.Contains(head, "1 waits for you") {
			t.Errorf("%s: the list's head does not say what waits: %s", path, head)
		}
	}
	// An error page stands under no mode, even where something waits.
	if r := f.get("/beads/no-such-bead"); r.status != http.StatusNotFound || strings.Contains(railOf(t, r.body), "wv-badge") {
		t.Errorf("the missing bead's page counts: %d %s", r.status, railOf(t, r.body))
	}

	// Two wait: the count is plural.
	window, err := f.st.CreateBead(ctx, store.NewBead{Title: "a window", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.SetHold(ctx, window.ID, true, store.Expect{}, "me")
	must(t, err)
	if tile := badge("/beads"); !strings.Contains(tile, `title="2 wait for you"`) {
		t.Errorf("/beads does not count two waiting beads: %s", tile)
	}
	for _, path := range pages {
		if text := squash(f.get(path).body); strings.Contains(text, "<aside") || strings.Contains(text, "needs-count") {
			t.Errorf("%s keeps a column or a count the window has no place for", path)
		}
	}
}

func itoa(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// The sessions page lists the running sessions with their bead, stage,
// group, runner, state and age, as JSON and as the page, and a manual
// group's claim apart from them; an ended session is not on it.
func TestSessions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	var empty web.Sessions
	r := f.get("/sessions", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &empty); err != nil || r.status != 200 || empty.Sessions == nil || len(empty.Sessions) != 0 {
		t.Fatalf("an empty yard: status %d, %v, %+v, want an empty list", r.status, err, empty)
	}
	if text := f.get("/sessions").body; !strings.Contains(text, "No session is running.") {
		t.Error("the page of a yard with no session does not say so")
	}

	b, sess := blockedYard(t, f)
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual"}, "me"))
	other, err := f.st.CreateBead(ctx, store.NewBead{Title: "which <colour>", Stage: "decide", Depot: "line"}, "me")
	must(t, err)
	manual, err := f.st.Claim(ctx, other.ID, "decisions", "decide", "yard")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, manual.ID, map[string]any{"manual": true}))
	gone, err := f.st.CreateBead(ctx, store.NewBead{Title: "ended", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	ended, err := f.st.Claim(ctx, gone.ID, "builders", "new", "yard")
	must(t, err)
	must(t, f.st.EndSession(ctx, ended.ID, "died", "the pane closed"))

	read := func() web.Sessions {
		t.Helper()
		var got web.Sessions
		r := f.get("/sessions", "Accept", "application/json")
		if err := json.Unmarshal([]byte(r.body), &got); err != nil || r.status != 200 {
			t.Fatalf("status %d, %v: %s", r.status, err, r.body)
		}
		for _, rows := range [][]web.SessionRow{got.Sessions, got.Manual} {
			for i := range rows {
				s := &rows[i]
				if s.StartedAt.IsZero() || s.Age == "" {
					t.Errorf("session %s has no start or age: %+v", s.ID, s)
				}
				s.StartedAt, s.Age = time.Time{}, ""
			}
		}
		// The list is the list command's (TestSessionsList).
		if got.Head == 0 || len(got.List) != len(got.Sessions)+len(got.Manual) {
			t.Errorf("head %d, and %d listed of %d live", got.Head, len(got.List), len(got.Sessions)+len(got.Manual))
		}
		got.List, got.Head = nil, 0
		return got
	}
	want := web.Sessions{Sessions: []web.SessionRow{
		{ID: sess.ID, Bead: b.ID, Title: "a door", Stage: "new", Group: "builders", Runner: "fake door-new", State: "running"},
	}, Manual: []web.SessionRow{
		{ID: manual.ID, Bead: other.ID, Title: "which <colour>", Stage: "decide", Group: "decisions", Runner: "manual", State: "claimed"},
	}}
	if got := read(); !reflect.DeepEqual(got, want) {
		t.Errorf("sessions\n got %+v\nwant %+v", got, want)
	}
	text := squash(f.get("/sessions").body)
	for _, part := range []string{
		// The stage draws its own head, and its rows open the beads' pages,
		// which stand in Decide.
		`<article class="wv-page" data-kind="list"><header class="wv-head"><h1 class="wv-title">Sessions</h1><p class="wv-meta">1 running, the oldest first</p></header>`,
		`<ol hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list:outerHTML"><li><a class="wv-srow" href="/beads/` + b.ID + `"><span class="wv-dot wv-dot--run"></span>`,
		// Its title, then what and where, then whose it is and when it began.
		`<span class="wv-srow__name">a door</span><br><span class="wv-srow__meta"><span class="wv-id">` + b.ID + `</span><span class="wv-sep">·</span><wbr>running<span class="wv-sep">·</span><wbr><span class="wv-nw">stage new</span><span class="wv-sep">·</span><wbr><span class="wv-nw">group builders</span><span class="wv-sep">·</span><wbr><span class="wv-nw">runner fake door-new</span></span><br>`,
		`<span class="wv-nw">session <span class="wv-id">` + sess.ID + `</span></span><span class="wv-sep">·</span><wbr><span class="wv-nw">started <time datetime=`,
		`<section class="wv-block"><h2 class="wv-sh"><span>Claimed by a manual group</span><span class="wv-sh__count">1</span></h2>`,
		`<span class="wv-srow__name">which &lt;colour&gt;</span>`,
		`<a class="wv-row" href="/sessions" aria-current="page">`,
	} {
		if !strings.Contains(text, part) {
			t.Errorf("the page lacks %q", part)
		}
	}
	if strings.Contains(text, gone.ID) || strings.Contains(text, "No session is running.") {
		t.Error("the page shows the ended session, or says none runs")
	}

	// Each fact of a session's lines is one unit and holds no space for a narrow
	// line to break at, so every separator carries the place, a <wbr>: without
	// it a phone's line cannot break at all and runs out of the window.
	row0 := srowOf(text, b.ID)
	if seps, breaks := strings.Count(row0, `<span class="wv-sep">·</span>`), strings.Count(row0, `<span class="wv-sep">·</span><wbr>`); seps == 0 || seps != breaks {
		t.Errorf("%d of %d separators of a session's lines are a place to break: %s", breaks, seps, row0)
	}

	// A session blocked on a prompt says so.
	must(t, f.st.LogDispatch(ctx, b.ID, "session_blocked", "yard", map[string]any{"session": sess.ID}))
	want.Sessions[0].Blocked = true
	if got := read(); !reflect.DeepEqual(got, want) {
		t.Errorf("blocked\n got %+v\nwant %+v", got, want)
	}
	row := srowOf(squash(f.get("/sessions").body), b.ID)
	if !strings.HasPrefix(row, `<a class="wv-srow" href="/beads/`+b.ID+`"><span class="wv-dot wv-dot--fault"></span>`) ||
		!strings.Contains(row, `<span class="wv-sep">·</span><wbr><span class="wv-fault">blocked</span><span class="wv-sep">·</span><wbr><span class="wv-nw">stage new</span>`) {
		t.Errorf("the page does not show the blocked session as blocked: %s", row)
	}
}

// The sessions page shows each session's effective login beside its bead
// originator, as JSON and as the page: the group's login_for, else the
// operator's (yardr-q0xd).
func TestSessionsLogin(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.st.AddUser(ctx, "jo", "jo"); err != nil {
		t.Fatal(err)
	}
	must(t, f.st.SetSetting(ctx, store.OperatorSetting, "jo", "jo"))
	if _, err := f.st.AddUser(ctx, "hal", "jo"); err != nil {
		t.Fatal(err)
	}
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "jo"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 5}, "jo"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "hal-builders", Runner: "herdr",
		Config: map[string]any{"kind": "claude", "login_for": "hal"}, Limit: 5}, "jo"))
	work := func(title, group string) string {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Stage: "new", Depot: "line", For: "hal"}, "jo")
		must(t, err)
		sess, err := f.st.Claim(ctx, b.ID, group, "new", "yard")
		must(t, err)
		must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "door-new"}))
		return b.ID
	}
	subsidy := work("hal's door on the operator's login", "builders")
	personal := work("hal's door on hal's login", "hal-builders")

	var got web.Sessions
	r := f.get("/sessions", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &got); err != nil || r.status != 200 {
		t.Fatalf("status %d, %v: %s", r.status, err, r.body)
	}
	type pair struct{ originator, login string }
	byBead := map[string]pair{}
	for _, s := range got.Sessions {
		byBead[s.Bead] = pair{s.Originator, s.Login}
	}
	if byBead[subsidy] != (pair{"hal", "jo"}) || byBead[personal] != (pair{"hal", "hal"}) {
		t.Errorf("sessions logins = %v, want subsidy hal/jo and personal hal/hal", byBead)
	}
	text := squash(f.get("/sessions").body)
	for id, part := range map[string]string{
		subsidy:  `<span class="wv-srow__meta"><span class="wv-nw">originator hal</span><span class="wv-sep">·</span><wbr><span class="wv-nw">login jo</span><span class="wv-sep">·</span><wbr><span class="wv-nw">session `,
		personal: `<span class="wv-srow__meta"><span class="wv-nw">originator hal</span><span class="wv-sep">·</span><wbr><span class="wv-nw">login hal</span><span class="wv-sep">·</span><wbr><span class="wv-nw">session `,
	} {
		if row := srowOf(text, id); !strings.Contains(row, part) {
			t.Errorf("the row of %s lacks %q: %s", id, part, row)
		}
	}
}

// The sessions page shows each session's bead originator beside its group,
// as JSON and as the page.
func TestSessionsOriginator(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.st.AddUser(ctx, "jo", "jo"); err != nil {
		t.Fatal(err)
	}
	must(t, f.st.SetSetting(ctx, store.OperatorSetting, "jo", "jo"))
	if _, err := f.st.AddUser(ctx, "hal", "jo"); err != nil {
		t.Fatal(err)
	}
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "jo"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake", Limit: 5}, "jo"))
	work := func(title, for_ string) string {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Stage: "new", Depot: "line", For: for_}, "jo")
		must(t, err)
		sess, err := f.st.Claim(ctx, b.ID, "builders", "new", "yard")
		must(t, err)
		must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "door-new"}))
		return b.ID
	}
	jo := work("jo's door", "")
	hal := work("hal's door", "hal")

	var got web.Sessions
	r := f.get("/sessions", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &got); err != nil || r.status != 200 {
		t.Fatalf("status %d, %v: %s", r.status, err, r.body)
	}
	byBead := map[string]string{}
	for _, s := range got.Sessions {
		byBead[s.Bead] = s.Originator
	}
	if len(byBead) != 2 || byBead[jo] != "jo" || byBead[hal] != "hal" {
		t.Errorf("sessions originators = %v, want jo's jo and hal's hal", byBead)
	}
	text := squash(f.get("/sessions").body)
	for id, part := range map[string]string{
		jo:  `<span class="wv-srow__meta"><span class="wv-nw">originator jo</span><span class="wv-sep">·</span><wbr>`,
		hal: `<span class="wv-srow__meta"><span class="wv-nw">originator hal</span><span class="wv-sep">·</span><wbr>`,
	} {
		if row := srowOf(text, id); !strings.Contains(row, part) {
			t.Errorf("the row of %s lacks %q: %s", id, part, row)
		}
	}
}

// How long ago, in its two largest units.
func TestAge(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		-time.Second:                    "0s",
		42 * time.Second:                "42s",
		12*time.Minute + 3*time.Second:  "12m",
		3 * time.Hour:                   "3h",
		3*time.Hour + 4*time.Minute:     "3h 4m",
		48 * time.Hour:                  "2d",
		51*time.Hour + 59*time.Minute:   "2d 3h",
		59*time.Second + time.Second/2:  "1m",
		59*time.Minute + 59*time.Second: "59m",
	} {
		if got := web.Age(d); got != want {
			t.Errorf("age(%v) = %q, want %q", d, got, want)
		}
	}
	// A card made of an id alone has no time, and so no age: measured from the
	// zero time it would read in the hundred thousands of days.
	if got := (web.Card{ID: "x"}).Age(); got != "" {
		t.Errorf("the age of a card with no time = %q, want none", got)
	}
	if got := (web.Card{ID: "x", UpdatedAt: time.Now().Add(-3 * time.Hour)}).Age(); got != "3h" {
		t.Errorf("the age of a card changed three hours ago = %q, want 3h", got)
	}
}

// The crew page lists each crew member, whether it runs and since when,
// and how many events of its inbox it has not read. What its journal says
// is not on it.
func TestCrew(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	var empty web.Crew
	r := f.get("/crew", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &empty); err != nil || r.status != 200 || empty.Crew == nil || len(empty.Crew) != 0 {
		t.Fatalf("an empty yard: status %d, %v, %+v, want an empty list", r.status, err, empty)
	}
	if text := f.get("/crew").body; !strings.Contains(text, "This yard has no crew.") {
		t.Error("the page of a yard with no crew does not say so")
	}

	for _, name := range []string{"yardmaster", "zed"} {
		_, err := f.st.PutCrew(ctx, store.CrewMember{Name: name, Runner: "herdr", Enabled: true,
			Config: map[string]any{"kind": "claude", "model": "opus"}}, "me")
		must(t, err)
	}
	must(t, f.st.CrewStarted(ctx, "yardmaster", map[string]any{"agent": "yardmaster"}))
	must(t, f.st.Mail(ctx, "yardmaster", "the tests are red", "builder"))
	must(t, f.st.Mail(ctx, "yardmaster", "and still red", "builder"))
	_, err := f.st.Journal(ctx, "yardmaster", store.NoteJournal, "dear diary", "yardmaster")
	must(t, err)
	must(t, f.st.SetCrewEnabled(ctx, "zed", false, "", "me"))

	var got web.Crew
	r = f.get("/crew", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &got); err != nil || r.status != 200 {
		t.Fatalf("status %d, %v: %s", r.status, err, r.body)
	}
	if len(got.Crew) != 2 || got.Crew[0].Since == nil || got.Crew[0].Up == "" {
		t.Fatalf("got %+v, want two crew members, the first running since some time", got)
	}
	got.Crew[0].Since, got.Crew[0].Up = nil, ""
	// The kind is in agent, and a field of its own for a client.
	head, err := f.st.Head(ctx)
	must(t, err)
	if got.Crew[0].Name != "yardmaster" || got.Crew[0].State != "running" || !got.Crew[0].Running || !got.Crew[0].Enabled ||
		got.Crew[0].Agent != "herdr · claude · opus" || got.Crew[0].Kind != "claude" || got.Crew[0].Unread != 2 ||
		got.Crew[1].Name != "zed" || got.Crew[1].State != "stopped" || got.Crew[1].Agent != "herdr · claude · opus" || got.Crew[1].Kind != "claude" || got.Crew[1].Enabled {
		t.Errorf("crew members: %+v", got.Crew)
	}
	if got.Head != head {
		t.Errorf("crew head %d, want %d", got.Head, head)
	}
	text := squash(f.get("/crew").body)
	for _, part := range []string{
		`<h2>yardmaster</h2><span class="state"><span class="dot running"></span><span>running</span></span><span class="item-count">2 unread</span>`,
		`<h2>zed</h2><span class="state"><span>stopped</span> · disabled</span></header>`,
		`<dt>agent</dt><dd>herdr · claude · opus</dd>`,
		`<dt>running since</dt><dd><time datetime="`,
	} {
		if !strings.Contains(text, part) {
			t.Errorf("the page lacks %q", part)
		}
	}
	if got := marked(railOf(t, f.get("/crew").body), "aria-current", "true"); len(got) != 1 || got[0] != "/crew" {
		t.Errorf("the crew page opens the tiles %v, want Crew's", got)
	}
	if strings.Count(text, "running since") != 1 {
		t.Error("the stopped crew member is shown running since some time")
	}
	for _, not := range []string{"dear diary", "the tests are red"} {
		if strings.Contains(text, not) || strings.Contains(r.body, not) {
			t.Errorf("the page shows %q: a crew member's journal and mail are its own", not)
		}
	}

	// Reading the inbox takes the count down.
	page, err := f.st.Inbox(ctx, "yardmaster", 0)
	must(t, err)
	must(t, f.st.AckInbox(ctx, "yardmaster", page.Last))
	if text := squash(f.get("/crew").body); !strings.Contains(text, `<h2>yardmaster</h2>`) || strings.Contains(text, "2 unread") {
		t.Error("the yardmaster read its inbox, and the page still counts 2 unread")
	}

	// While a daemon_lost event stands with no start after it, the
	// running crew member reads unknown since the daemon's last pass,
	// with no live dot and no running-since; the stopped one reads on.
	stopped := time.Now().Add(-time.Hour)
	if _, _, err := f.own.NoteDaemonLost(ctx, stopped); err != nil {
		t.Fatal(err)
	}
	want := store.UnknownSince(stopped)
	r = f.get("/crew", "Accept", "application/json")
	var lost web.Crew
	if err := json.Unmarshal([]byte(r.body), &lost); err != nil || r.status != 200 {
		t.Fatalf("status %d, %v: %s", r.status, err, r.body)
	}
	if len(lost.Crew) != 2 || lost.Crew[0].State != want || lost.Crew[0].Running || !lost.Crew[0].Lost ||
		lost.Crew[0].Since != nil || lost.Crew[0].Up != "" || lost.Crew[1].State != "stopped" || lost.Crew[1].Lost {
		t.Fatalf("lost crew: %+v", lost.Crew)
	}
	if text := squash(f.get("/crew").body); !strings.Contains(text, `<span>`+want+`</span>`) ||
		strings.Contains(text, "running since") || strings.Contains(text, "dot running") {
		t.Errorf("the page of a lost daemon lacks the unknown state, or keeps a live dot or a running-since")
	}
}

// busyStore is a yard in which something happens while a page reads it:
// each time the depots are listed, a bead gets a note.
type busyStore struct {
	store.API
	bead string
}

func (s busyStore) Depots(ctx context.Context) ([]store.Depot, error) {
	if err := s.API.AddNote(ctx, s.bead, "meanwhile", "me"); err != nil {
		return nil, err
	}
	return s.API.Depots(ctx)
}

// A page's stream starts after the head its handler read before it read
// the yard, not after one read once the page is built: an event that
// happens in between is in neither the page nor a stream that starts
// later, and a card that moved then would stay where it was.
func TestStreamStartsBeforeThePageWasRead(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	srv, err := web.NewServer(busyStore{API: f.st, bead: f.byTitle()["three"].ID}, f.home, nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	defer shut(ts)
	for _, path := range []string{"/flows", "/beads", "/sessions", "/crew", "/depots", "/limits"} {
		head, err := f.st.Head(ctx)
		must(t, err)
		res, err := http.Get(ts.URL + path)
		must(t, err)
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		must(t, err)
		after, err := f.st.Head(ctx)
		must(t, err)
		if after == head {
			t.Fatalf("%s: nothing happened while the page was read", path)
		}
		if want := `sse-connect="/events?after=` + itoa(head); !strings.Contains(string(body), want+`"`) && !strings.Contains(string(body), want+`&amp;`) {
			t.Errorf("%s: the stream does not start after %d, the head before the page was read (now %d)", path, head, after)
		}
	}
}

// A weighty mapping card, on /line's stage, shows the weight first and the
// decline path with the terminal command instead of the pick buttons, as its
// bead's page and the home page's do. A weighty secret card shows the weight
// with what the ask is for and the decline path, and a routine card keeps its
// picks.
func TestWaitsWeightyAsk(t *testing.T) {
	t.Parallel()
	f, b, seq := mappingActYard(t)
	ask := strconv.FormatUint(seq, 10)
	text := oneCard(t, f.get("/line").body)
	for _, want := range []string{
		`<a class="wv-id" href="/beads/` + b.ID + `">` + b.ID + `</a>`,
		`<p class="wv-ask__weight">WEIGHTY · as-user: lets calls or words count as a person&#39;s: a pick says which user U777 in talk is, and a delivery from them is then heard as that user&#39;s</p>`,
		`This card is weighty and this window has no gate. Approve it in footplate, or in a terminal on the yard's machine:`,
		`Approve it with <code>yardr bead answer ` + b.ID + ` ` + ask + ` &lt;n&gt;</code>`,
		`<span class="wv-opt__n">1</span><span class="wv-opt__b"><span class="wv-opt__t">ada</span>`,
		`<span class="wv-opt__n">2</span><span class="wv-opt__b"><span class="wv-opt__t">mckean</span>`,
		`hx-preserve="true" name="words"`,
		laterButton,
		declineButton,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/line lacks %q", want)
		}
	}
	if strings.Contains(text, `name="option"`) {
		t.Errorf("the weighty wait offers its picks:\n%s", text)
	}

	f2, b2, seq2 := secretActYard(t)
	secret := oneCard(t, f2.get("/line").body)
	for _, want := range []string{
		`<p class="wv-ask__weight">WEIGHTY · credential: gives a credential or an address to the yard: CRM_TOKEN, for the source crm (https://crm.example.com)</p>`,
		`a secret: CRM_TOKEN, for the source crm (https://crm.example.com)`,
		`This card is weighty and this window has no gate. Approve it in footplate, or in a terminal on the yard's machine:`,
		`Give it with <code>yardr bead answer ` + b2.ID + ` ` + strconv.FormatUint(seq2, 10) + ` --secret</code>`,
		`<button class="wv-btn" type="submit" name="action" value="later">Later</button>`,
		`<button class="wv-btn" type="submit" name="action" value="decline" title="Say no to all options. The asker finds another way.">Decline</button>`,
	} {
		if !strings.Contains(secret, want) {
			t.Errorf("/line lacks %q", want)
		}
	}
	for _, foreign := range []string{`name="option"`, `type="password"`, `value="give"`} {
		if strings.Contains(secret, foreign) {
			t.Errorf("the secret wait shows %q:\n%s", foreign, secret)
		}
	}

	// A routine card's wait keeps its picks and shows no weight.
	f3, _ := talkFixture(t, "jo")
	actYard(t, f3, pickAsk())
	routine := oneCard(t, f3.get("/line").body)
	for _, want := range pickOptions[:2] {
		if !strings.Contains(routine, want) {
			t.Errorf("/line lacks %q", want)
		}
	}
	for _, foreign := range []string{`class="wv-ask__weight"`, "WEIGHTY", "no gate"} {
		if strings.Contains(routine, foreign) {
			t.Errorf("the routine wait shows %q:\n%s", foreign, routine)
		}
	}
}
