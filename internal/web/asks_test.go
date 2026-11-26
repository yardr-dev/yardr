package web_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"html"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/web"
)

// askFixture is a yard with an operator and a depot, ready for asks.
func askFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t)
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
	must(t, f.st.SetSetting(ctx, "operator", "mckean", "me"))
	return f
}

func askBead(t *testing.T, f *fixture, title, stage string, labels ...string) *store.Bead {
	t.Helper()
	b, err := f.st.CreateBead(context.Background(), store.NewBead{
		Title: title, Stage: stage, Depot: "home", Labels: labels,
	}, "me")
	must(t, err)
	return b
}

func askOn(t *testing.T, f *fixture, id string, ask store.Ask) uint64 {
	t.Helper()
	seq, err := f.st.AddAsk(context.Background(), id, ask, "yardmaster")
	must(t, err)
	return seq
}

func pickAsk() store.Ask {
	return store.Ask{
		Question: "Move the links now?",
		To:       "mckean",
		Context: []string{
			"The links file declares the new source.",
			"The old source goes away Friday.",
		},
		Options: []store.AskOption{
			{Label: "Move them now", Consequence: "all links point at the new source tonight"},
			{Label: "After Friday", Consequence: "keep the old source until the release is out"},
		},
		Recommend: 1,
		Why:       "the test passed",
	}
}

// A bead with an open ask and no other wait stands in the home list: the
// question first, the bead small, the recommendation and the age faint.
// It counts as a need, not a fault.
func TestHomeNeedsAsk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	askOn(t, f, b.ID, pickAsk())

	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 || h.Attention[0].Bead != b.ID {
		t.Fatalf("attention %+v, want the asked bead", h.Attention)
	}
	w := h.Attention[0]
	if len(w.Why) != 1 || w.Why[0].Kind != "ask" || w.Why[0].Text != "Move the links now?" {
		t.Fatalf("why %+v, want the ask's question", w.Why)
	}
	wantNote := "The links file declares the new source.\nThe old source goes away Friday."
	if w.Why[0].Note != wantNote {
		t.Errorf("note %q, want the ask's context", w.Why[0].Note)
	}
	// No new field: the reason's JSON is kind, text and note, as before,
	// and the waiting bead carries no topic.
	var l struct {
		Waits []struct {
			Why []map[string]any `json:"why"`
		} `json:"waits"`
		Attn []map[string]any `json:"-"`
	}
	r := f.get("/line", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &l); err != nil {
		t.Fatal(err)
	}
	for k := range l.Waits[0].Why[0] {
		if k != "kind" && k != "text" && k != "note" {
			t.Errorf("the ask reason's JSON has %q", k)
		}
	}
	var home map[string]any
	r = f.get("/", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &home); err != nil {
		t.Fatal(err)
	}
	attn, _ := home["attention"].([]any)
	if len(attn) != 1 {
		t.Fatalf("attention has %d rows, want one", len(attn))
	}
	for k := range attn[0].(map[string]any) {
		switch k {
		case "bead", "title", "depot", "stage", "since", "why":
		default:
			t.Errorf("the waiting bead's JSON has %q", k)
		}
	}

	s := answerSection(t, f.get("/").body)
	if !strings.HasPrefix(s, "1 needs you.</p>") {
		t.Errorf("the answer is %q, want one need and no fault", s)
	}
	want := `<a href="/beads/` + b.ID + `" class="ask"><span class="entry-title ask-q">Move the links now?</span>` +
		`<span class="separator" aria-hidden="true">·</span>` +
		`<span class="ask-meta"><span class="bead">` + b.ID + `</span>` +
		`<span class="separator" aria-hidden="true">·</span><span class="faint">`
	if !strings.Contains(s, want) {
		t.Errorf("the row lacks %q", want)
	}
	if !strings.Contains(s, " ago</span></span>") {
		t.Error("the ask row's age lacks its unit of time")
	}
	row, _, _ := strings.Cut(s, `<div class="need-ask">`)
	if strings.Contains(row, "1. Move them now") {
		t.Error("the ask row repeats the recommendation shown in its detail")
	}
	for _, not := range []string{"held", "a door", "All waiting"} {
		if strings.Contains(s, not) {
			t.Errorf("the ask row has %q", not)
		}
	}
	_ = ctx
}

// Under its one-line row the home ask stands whole, as on its bead's
// page: the context lines, each option with its consequence, and the
// recommendation labelled. The row above it links to the bead.
func TestHomeNeedsAskDetail(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	if body := f.get("/").body; strings.Contains(body, "pill") {
		t.Errorf("a bead with no ask carries a pill: %s", body)
	}
	askOn(t, f, b.ID, pickAsk())

	s := answerSection(t, f.get("/").body)
	for _, want := range []string{
		`<a href="/beads/` + b.ID + `" class="ask">`,
		`<div class="need-ask">`,
		`<p class="ask-head"><a href="/beads/` + b.ID + `"><span class="dot chip-`,
		`depot · bead ` + b.ID + ` · Everything else</a></p>`,
		`<div class="ask-question"><p>Move the links now?</p><span class="asked">asked `,
		`<p class="ask-asker">asked by yardmaster</p>`,
		`<p class="entry-note">The links file declares the new source.</p>`,
		`<p class="entry-note">The old source goes away Friday.</p>`,
		`<li><span>1. Move them now</span><span class="faint"> — all links point at the new source tonight</span><span class="recommended">recommended: the test passed</span></li>`,
		`<li><span>2. After Friday</span><span class="faint"> — keep the old source until the release is out</span></li>`,
		// The yard's own last pick, the shape a card stored before
		// yardr-17bj keeps: it stands as a plain row, recommended by
		// nobody.
		`<li><span>3. You decide</span><span class="faint"> — leave it to yardmaster</span></li>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the home ask lacks %q", want)
		}
	}
	if strings.Contains(s, `<p class="entry-note">1. Move them now</p>`) {
		t.Error("the home ask shows the recommendation unlabelled")
	}
	for _, foreign := range []string{"Recommended:", "Recommends", "to approve:"} {
		if strings.Contains(s, foreign) {
			t.Errorf("the home ask shows the old wording %q", foreign)
		}
	}
}

// A bead that both waits and carries an open ask stands once, fault first:
// the held lead, then the question. It counts once, as a fault.
func TestHomeNeedsAskAndFault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	_, err := f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me")
	must(t, err)
	askOn(t, f, b.ID, pickAsk())

	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 {
		t.Fatalf("attention has %d rows, want one", len(h.Attention))
	}
	w := h.Attention[0]
	if len(w.Why) != 2 || w.Why[0].Kind != "held" || w.Why[1].Kind != "ask" {
		t.Fatalf("why %+v, want the hold first and the ask after", w.Why)
	}
	s := answerSection(t, f.get("/").body)
	if !strings.HasPrefix(s, "1 needs you. 1 fault.</p>") {
		t.Errorf("the answer is %q: the bead counts once, as a fault", s)
	}
	want := `<a href="/beads/` + b.ID + `" class="ask fault"><span class="dot held"></span><span class="held">held</span>` +
		`<span class="separator" aria-hidden="true">·</span>` +
		`<span class="entry-title ask-q">Move the links now?</span>`
	if !strings.Contains(s, want) {
		t.Errorf("the row lacks %q", want)
	}
	text := squash(f.get("/line").body)
	heldAt, askAt := strings.Index(text, "held by me"), strings.Index(text, "Move the links now?")
	if heldAt < 0 || askAt < 0 || heldAt > askAt {
		t.Error("/line does not name the hold first and the question after")
	}
}

// /line shows the same asks whole, as the bead page does: the context
// lines, each option with its consequence, and the recommendation
// labelled. The bead id links to the bead.
func TestLineAskContext(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	askOn(t, f, b.ID, pickAsk())

	got := waiting(lineJSON(t, f))
	if len(got) != 1 || len(got[0].Why) != 1 || got[0].Why[0].Kind != "ask" {
		t.Fatalf("/line waits %+v, want the ask", got)
	}
	text := squash(f.get("/line").body)
	for _, want := range []string{
		`<a class="bead" href="/beads/` + b.ID + `">` + b.ID + `</a>`,
		`depot · bead ` + b.ID + ` · Everything else</a></p>`,
		`<div class="ask-question"><p>Move the links now?</p><span class="asked">asked `,
		`<p class="ask-asker">asked by yardmaster</p>`,
		`<p class="entry-note">The links file declares the new source.</p>`,
		`<p class="entry-note">The old source goes away Friday.</p>`,
		`<li><span>1. Move them now</span><span class="faint"> — all links point at the new source tonight</span><span class="recommended">recommended: the test passed</span></li>`,
		`<li><span>2. After Friday</span><span class="faint"> — keep the old source until the release is out</span></li>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/line lacks %q", want)
		}
	}
	// The card stands folded under its question with its age: the row a
	// line of many waits is read by. The summary's question gives way to
	// the card's own once it is open, and the page keeps no aside to say
	// the card again.
	fold := regexp.MustCompile(`<details class="wait-ask" id="ask-` + regexp.QuoteMeta(b.ID) + `-\d+"><summary><span class="fold-closed">Move the links now\?<span class="asked">asked [^<]+</span></span><span class="fold-open">Fold</span></summary>`)
	if !fold.MatchString(text) {
		t.Errorf("/line lacks the folded ask row:\n%s", text)
	}
	if strings.Contains(text, "<details class=\"wait-ask\" open") {
		t.Error("/line opens an ask card nobody opened")
	}
	if n := strings.Count(text, "Move the links now?"); n != 2 {
		t.Errorf("/line says the question %d times, want twice: the row and its card", n)
	}
	// An open card's row is clipped away, never display: none, so the
	// summary's accessible name keeps the question and several open cards
	// are told apart by a screen reader; the visible word stays Fold.
	for _, rule := range []string{
		".wait-ask[open] > summary .fold-closed { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }",
		".wait-ask > summary .fold-open { display: none; color: var(--faint); }",
	} {
		css, err := os.ReadFile("assets/yard.css")
		must(t, err)
		if !strings.Contains(string(css), rule) {
			t.Errorf("yard.css lacks %q", rule)
		}
	}
	for _, foreign := range []string{"Recommended:", "Recommends", "to approve:"} {
		if strings.Contains(text, foreign) {
			t.Errorf("/line shows the old wording %q", foreign)
		}
	}
}

// The row does not duplicate recommendations. An ask with no options and
// one with options but no recommendation both leave the detail to speak.
func TestHomeNeedsAskRecommend(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	words := askBead(t, f, "words", "new")
	askOn(t, f, words.ID, store.Ask{
		Question: "What should the greeting say?", To: "mckean",
		Context: []string{"the greeting."},
	})
	plain := askBead(t, f, "plain", "new")
	askOn(t, f, plain.ID, store.Ask{
		Question: "Which colour?", To: "mckean", Context: []string{"the door."},
		Options: []store.AskOption{
			{Label: "Red", Consequence: "paint it red"},
			{Label: "Blue", Consequence: "paint it blue"},
		},
	})
	s := answerSection(t, f.get("/").body)
	if strings.Contains(s, "words wanted") {
		t.Error("the ask row repeats the words-wanted detail")
	}
	// The plain ask's row carries no recommendation, but its options
	// stand with their consequences.
	rows := strings.Split(s, `<li><a href="/beads/`)
	found := false
	for _, row := range rows[1:] {
		id, _, _ := strings.Cut(row, `"`)
		if id != plain.ID {
			continue
		}
		found = true
		if strings.Contains(row, "Recommended:") {
			t.Errorf("the unrecommended row names a recommendation: %q", row)
		}
		for _, want := range []string{
			`<li><span>1. Red</span><span class="faint"> — paint it red</span></li>`,
			`<li><span>2. Blue</span><span class="faint"> — paint it blue</span></li>`,
		} {
			if !strings.Contains(row, want) {
				t.Errorf("the unrecommended row lacks %q", want)
			}
		}
	}
	if !found {
		t.Error("the plain ask stands in no row")
	}
}

// The web stays flat and shows the bead's first topic label small where
// there is one, and the card's head names it.
func TestHomeNeedsAskTopic(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new", "topic:linear-out", "area:web")
	askOn(t, f, b.ID, pickAsk())
	s := answerSection(t, f.get("/").body)
	want := `<span class="ask-meta"><span class="bead">` + b.ID + `</span><span class="label"> linear-out</span>`
	if !strings.Contains(s, want) {
		t.Errorf("the row lacks %q", want)
	}
	if strings.Contains(s, "topic:") || strings.Contains(s, "area:") {
		t.Error("the row shows the raw label")
	}
	nolabel := askBead(t, f, "no label", "new")
	askOn(t, f, nolabel.ID, pickAsk())
	s = answerSection(t, f.get("/").body)
	rows := strings.Split(s, `<li><a href="/beads/`)
	for _, row := range rows[1:] {
		id, _, _ := strings.Cut(row, `"`)
		if id != nolabel.ID {
			continue
		}
		if !strings.Contains(row, `<span class="bead">`+nolabel.ID+`</span>`) {
			t.Errorf("the labelless row lacks its linked bead id")
		}
		if strings.Contains(row, `<span class="label">`) {
			t.Errorf("the labelless row has a topic label: %q", row)
		}
	}
	// The card's head names the bead's topic, without its prefix, and
	// links to the bead's page; a bead with no topic label heads
	// Everything else.
	for _, want := range []string{
		`<p class="ask-head"><a href="/beads/` + b.ID + `">`,
		`depot · bead ` + b.ID + ` · linear-out</a></p>`,
		`depot · bead ` + nolabel.ID + ` · Everything else</a></p>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the card head lacks %q", want)
		}
	}
}

// A stage card put now reads whole on Home and its bead's page: the frozen
// bead header, the moves, and no You decide. Its head names the bead's
// topic, never the stage the yard put it for. A card put before keeps its
// You decide pick: the partial renders stored options either way, and a
// hand ask stores the same You-decide-last shape, pinned in
// TestHomeNeedsAskDetail (the store's TestStageAnswerYouDecide pins the old
// card's stored shape).
func TestStageCardNewShape(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, "flows", "work"), 0o755))
	must(t, os.WriteFile(filepath.Join(home, "flows", "work", "review.md"), []byte(`Review the diff.

## Ask
kind: choice
question: Where should this bead go next?
context:
- The builder asked for a decision; the question is in their note.
- The branch is ready to review either way.
recommend: 1
why: The question is small and the branch is green.
done: The bead moves to approved with your words as the note.
changes: The bead goes back for changes.
`), 0o644))
	api, own := storetest.NewOwnedIn(t, home)
	srv, err := web.NewServer(api, home, nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: api, own: own, ts: ts, home: home}
	f.signIn(srv, srv.ActAs("mckean"))
	ctx := context.Background()
	must(t, api.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "mckean"))
	must(t, api.SetSetting(ctx, store.OperatorSetting, "mckean", "mckean"))
	must(t, api.SetSetting(ctx, store.NameSetting, "depot", "mckean"))
	fl, err := store.ParseFlow("new -> review\nreview -> review | approved on done | new on changes\napproved -> merged\nmerged terminal\n")
	must(t, err)
	fl.Name = "work"
	_, err = api.PutFlow(ctx, *fl, "mckean")
	must(t, err)
	must(t, api.SetFlowPointer(ctx, "home", "", "work", "mckean"))
	must(t, api.PutGroup(ctx, store.Group{Name: "reviewers", Runner: "manual", Members: []string{"mckean"}}, "mckean"))
	_, err = api.AddRoute(ctx, store.Route{Stage: "review", Group: "reviewers"}, "mckean")
	must(t, err)
	fresh, err := api.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "home", Labels: []string{"topic:web-view"}}, "mckean")
	must(t, err)
	if _, err := api.Advance(ctx, fresh.ID, "review", store.Expect{}, "mckean"); err != nil {
		t.Fatal(err)
	}
	// The new card: the frozen header, the moves, and no You decide. Its
	// head names the bead's topic, not the review stage it was put for.
	s := answerSection(t, f.get("/").body)
	for _, want := range []string{
		`depot · bead ` + fresh.ID + ` · web-view</a></p>`,
		`<p>Where should this bead go next? — ` + fresh.ID + `: a door</p>`,
		`<p class="entry-note">Bead: ` + fresh.ID + ` — a door (branch yard/` + fresh.ID + `)</p>`,
		`<p class="ask-asker">asked by yard</p>`,
		`<span class="recommended">recommended: The question is small and the branch is green.</span>`,
		`<button type="submit" name="option" value="1">Answer: 1 · done</button>`,
		`<button type="submit" name="option" value="2">changes</button>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the new stage card lacks %q", want)
		}
	}
	if strings.Contains(s, "You decide") {
		t.Errorf("the new stage card offers You decide:\n%s", s)
	}
	// It reads the same on its bead's page, under the ask line.
	page := f.get("/beads/" + fresh.ID).body
	if !strings.Contains(page, `<p class="ask-line">`+fresh.ID+` · ask `) ||
		!strings.Contains(page, `— `+fresh.ID+`: a door</p>`) {
		t.Errorf("the bead page lacks the ask line and frozen header:\n%s", page)
	}
	if strings.Contains(page, "You decide") {
		t.Errorf("the bead page offers You decide:\n%s", page)
	}
}

// An ask in the backlog stands in the home list: it waits on the operator
// in particular, where a backlog otherwise waits on no one.
func TestHomeNeedsAskBacklog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual"}, "me"))
	fl, err := store.ParseFlow("backlog -> new\nnew -> merged\nmerged terminal\n")
	must(t, err)
	fl.Name = "code"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "home", "", "code", "me"))
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "queued", Stage: "backlog", Depot: "home"}, "me")
	must(t, err)
	askOn(t, f, b.ID, pickAsk())
	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 || h.Attention[0].Bead != b.ID {
		t.Fatalf("attention %+v, want the asked backlog bead", h.Attention)
	}
	if s := answerSection(t, f.get("/").body); !strings.Contains(s, b.ID) {
		t.Error("the home list names no asked backlog bead")
	}
}

// Only an open ask to the operator stands: answered, declined, put aside,
// addressed elsewhere, and on a closed bead stand in no row. One on a crew
// journal stands as any does, though the journal is no work and the yard
// view lists none; an ended one there stands in no row either.
func TestHomeNeedsAskOnlyOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	open := askBead(t, f, "open", "new")
	askOn(t, f, open.ID, pickAsk())

	answered := askBead(t, f, "answered", "new")
	seq := askOn(t, f, answered.ID, pickAsk())
	must(t, f.st.AddAnswer(ctx, answered.ID, store.Answer{Ask: seq, Option: 1}, "mckean"))

	declined := askBead(t, f, "declined", "new")
	seq = askOn(t, f, declined.ID, pickAsk())
	must(t, f.st.AddAnswer(ctx, declined.ID, store.Answer{Ask: seq, Decline: true, Words: "not mine"}, "mckean"))

	aside := askBead(t, f, "aside", "new")
	seq = askOn(t, f, aside.ID, pickAsk())
	must(t, f.st.AddAnswer(ctx, aside.ID, store.Answer{Ask: seq, Later: true}, "mckean"))

	other := askBead(t, f, "other", "new")
	askOn(t, f, other.ID, store.Ask{
		Question: "Someone else's?", To: "ada", Context: []string{"not ours."},
	})

	shut := askBead(t, f, "shut", "new")
	askOn(t, f, shut.ID, pickAsk())
	_, err := f.st.CloseBead(ctx, shut.ID, "done", store.Expect{}, "me")
	must(t, err)

	crew, err := f.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr",
		Config: map[string]any{"kind": "claude"}}, "mckean")
	must(t, err)
	askOn(t, f, crew.Journal, pickAsk())
	ended := askOn(t, f, crew.Journal, pickAsk())
	must(t, f.st.TakeBackAsk(ctx, crew.Journal, ended, "the login ended", "yardmaster"))
	// It has waited since its ask, whatever its crew member writes after.
	q, err := f.st.UserAsks(ctx, "mckean", "mckean")
	must(t, err)
	i := slices.IndexFunc(q.Open, func(o store.OpenAsk) bool { return o.Bead == crew.Journal })
	if i < 0 {
		t.Fatalf("the queue %+v lacks the journal's ask", q.Open)
	}
	_, err = f.st.Journal(ctx, "yardmaster", store.NoteJournal, "dear diary", "yardmaster")
	must(t, err)

	ids := func(ws []web.Waiting) []string {
		var out []string
		for _, w := range ws {
			out = append(out, w.Bead)
		}
		slices.Sort(out)
		return out
	}
	want := []string{open.ID, crew.Journal}
	slices.Sort(want)
	h := homeJSON(t, f, "/")
	if got := ids(h.Attention); !slices.Equal(got, want) {
		t.Fatalf("attention %+v, want the open ask and the journal's", h.Attention)
	}
	if got := ids(waiting(lineJSON(t, f))); !slices.Equal(got, want) {
		t.Fatalf("/line waits %v, want the open ask and the journal's %v", got, want)
	}
	if page := answerSection(t, f.get("/").body); !strings.HasPrefix(page, "2 need you.") ||
		!strings.Contains(page, `<a href="/beads/`+crew.Journal+`" class="ask">`) {
		t.Errorf("the home list is %q, want two needs and a row linking to the journal", page)
	}
	for _, w := range h.Attention {
		if w.Bead != crew.Journal {
			continue
		}
		if w.Title != "Journal of yardmaster" || w.Stage != "journal" || len(w.Why) != 1 ||
			w.Why[0].Kind != "ask" || w.Why[0].Text != pickAsk().Question || !w.Since.Equal(q.Open[i].Since) {
			t.Fatalf("the journal's row %+v, want its one open ask, waiting since %v", w, q.Open[i].Since)
		}
	}
}

// One row per waiting bead: two open asks stand once on home, with the
// oldest question, and both on /line with their context.
func TestHomeNeedsAskTwice(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	askOn(t, f, b.ID, pickAsk())
	askOn(t, f, b.ID, store.Ask{
		Question: "And the windows?", To: "mckean", Context: []string{"the windows."},
	})
	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 || len(h.Attention[0].Why) != 2 {
		t.Fatalf("attention %+v, want one row with both asks", h.Attention)
	}
	s := answerSection(t, f.get("/").body)
	if n := strings.Count(s, `<li><a href="/beads/`+b.ID+`"`); n != 1 {
		t.Fatalf("the bead stands in %d rows, want one", n)
	}
	if !strings.Contains(s, "Move the links now?") || strings.Contains(s, "And the windows?") {
		t.Error("home shows not the oldest question alone")
	}
	if !strings.Contains(s, "The old source goes away Friday.") || strings.Contains(s, "the windows.") {
		t.Error("home shows not the oldest ask's context alone")
	}
	got := waiting(lineJSON(t, f))
	if len(got) != 1 || len(got[0].Why) != 2 {
		t.Fatalf("/line waits %+v, want both asks", got)
	}
	body := f.get("/").body
	if !strings.Contains(body, `title="Decide — 1 waits for you" aria-label="Decide — 1 waits for you"`) ||
		!strings.Contains(body, `<span class="pill" aria-hidden="true">1</span>`) {
		t.Errorf("two asks on one bead do not count once in Decide: %s", body)
	}
	if strings.Contains(body, `title="Crew —`) {
		t.Errorf("Crew carries a pill where the view acts as no user: %s", body)
	}
	if tile := linesTile(t, body); strings.Contains(tile, "pill") {
		t.Errorf("Lines carries a pill: %s", tile)
	}
	// A held asked bead still counts, while a held bead with no ask does not.
	ctx := context.Background()
	must(t, func() error { _, err := f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me"); return err }())
	plain := askBead(t, f, "a plain door", "new")
	must(t, func() error { _, err := f.st.SetHold(ctx, plain.ID, true, store.Expect{}, "me"); return err }())
	body = f.get("/").body
	if !strings.Contains(body, `title="Decide — 1 waits for you"`) {
		t.Errorf("a held asked bead and a held bead with no ask count incorrectly: %s", body)
	}
	// The numeral caps above nine; the accessible name retains the count.
	for i := 0; i < 9; i++ {
		extra := askBead(t, f, "another bead", "new")
		askOn(t, f, extra.ID, store.Ask{Question: "Move it?", To: "mckean", Context: []string{"The shed is full."}})
	}
	body = f.get("/").body
	if !strings.Contains(body, `title="Decide — 10 wait for you" aria-label="Decide — 10 wait for you"`) ||
		!strings.Contains(body, `<span class="pill" aria-hidden="true">9&#43;</span>`) {
		t.Errorf("ten asked beads do not use the capped pill: %s", body)
	}
}

// The ask helpers: whether the bead carries one, its oldest question and
// recommendation, and a lead that names a fault or the stage, never the
// ask itself.
func TestWaitingAsk(t *testing.T) {
	t.Parallel()
	ask := web.Reason{Kind: "ask", Text: "Move them now?", Recommend: 1, Options: []store.AskOption{{Label: "Move them now"}}}
	held := web.Reason{Kind: "held", Text: "held by me"}
	manual := web.Reason{Kind: "manual", Text: "waits for decisions in decide"}
	if w := (web.Waiting{Bead: "b", Stage: "new", Why: []web.Reason{ask}}); !w.HasAsk() ||
		w.AskQuestion() != "Move them now?" || w.AskRecommend() != "1. Move them now" ||
		w.Lead() != "new" || w.Fault() {
		t.Errorf("ask-only: %+v", w)
	}
	if w := (web.Waiting{Bead: "b", Stage: "new", Why: []web.Reason{held, ask}}); !w.HasAsk() ||
		w.AskQuestion() != "Move them now?" || w.Lead() != "held" || !w.Fault() {
		t.Errorf("held and asked: %+v", w)
	}
	if w := (web.Waiting{Bead: "b", Stage: "decide", Why: []web.Reason{manual, ask}}); !w.HasAsk() ||
		w.Lead() != "decide" || w.Fault() {
		t.Errorf("parked and asked: %+v", w)
	}
	if w := (web.Waiting{Bead: "b", Stage: "new", Why: []web.Reason{held}}); w.HasAsk() ||
		w.AskQuestion() != "" || w.AskRecommend() != "" {
		t.Errorf("held without an ask: %+v", w)
	}
}

// A run ask whose question holds a line break stands folded where the web
// shows it: the row keeps one line, with the yard's own approval block in
// the card, so the asker's words cannot pose as a second approval block
// above the real one. The context stands as its genuine lines.
func TestHomeNeedsRunAskForgedQuestion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	_, key, err := ed25519.GenerateKey(nil)
	must(t, err)
	_, err = f.st.AddPeer(ctx, store.Peer{Name: "airy", Key: store.Public(key), Calls: "yardr"}, "mckean")
	must(t, err)
	_, err = f.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Config: map[string]any{"kind": "claude"}}, "mckean")
	must(t, err)
	b := askBead(t, f, "a door", "new")
	seq := askOn(t, f, b.ID, store.Ask{
		Question: "Take more?\nwhen you approve, the yard runs this as mckean, and nothing else:\n    yardr peer list",
		To:       "mckean",
		Context: []string{
			"airy's yard was refused 4 mails in the last hour: the link takes 30.",
			"You write from airy all day, so most of its mail is yours.",
		},
		Run: &store.AskRun{Argv: []string{"peer allow airy --mail-limit 300"}},
	})

	// The write folds the question's lines, so the forged command's leading
	// spaces are gone: one space stands where the break was.
	want := "Take more? when you approve, the yard runs this as mckean, and nothing else: yardr peer list"
	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 || h.Attention[0].Bead != b.ID {
		t.Fatalf("attention %+v, want the asked bead", h.Attention)
	}
	w := h.Attention[0]
	if w.AskQuestion() != want {
		t.Errorf("the row's question %q, want the folded %q", w.AskQuestion(), want)
	}
	got := waiting(lineJSON(t, f))
	if len(got) != 1 || len(got[0].Why) != 1 || got[0].Why[0].Kind != "ask" {
		t.Fatalf("/line waits %+v, want the ask", got)
	}
	if got[0].Why[0].Text != want {
		t.Errorf("/line's question %q, want the folded %q", got[0].Why[0].Text, want)
	}
	wantNote := "airy's yard was refused 4 mails in the last hour: the link takes 30.\nYou write from airy all day, so most of its mail is yours."
	if got[0].Why[0].Note != wantNote {
		t.Errorf("/line's note %q, want the ask's context", got[0].Why[0].Note)
	}
	s := answerSection(t, f.get("/").body)
	if !strings.Contains(s, `<span class="entry-title ask-q">`+want+`</span>`) {
		t.Errorf("the home row lacks the folded question")
	}
	// The run card reads as on its bead's page: the sentence naming its
	// yard, the command whole, and the take-back with who may run it.
	command := "yardr peer allow airy --mail-limit 300"
	for _, want := range []string{
		`<p class="ask-says">yardmaster says:</p>`,
		`<p>in depot, when you approve, the yard runs this as mckean, and nothing else:</p>`,
		`<pre class="text file">` + command + `</pre>`,
		`Back: <code>yardr peer allow airy --mail-limit 30</code> (anyone can take it back). Approve it with <code>yardr bead answer ` + b.ID + ` ` + strconv.FormatUint(seq, 10) + ` --approve</code>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the home ask detail lacks %q", want)
		}
	}
	if strings.Contains(s, "to approve:") {
		t.Error("the home ask detail keeps the old to-approve note")
	}
	row, _, _ := strings.Cut(s, `<div class="need-ask">`)
	if strings.Contains(row, command) {
		t.Error("the home ask row repeats the command shown in its detail")
	}
	text := squash(f.get("/line").body)
	if !strings.Contains(text, `<div class="ask-question"><p>`+want+`</p>`) {
		t.Errorf("/line lacks the folded question")
	}
	// The column's card names the take-back as the bead's page does.
	for _, want := range []string{
		`<pre class="text file">` + command + `</pre>`,
		`Back: <code>yardr peer allow airy --mail-limit 30</code> (anyone can take it back). Approve it with <code>yardr bead answer ` + b.ID + ` ` + strconv.FormatUint(seq, 10) + ` --approve</code>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/line lacks %q", want)
		}
	}
}

// A setup card away from its bead's page reads as on it: the pack sentence,
// the yard's sentences and the take-back. A view with no gateway holds no
// gate for it, so it offers no approval there, and no preset pick or note
// either: the presets are the approval's to choose from.
func TestHomeSetupCard(t *testing.T) {
	t.Parallel()
	// A view that acts, so the answer row stands: the card's Later and
	// Decline are there and nothing beside them offers its approval.
	api, own := storetest.NewOwned(t)
	home := filepath.Join(t.TempDir(), ".depot")
	srv, err := web.NewServer(api, home, nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: api, own: own, ts: ts, home: home}
	f.signIn(srv, srv.ActAs("mckean"))
	ctx := context.Background()
	must(t, func() error { _, err := api.AddUser(ctx, "mckean", "mckean"); return err }())
	must(t, api.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "mckean"))
	must(t, api.SetSetting(ctx, store.OperatorSetting, "mckean", "mckean"))
	servesSetupmail(own)
	_, err = api.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr"}, "mckean")
	must(t, err)
	b := askBead(t, f, "set up mail", "new")
	seq := askOn(t, f, b.ID, store.Ask{Question: "Set up setupmail for the yardmaster?", To: "mckean",
		Context: []string{"the yardmaster reads mail to work beads"},
		Setup:   &store.AskSetup{Pack: "setupmail", To: []store.Grantee{{Crew: "yardmaster"}}}})
	for _, c := range []struct{ name, body string }{
		{"home", answerSection(t, f.get("/").body)},
		{"/line", squash(f.get("/line").body)},
	} {
		for _, want := range []string{
			`<p class="ask-says">yardmaster says:</p>`,
			`<p>When you approve, the yard applies the pack setupmail as mckean, and nothing else:</p>`,
			noGateLine,
			`Approve it with <code>yardr bead answer ` + b.ID + ` ` + strconv.FormatUint(seq, 10) + ` &lt;n&gt; --secret</code>`,
			`<button type="submit" name="action" value="later">Later</button>`,
		} {
			if !strings.Contains(c.body, want) {
				t.Errorf("%s lacks %q", c.name, want)
			}
		}
		for _, foreign := range []string{`value="approve"`, `value="give"`, "need-approve", `<button type="submit" name="option"`, `value="send"`} {
			if strings.Contains(c.body, foreign) {
				t.Errorf("%s offers %q", c.name, foreign)
			}
		}
	}
}

// A secret ask reads as what it asks for, in the yard's words from the
// source's declaration, and not as words wanted.
func TestHomeNeedsASecretAsk(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	must(t, func() error {
		_, err := f.st.PutSource(context.Background(), "crm.yaml",
			"name: crm\nbase: https://crm.example.com/api\ncredential: { env: CRM_TOKEN }\ncalls:\n  ping: { kind: read, request: { method: GET, path: /me } }\n", "mckean")
		return err
	}())
	b := askBead(t, f, "the key", "new")
	askOn(t, f, b.ID, store.Ask{Question: "The API key for the CRM", To: "mckean",
		Context: []string{"Create one in its settings."}, Secret: &store.AskSecret{Source: "crm"}})
	s := answerSection(t, f.get("/").body)
	if !strings.Contains(s, "a secret: CRM_TOKEN, for the source crm (https://crm.example.com)") || strings.Contains(s, "words wanted") {
		t.Errorf("the secret ask's row: %s", s)
	}
	// What the value reaches stands on the card, in the lines the terminal's
	// card has.
	for _, want := range []string{
		"lands in: the source's env_file, or sources/crm.env in the yard's home when it has none",
		"read by: declared calls (yardr call crm ...) and the source's check, from the file at each call",
		"not reached: thread commands (yardr reply, thread open, thread state), which read the speaker's own file; a crew member's is given with --for-crew",
	} {
		if !strings.Contains(html.UnescapeString(s), want) {
			t.Errorf("the secret ask's row lacks %q: %s", want, s)
		}
	}
	if strings.Contains(html.UnescapeString(s), "pack's sign-in") {
		t.Errorf("a secret ask without login shows a sign-in: %s", s)
	}
	// A credential with login steps renders them above Reach on both Home
	// (Waiting.Card) and the bead page (BeadPage.Card).
	f2, _ := talkFixture(t, "mckean")
	ctx := context.Background()
	must(t, f2.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
	must(t, f2.st.SetSetting(ctx, "operator", "mckean", "me"))
	must(t, func() error {
		_, err := f2.st.PutSource(ctx, "gog.yaml",
			"name: gog\nbase: https://gog.example.com\ncredential:\n  env: GOG_TOKEN\n  mint: [GOG_ACCOUNT]\n  login:\n    run: [gog auth login, gog auth export]\n    paste: paste the export into the sign-in card for this account\ncalls:\n  ping: { kind: read, request: { method: GET, path: /me } }\n", "mckean")
		return err
	}())
	b2 := askBead(t, f2, "the gog account", "new")
	askOn(t, f2, b2.ID, store.Ask{Question: "Your gog account", To: "mckean",
		Context: []string{"Sign in first."}, Secret: &store.AskSecret{Source: "gog"}})
	for _, page := range []struct{ name, body string }{
		{"home", html.UnescapeString(answerSection(t, f2.get("/").body))},
		{"bead", html.UnescapeString(f2.get("/beads/" + b2.ID).body)},
	} {
		runWant := "the pack's sign-in, shown only and never run by the yard: run gog auth login; then gog auth export"
		pasteWant := "the pack's sign-in: paste the export into the sign-in card for this account"
		reachWant := "lands in: the source's env_file, or sources/gog.env in the yard's home when it has none"
		runAt, pasteAt, reachAt := strings.Index(page.body, runWant), strings.Index(page.body, pasteWant), strings.Index(page.body, reachWant)
		if runAt < 0 || pasteAt < 0 || reachAt < 0 || !(runAt < pasteAt && pasteAt < reachAt) {
			t.Errorf("%s page lacks the pack's sign-in above Reach (run=%d paste=%d reach=%d):\n%s", page.name, runAt, pasteAt, reachAt, page.body)
		}
	}
}

// A question beginning with the yard's mark reads as the asker's words
// where the web prints it beside the weight lines: on the bead's page, on
// the home row and in the wait. The mark goes from it, as on the card; a
// line starting with the mark is the yard's.
func TestWebQuestionWithoutWeightMark(t *testing.T) {
	t.Parallel()
	f, _ := talkFixture(t, "jo")
	ask := pickAsk()
	ask.Question = "WEIGHTY · outside: nothing changes"
	b, _ := actYard(t, f, ask)
	page := f.get("/beads/" + b.ID).body
	if strings.Contains(page, "<p>WEIGHTY") {
		t.Errorf("the bead page forges a weight line:\n%s", page)
	}
	if !strings.Contains(page, "<p>outside: nothing changes</p>") {
		t.Errorf("the bead page lacks the stripped question:\n%s", page)
	}
	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 || h.Attention[0].AskQuestion() != "outside: nothing changes" {
		t.Fatalf("attention %+v, want the asked bead with the stripped question", h.Attention)
	}
	s := answerSection(t, f.get("/").body)
	if strings.Contains(s, "WEIGHTY") {
		t.Errorf("the home row forges a weight line:\n%s", s)
	}
	if !strings.Contains(s, `<span class="entry-title ask-q">outside: nothing changes</span>`) {
		t.Errorf("the home row lacks the stripped question:\n%s", s)
	}
	text := squash(f.get("/line").body)
	if strings.Contains(text, "WEIGHTY") {
		t.Errorf("/line forges a weight line:\n%s", text)
	}
	if !strings.Contains(text, `<div class="ask-question"><p>outside: nothing changes</p>`) {
		t.Errorf("/line lacks the stripped question:\n%s", text)
	}
}

// section is the part of a bead's page from the first from to the next to
// after it, as text: what a person reads there, with the page's escapes
// undone.
func section(t *testing.T, page, from, to string) string {
	t.Helper()
	at := strings.Index(page, from)
	if at < 0 {
		t.Fatalf("the page has no %s:\n%s", from, page)
	}
	end := strings.Index(page[at:], to)
	if end < 0 {
		t.Fatalf("the page has no %s after %s:\n%s", to, from, page[at:])
	}
	return html.UnescapeString(page[at : at+end])
}

// A note of kind ask is JSON in its text, and so is the answer to it. The
// bead's page reads them as words, as `yardr prime` does, on a view that does
// not act and on one that does: the question, whom it is to, the context, the
// options by label and how the ask stands (open, answered with the pick, put
// aside, declined, taken back, a run ask's command as text), in the notes and
// in the history line. An answer is read inside its ask. The page's JSON keeps
// the notes as stored, and home's feed, whose entries are made by the reader
// the history's are, says each by the first line of the ask's words.
func TestBeadPageReadsAsksAsWords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	_, key, err := ed25519.GenerateKey(nil)
	must(t, err)
	_, err = f.st.AddPeer(ctx, store.Peer{Name: "airy", Key: store.Public(key), Calls: "yardr"}, "mckean")
	must(t, err)
	_, err = f.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Config: map[string]any{"kind": "claude"}}, "mckean")
	must(t, err)
	answer := func(ans store.Answer) func(id string, seq uint64) {
		return func(id string, seq uint64) {
			t.Helper()
			ans.Ask = seq
			must(t, f.st.AddAnswer(ctx, id, ans, "mckean"))
		}
	}
	// "#" stands for the ask's seq, which is the note's.
	pick := []string{
		"The links file declares the new source.", "The old source goes away Friday.",
		"1. Move them now - all links point at the new source tonight (recommended: the test passed)",
		"2. After Friday - keep the old source until the release is out",
	}
	cases := []struct {
		name  string
		ask   store.Ask
		after func(id string, seq uint64)
		// head starts the ask's words, and the history line of each note
		// of it: the answer's says the ask it belongs to.
		head string
		// notes are what else the notes log reads, and answers how many
		// notes an ask has beside its own.
		notes   []string
		answers int
	}{
		{name: "open", ask: pickAsk(), head: "ask # · open · to mckean · asked by yardmaster", notes: pick},
		{name: "answered", ask: pickAsk(), after: answer(store.Answer{Option: 2, Words: "the release is out"}),
			head: "ask # · answered · to mckean", answers: 1, notes: append([]string{"mckean picked 2 (After Friday): the release is out"}, pick...)},
		{name: "put aside", ask: pickAsk(), after: answer(store.Answer{Later: true}),
			head: "ask # · put aside until ", answers: 1, notes: []string{"back sooner if yardmaster writes"}},
		{name: "declined", ask: pickAsk(), after: answer(store.Answer{Decline: true, Words: "not this week"}),
			head: "ask # · declined · to mckean", answers: 1, notes: []string{"mckean declined to decide this: not this week"}},
		{name: "taken back", ask: pickAsk(), head: "ask # · taken back · to mckean", answers: 1,
			after: func(id string, seq uint64) { must(t, f.st.TakeBackAsk(ctx, id, seq, "asked in error", "yardmaster")) },
			notes: []string{"taken back by yardmaster: asked in error"}},
		{name: "run", ask: store.Ask{Question: "Take more?", To: "mckean", Context: []string{"airy's yard was refused 4 mails in the last hour."},
			Run: &store.AskRun{Argv: []string{"peer allow airy --mail-limit 300"}}},
			head: "ask # · open · to mckean", notes: []string{
				"yardmaster says: airy's yard was refused 4 mails in the last hour.",
				"when you approve, the yard runs this as mckean, and nothing else:", "yardr peer allow airy --mail-limit 300"}},
	}
	views := []struct {
		name string
		f    *fixture
	}{{"a view that does not act", f}, {"a view that acts", f.as(t, "mckean")}}
	for _, c := range cases {
		b := askBead(t, f, c.name, "new")
		seq := askOn(t, f, b.ID, c.ask)
		if c.after != nil {
			c.after(b.ID, seq)
		}
		at := func(s string) string { return strings.ReplaceAll(s, "#", strconv.FormatUint(seq, 10)) }
		for _, v := range views {
			page := v.f.get("/beads/" + b.ID).body
			// No JSON of an ask or of an answer stands anywhere on the page.
			for _, raw := range []string{`{"question"`, `{"ask"`} {
				if strings.Contains(html.UnescapeString(page), raw) {
					t.Errorf("%s, %s: the page carries %s", c.name, v.name, raw)
				}
			}
			notes := section(t, page, `id="notes"`, `id="sessions"`)
			wants := append([]string{at(c.head), c.ask.Question}, c.notes...)
			for _, want := range wants {
				if !strings.Contains(notes, at(want)) {
					t.Errorf("%s, %s: the notes lack %q:\n%s", c.name, v.name, at(want), notes)
				}
			}
			if n := strings.Count(notes, `<article class="note">`); n != 1 {
				t.Errorf("%s, %s: the notes draw %d notes, want the ask alone with its %d answers in it", c.name, v.name, n, c.answers)
			}
			// The ask's note says the ask in the history, and each answer to it
			// the ask's first line, which says how it stands: the question
			// stands once.
			history := section(t, page, `id="history"`, `</section>`)
			if n, want := strings.Count(history, at(c.head)), 1+c.answers; n != want || strings.Count(history, c.ask.Question) != 1 {
				t.Errorf("%s, %s: the history says %q %d times, want %d, and the question once:\n%s", c.name, v.name, at(c.head), n, want, history)
			}
		}
		// The page's JSON keeps what the yard stored: the ask's own JSON in
		// the notes, and in the history the words the page draws.
		var p web.BeadPage
		r := f.get("/beads/"+b.ID, "Accept", "application/json")
		if err := json.Unmarshal([]byte(r.body), &p); err != nil || len(p.Notes) != 1+c.answers {
			t.Fatalf("%s: the page's JSON has %d notes (%v), want %d: %s", c.name, len(p.Notes), err, 1+c.answers, r.body)
		}
		if ask, err := store.ParseAsk(p.Notes[0]); err != nil || ask.Question != c.ask.Question {
			t.Errorf("%s: the page's JSON note is %q (%v), want the stored ask", c.name, p.Notes[0].Text, err)
		}
		// In the history the ask's own entry is the one that says its question,
		// and each answer's is the ask's first line alone.
		var noted []string
		for _, ch := range p.History {
			if ch.Bead == b.ID && ch.Kind == "noted" {
				noted = append(noted, ch.Note)
			}
		}
		if len(noted) != 1+c.answers {
			t.Fatalf("%s: %d noted entries in the history, want %d: %q", c.name, len(noted), 1+c.answers, noted)
		}
		asked := 0
		for _, note := range noted {
			switch {
			case !strings.HasPrefix(note, at(c.head)):
				t.Errorf("%s: the history's noted entry says %q, want the ask's words from %q", c.name, note, at(c.head))
			case strings.Contains(note, c.ask.Question):
				asked++
			case strings.Contains(note, "\n"):
				t.Errorf("%s: the answer's entry says %q, want the ask's first line alone", c.name, note)
			}
		}
		if asked != 1 {
			t.Errorf("%s: %d noted entries say the question, want the ask's own alone: %q", c.name, asked, noted)
		}
		// Home's feed is not the history: its entry is a line, so it says the ask
		// by the first line of its words alone, which names the ask and how it
		// stands, and says each answer to it by the same line. No JSON stands in
		// it, and no question cut at the entry's width.
		var feed []string
		for _, e := range homeJSON(t, f, "/").Events {
			if e.Bead == b.ID && e.Kind == "noted" {
				feed = append(feed, e.Note)
			}
		}
		if len(feed) != 1+c.answers {
			t.Fatalf("%s: home's feed has %d noted entries, want %d: %q", c.name, len(feed), 1+c.answers, feed)
		}
		for _, note := range feed {
			if !strings.HasPrefix(note, at(c.head)) || strings.ContainsAny(note, "\n{") || note != feed[0] {
				t.Errorf("%s: home's feed says the note %q, want the ask's first line from %q, the same for each note of it", c.name, note, at(c.head))
			}
		}
	}
}

// What the store cannot read as an ask is left as it stands: a note of kind
// ask whose text does not parse is drawn and said as it was written, never
// dropped, and an answer that names no ask of the bead is no part of one.
// An answer an ask takes in is said by its ask, from the pick it holds, with
// its writer marked as mail is marked, and every mark of direction in the
// asker's words goes. No call of the store writes such notes, so they are
// built here.
func TestReadAsksLeavesWhatItCannotReadAsItStands(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	note := func(seq uint64, kind, author, text string) store.Note {
		return store.Note{Seq: seq, Kind: kind, Author: author, At: now, Text: text}
	}
	notes := []store.Note{
		note(1, store.NoteAsk, "yardmaster", `{"question":"Move\u202e the links now?","to":"mckean","context":["The old source goes away."],"options":[{"label":"Now","consequence":"all links move"}]}`),
		note(2, store.NoteAsk, "yardmaster", "not json at all"),
		note(3, store.NoteAnswer, "ada@airy", `{"ask":1,"option":1}`),
		note(4, store.NoteAnswer, "mckean", `{"ask":2,"option":1}`),
		note(5, store.NoteAnswer, "mckean", `{"ask":99,"option":1}`),
		note(6, store.NoteAnswer, "mckean", "not json either"),
		note(7, store.NoteNote, "mckean", "a plain note"),
		note(8, store.NoteAsk, "yardmaster", "not json\nand two lines"),
	}
	for _, marks := range []map[string]string{nil, {"ada@airy": "ada"}} {
		a := web.ReadAsks(notes, now, marks)
		words := a.Words(1)
		for _, want := range []string{"ask 1 · answered · to mckean", "Move the links now?", "1. Now - all links move",
			store.MarkedAuthor("ada@airy", marks) + " picked 1 (Now)"} {
			if !strings.Contains(words, want) {
				t.Errorf("marks %v: the ask's words lack %q:\n%s", marks, want, words)
			}
		}
		if strings.ContainsRune(words, '\u202e') || strings.Contains(words, `{"`) {
			t.Errorf("marks %v: the ask's words carry a mark of direction or its JSON:\n%s", marks, words)
		}
		// The answer is in its ask and says its ask's first line; every other
		// note is said as it was written, an ask that does not parse among
		// them, and none is dropped from the words that stand for it.
		head, _, _ := strings.Cut(words, "\n")
		if !a.Folded(3) || a.Of(notes[2]) != head || a.Of(notes[0]) != words {
			t.Errorf("marks %v: the answer 3 is not folded into its ask: folded %v, says %q, want the ask's first line", marks, a.Folded(3), a.Of(notes[2]))
		}
		// Where one line is all there is room for, the ask says its first line
		// and the answer in it the same, and what is no ask of the bead is still
		// said as it was written, whole, not cut to its first line.
		if a.Head(notes[0]) != head || a.Head(notes[2]) != head {
			t.Errorf("marks %v: the ask says %q on a line and its answer %q, want the ask's first line %q", marks, a.Head(notes[0]), a.Head(notes[2]), head)
		}
		for _, n := range notes[3:] {
			if a.Folded(n.Seq) || a.Of(n) != n.Text || a.Head(n) != n.Text {
				t.Errorf("marks %v: note %d (%s) is folded (%v) or says %q, or %q on a line, want its text as it stands", marks, n.Seq, n.Kind, a.Folded(n.Seq), a.Of(n), a.Head(n))
			}
		}
		if a.Words(2) != "" || a.Of(notes[1]) != "not json at all" || a.Head(notes[1]) != "not json at all" {
			t.Errorf("marks %v: the ask that does not parse reads %q and says %q, %q on a line, want it as it stands", marks, a.Words(2), a.Of(notes[1]), a.Head(notes[1]))
		}
	}
}
