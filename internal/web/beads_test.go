package web_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/web"
)

// getJSON reads target as JSON into v and returns the answer.
func (f *fixture) getJSON(target string, v any) resp {
	f.t.Helper()
	r := f.get(target, "Accept", "application/json")
	if r.status != 200 {
		f.t.Fatalf("%s: status %d: %s", target, r.status, r.body)
	}
	if err := json.Unmarshal([]byte(r.body), v); err != nil {
		f.t.Fatalf("%s: %v: %s", target, err, r.body)
	}
	return r
}

// byTitle maps the title of each open bead to the bead.
func (f *fixture) byTitle() map[string]store.Bead {
	f.t.Helper()
	bs, err := f.st.ListBeads(context.Background(), store.BeadFilter{})
	must(f.t, err)
	out := map[string]store.Bead{}
	for _, b := range bs {
		out[b.Title] = b
	}
	return out
}

// titles are the titles of cards, in their order.
func titles(cs []web.Card) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Title)
	}
	return out
}

// srowOf is the row of the stage that draws the bead id, from its link to the
// end of it, "" when the page has none.
func srowOf(body, id string) string {
	at := strings.Index(body, `<a class="wv-srow" href="/beads/`+id+`">`)
	if at < 0 {
		return ""
	}
	return body[at : at+strings.Index(body[at:], "</a>")]
}

// cards are the beads of /beads as the page shows them.
func cards(rs []web.BeadRow) []web.Card {
	out := []web.Card{}
	for _, r := range rs {
		out = append(out, r.Card())
	}
	return out
}

// /flows as JSON: per depot its flows, each stage with its group, its stage
// file and the open beads in it, the newest change first. A bead of a depot
// with no flow is in the depot's elsewhere; a closed one is nowhere.
func TestFlowsJSON(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 1}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "people", Runner: "manual", Members: []string{"ada"}}, "me"))
	line := "line"
	_, err := f.st.AddRoute(ctx, store.Route{Stage: "new", Depot: &line, Group: "builders"}, "me")
	must(t, err)
	_, err = f.st.AddRoute(ctx, store.Route{Stage: "review", Group: "people"}, "me")
	must(t, err)
	// A store flow's stage file is in the yard's home.
	file := filepath.Join(store.HomeStageDir(f.home, "code"), "new.md")
	must(t, os.MkdirAll(filepath.Dir(file), 0o755))
	must(t, os.WriteFile(file, []byte("Build it.\n"), 0o644))
	head, err := f.st.Head(ctx)
	must(t, err)

	var got web.Flows
	f.getJSON("/flows", &got)
	if got.Head != head || len(got.Depots) != 2 {
		t.Fatalf("head %d, %d depots, want %d and 2: %+v", got.Head, len(got.Depots), head, got)
	}
	depot := got.Depots[0]
	if depot.Name != "line" || len(depot.Flows) != 2 || len(depot.Elsewhere) != 0 || depot.Error != "" {
		t.Fatalf("depot line: %+v", depot)
	}
	code := depot.Flows[0]
	if code.Name != "code" || code.Type != "" || code.Source != "store" || len(code.Stages) != 4 {
		t.Fatalf("flow code: %+v", code)
	}
	type stage struct {
		name, group, file string
		human, terminal   bool
		members, next     []string
		beads             []string
	}
	var have []stage
	for _, s := range code.Stages {
		have = append(have, stage{s.Stage, s.Group, s.StageFile, s.Human, s.Terminal, s.Members, s.Next, titles(s.Beads)})
	}
	want := []stage{
		// "two" was held after "one" was made: the newest change is first.
		{name: "backlog", next: []string{"new"}, beads: []string{"two", "one"}},
		{name: "new", group: "builders", file: file, next: []string{"review", "backlog"}, beads: []string{"three"}},
		{name: "review", group: "people", human: true, members: []string{"ada"}, next: []string{"merged", "new"}, beads: []string{}},
		{name: "merged", terminal: true, next: []string{}, beads: []string{}},
	}
	if !reflect.DeepEqual(have, want) {
		t.Errorf("flow code's stages:\n got %+v\nwant %+v", have, want)
	}
	if two := code.Stages[0].Beads[0]; !two.Held || two.Working || two.Status != "open" || two.Depot != "line" || two.Stage != "backlog" {
		t.Errorf("the held bead's card: %+v", two)
	}
	bugs := depot.Flows[1]
	if bugs.Name != "bugs" || bugs.Type != "bug" || len(bugs.Stages) != 2 || !reflect.DeepEqual(titles(bugs.Stages[0].Beads), []string{"five"}) {
		t.Errorf("flow bugs: %+v", bugs)
	}
	loose := got.Depots[1]
	if loose.Name != "loose" || len(loose.Flows) != 0 || !reflect.DeepEqual(titles(loose.Elsewhere), []string{"six"}) {
		t.Errorf("depot loose: %+v", loose)
	}
}

// A bead that pins a flow of its own follows that one, so it stands off
// the depot's flow even in a stage the depot's flow has (as `yardr flow show`
// counts it).
func TestFlowsPinnedBead(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	fl, err := store.ParseFlow("new -> gone\ngone terminal\n")
	must(t, err)
	fl.Name = "short"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	_, err = f.st.CreateBead(ctx, store.NewBead{Title: "pinned", Stage: "new", Depot: "line",
		Metadata: map[string]any{"flow": "short"}}, "me")
	must(t, err)
	var got web.Flows
	f.getJSON("/flows", &got)
	if depot := got.Depots[0]; !reflect.DeepEqual(titles(depot.Elsewhere), []string{"pinned"}) ||
		!reflect.DeepEqual(titles(depot.Flows[0].Stages[1].Beads), []string{"three"}) {
		t.Errorf("elsewhere %v, new %v", titles(depot.Elsewhere), titles(depot.Flows[0].Stages[1].Beads))
	}
}

// A depot whose flows cannot be read still shows its beads, and the other
// depots their flows.
func TestFlowsReadError(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	srv, err := web.NewServer(failingDepotFlows{API: f.st, depot: "line"}, f.home, nil)
	must(t, err)
	f.ts.Config.Handler = srv
	var got web.Flows
	f.getJSON("/flows", &got)
	if depot := got.Depots[0]; depot.Error == "" || len(depot.Flows) != 0 || len(depot.Elsewhere) != 4 {
		t.Errorf("depot line: %+v", depot)
	}
	// The depot's error stays on the page beside its beads, folded stages or none.
	if r := f.get("/flows"); r.status != 200 || !strings.Contains(r.body, `id="off/loose"`) ||
		!strings.Contains(r.body, `<p class="held">repository is unavailable</p>`) {
		t.Errorf("the page: status %d", r.status)
	}
}

// section is the id of the element that holds a card on /flows.
var (
	sectionRE = regexp.MustCompile(`<div class="cards" id="([^"]+)" sse-swap="[^"]+" hx-target="this" hx-swap="afterbegin settle:0ms">(.*?)</div>`)
	cardRE    = regexp.MustCompile(`<article class="card( arrived)?" id="([^"]+)"`)
)

// sections reads /flows as a browser shows it: the id of each card's
// element, by the id of the section it is in.
func (f *fixture) sections() map[string]string {
	f.t.Helper()
	r := f.get("/flows")
	if r.status != 200 {
		f.t.Fatalf("/flows: status %d: %s", r.status, r.body)
	}
	out := map[string]string{}
	for _, sec := range sectionRE.FindAllStringSubmatch(squashLines(r.body), -1) {
		for _, c := range cardRE.FindAllStringSubmatch(sec[2], -1) {
			out[c[2]] = sec[1]
		}
	}
	return out
}

func squashLines(s string) string { return strings.ReplaceAll(s, "\n", " ") }

// foldedRE finds the stages /flows folds to their head line.
var foldedRE = regexp.MustCompile(`<li class="stage quiet[^"]*"><details><summary><h4>([^<]*)</h4>`)

// folded are the stages /flows stands folded, in the page's order: a stage
// folds while it holds no bead.
func (f *fixture) folded() []string {
	f.t.Helper()
	var out []string
	for _, m := range foldedRE.FindAllStringSubmatch(squash(f.get("/flows").body), -1) {
		out = append(out, m[1])
	}
	return out
}

// The page: one section per stage, named so the stream can put a card in
// it, each bead a card that links to its page; and the stream it listens
// to, after the page's head.
func TestFlowsHTML(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	head, err := f.st.Head(ctx)
	must(t, err)
	r := f.get("/flows")
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q", r.status, r.header.Get("Content-Type"))
	}
	bs := f.byTitle()
	for _, want := range []string{
		shellOpen(head, "card"),
		// settle:0ms: a card the stream brought listens at once, so the
		// bead's next event, however soon, takes it out again.
		`<div class="cards" id="at/line//backlog" sse-swap="at/line//backlog" hx-target="this" hx-swap="afterbegin settle:0ms">`,
		`<div class="cards" id="at/line//merged" sse-swap="at/line//merged" hx-target="this" hx-swap="afterbegin settle:0ms">`,
		`<div class="cards" id="at/line/bug/triage" sse-swap="at/line/bug/triage" hx-target="this" hx-swap="afterbegin settle:0ms">`,
		`<div class="cards" id="off/loose" sse-swap="off/loose" hx-target="this" hx-swap="afterbegin settle:0ms">`,
		`<article class="card" id="bead/` + bs["three"].ID + `" sse-swap="bead/` + bs["three"].ID + `" hx-target="this" hx-swap="delete">`,
		`<a class="bead" href="/beads/` + bs["three"].ID + `">` + bs["three"].ID + `</a>`,
		`<p class="card-title">three</p>`,
		`<a class="wv-row" href="/flows" aria-current="page">`,
	} {
		if !strings.Contains(r.body, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	// A stage with no bead is folded to its head line: a closed details whose
	// body is the section the stream fills, the count after it. A stage with
	// a bead has the head and cards it always had, in no details.
	page := squash(r.body)
	for _, want := range []string{
		`<li class="stage quiet terminal"><details><summary><h4>merged</h4><span class="label stage-who">closes the bead</span></summary>` +
			`<div class="cards" id="at/line//merged" sse-swap="at/line//merged" hx-target="this" hx-swap="afterbegin settle:0ms"></div></details>` +
			`<span class="count num" aria-hidden="true"></span></li>`,
		`<li class="stage"><h4>new</h4><p class="label stage-who">no route: waits for a person</p>` +
			`<div class="cards" id="at/line//new" sse-swap="at/line//new" hx-target="this" hx-swap="afterbegin settle:0ms"><article class="card"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if got, want := f.folded(), []string{"review", "merged", "fixed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("folded stages %v, want %v", got, want)
	}
	// The stylesheet shows a folded stage while a card is in it: a closed
	// details leaves its body, and the count, out. What the pixels do is the
	// bead's screenshots.
	css, err := os.ReadFile("assets/yard.css")
	must(t, err)
	if !strings.Contains(string(css), ".stage.quiet:has(.card) details::details-content { content-visibility: visible; }") {
		t.Error("the stylesheet does not show a folded stage that holds a card")
	}
	got := f.sections()
	want := map[string]string{
		"bead/" + bs["one"].ID: "at/line//backlog", "bead/" + bs["two"].ID: "at/line//backlog",
		"bead/" + bs["three"].ID: "at/line//new", "bead/" + bs["five"].ID: "at/line/bug/triage",
		"bead/" + bs["six"].ID: "off/loose",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cards by section:\n got %v\nwant %v", got, want)
	}
}

// apply does to cards (a card's element id to its section's) what the page
// does with one frame of the card stream: the card that hears its name
// takes itself out, the section that hears its name puts the card on top.
// The frame ends with the unnamed message the right column hears, the
// event's seq, whether a card moved or not.
func apply(t *testing.T, cards map[string]string, fr frame) {
	t.Helper()
	left := ""
	if n := len(fr.msgs); n == 0 || fr.msgs[n-1] != (msg{"", fmt.Sprint(fr.id)}) {
		t.Fatalf("frame %d does not end with its seq as an unnamed message: %+v", fr.id, fr.msgs)
	}
	for i, m := range fr.msgs[:len(fr.msgs)-1] {
		switch {
		case i == 0 && strings.HasPrefix(m.event, "bead/"):
			left = m.event
			delete(cards, left)
		case i == 1 && (strings.HasPrefix(m.event, "at/") || strings.HasPrefix(m.event, "off/")):
			c := cardRE.FindStringSubmatch(m.data)
			if c == nil || c[2] != left || !strings.Contains(m.data, ` sse-swap="`+left+`" hx-target="this" hx-swap="delete"`) {
				t.Fatalf("frame %d puts in another card than it took out, or one that cannot leave: %s", fr.id, m.data)
			}
			cards[c[2]] = m.event
		default:
			t.Fatalf("frame %d: message %d is %q: %s", fr.id, i, m.event, m.data)
		}
	}
}

// put is the card a frame of the card stream puts into a section, "" when
// it puts none.
func put(fr frame) string {
	for _, m := range fr.msgs {
		if strings.HasPrefix(m.event, "at/") || strings.HasPrefix(m.event, "off/") {
			return m.data
		}
	}
	return ""
}

// The card stream moves a bead's card from one section of /flows to the
// next: taken through new, review and merged, the card leaves each section
// and arrives in the other, and leaves the page when the bead lands. What
// an open page then shows is what a reload shows.
func TestFlowsStream(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	bs := f.byTitle()
	three, six := bs["three"], bs["six"]
	page := f.sections()
	var flows web.Flows
	f.getJSON("/flows", &flows)
	// The stage the first move fills is folded on the page the stream is
	// open on, and its section is inside the fold: the card lands there.
	if !strings.Contains(squash(f.get("/flows").body), `<li class="stage quiet"><details><summary><h4>review</h4>`) ||
		!reflect.DeepEqual(f.folded(), []string{"review", "merged", "fixed"}) {
		t.Fatalf("review is not folded: %v", f.folded())
	}
	s := f.stream(t, fmt.Sprintf("/events?after=%d&format=card", flows.Head))
	// step runs one change and applies the frames it sends to the page.
	step := func(change func(), frames int) frame {
		t.Helper()
		change()
		var fr frame
		for range frames {
			fr = s.next(t)
			apply(t, page, fr)
		}
		if got := f.sections(); !reflect.DeepEqual(page, got) {
			t.Fatalf("after frame %d the open page has\n%v\na reload shows\n%v", fr.id, page, got)
		}
		return fr
	}
	card := "bead/" + three.ID

	// new to review: out of new, into review, seen arriving.
	fr := step(func() {
		_, err := f.st.Advance(ctx, three.ID, "review", store.Expect{}, "me")
		must(t, err)
	}, 1)
	if page[card] != "at/line//review" || !strings.Contains(put(fr), `<article class="card arrived" id="`+card+`"`) {
		t.Errorf("the move to review: section %q, frame %s", page[card], put(fr))
	}
	// A reload folds the stages as the open page's stylesheet does: review
	// holds the card and stands open, new has given it up and folds.
	if got, want := f.folded(), []string{"new", "merged", "fixed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after the move to review, folded stages %v, want %v", got, want)
	}
	// A hold changes the card where it is: nothing arrives.
	fr = step(func() {
		_, err := f.st.SetHold(ctx, three.ID, true, store.Expect{}, "me")
		must(t, err)
	}, 1)
	if page[card] != "at/line//review" || strings.Contains(put(fr), "arrived") || !strings.Contains(put(fr), `<span class="held">held</span>`) {
		t.Errorf("the hold: section %q, frame %s", page[card], put(fr))
	}
	// A note changes no card: its seq alone, for the right column.
	if fr = step(func() { must(t, f.st.AddNote(ctx, three.ID, "looks right", "me")) }, 1); len(fr.msgs) != 1 {
		t.Errorf("a note came with a card: %+v", fr.msgs)
	}
	step(func() {
		_, err := f.st.SetHold(ctx, three.ID, false, store.Expect{}, "me")
		must(t, err)
	}, 1)
	// A new bead arrives in the first stage of its flow.
	var made *store.Bead
	fr = step(func() {
		var err error
		made, err = f.st.CreateBead(ctx, store.NewBead{Title: "seven", Type: "bug", Depot: "line"}, "me")
		must(t, err)
	}, 1)
	if page["bead/"+made.ID] != "at/line/bug/triage" || !strings.Contains(put(fr), "card arrived") {
		t.Errorf("the new bead: section %q, frame %s", page["bead/"+made.ID], put(fr))
	}
	// A bead of a depot with no flow stays off the flow.
	step(func() {
		_, err := f.st.Advance(ctx, six.ID, "anywhere", store.Expect{}, "me")
		must(t, err)
	}, 1)
	if page["bead/"+six.ID] != "off/loose" {
		t.Errorf("the loose bead is in %q", page["bead/"+six.ID])
	}
	// review to merged lands the bead: an `advanced` and a `closed`, and
	// the card is gone from the page.
	fr = step(func() {
		_, err := f.st.Advance(ctx, three.ID, "merged", store.Expect{}, "me")
		must(t, err)
	}, 2)
	if _, there := page[card]; there || len(fr.msgs) != 2 || fr.msgs[0] != (msg{card, "left"}) {
		t.Errorf("the landed bead is still on the page: %+v", fr.msgs)
	}
	// Reopened, it stands in the stage it landed in.
	step(func() {
		_, err := f.st.Reopen(ctx, three.ID, store.Expect{}, "me")
		must(t, err)
	}, 1)
	if page[card] != "at/line//merged" {
		t.Errorf("the reopened bead is in %q", page[card])
	}
	// merged holds it again, so it stands open; the stages left empty fold.
	if got, want := f.folded(), []string{"new", "review", "fixed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after the reopening, folded stages %v, want %v", got, want)
	}
	s.quiet(t)
}

// A move is three messages: the card leaves, it enters its section, and
// the event's seq for whatever reads itself again. Only the last carries
// the seq as its id, so a browser cut off between them reconnects with the
// seq before and is sent the whole move again; with the id on the first,
// the card would be gone from the page for good.
func TestCardStreamCursor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	three := f.byTitle()["three"]
	head, err := f.st.Head(ctx)
	must(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/events?after=%d&format=card", f.ts.URL, head), nil)
	must(t, err)
	res, err := http.DefaultClient.Do(req)
	must(t, err)
	defer res.Body.Close()
	_, err = f.st.Advance(ctx, three.ID, "review", store.Expect{}, "me")
	must(t, err)
	var lines []string
	for sc := bufio.NewScanner(res.Body); sc.Scan(); {
		if line := sc.Text(); !strings.HasPrefix(line, "retry:") && !strings.HasPrefix(line, "data: ") && line != "" {
			lines = append(lines, line)
		}
		if len(lines) == 3 {
			break
		}
	}
	want := []string{"event: bead/" + three.ID, "event: at/line//review", fmt.Sprintf("id: %d", head+1)}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("the move came as %q, want %q", lines, want)
	}
}

// /beads: the open beads, the newest change first, with the query string's
// filters for depot and stage.
func TestBeadsJSON(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	head, err := f.st.Head(ctx)
	must(t, err)
	var got web.Beads
	f.getJSON("/beads", &got)
	// "two" changed last (its hold); the rest in the order they were made.
	if want := []string{"two", "six", "five", "three", "one"}; !reflect.DeepEqual(titles(cards(got.Beads)), want) {
		t.Errorf("beads %v, want %v", titles(cards(got.Beads)), want)
	}
	if got.Head != head || !reflect.DeepEqual(got.Depots, []string{"line", "loose"}) ||
		!reflect.DeepEqual(got.Stages, []string{"backlog", "inbox", "new", "triage"}) {
		t.Errorf("head %d (want %d), depots %v, stages %v", got.Head, head, got.Depots, got.Stages)
	}
	// What each choice lists, beside the names: every depot has its count,
	// a depot with no bead its zero.
	if want := (map[string]int{"line": 4, "loose": 1}); !reflect.DeepEqual(got.DepotCounts, want) || got.AnyDepot != 5 {
		t.Errorf("depot counts %v, any %d, want %v and 5", got.DepotCounts, got.AnyDepot, want)
	}
	if want := (map[string]int{"backlog": 2, "inbox": 1, "new": 1, "triage": 1}); !reflect.DeepEqual(got.StageCounts, want) || got.AnyStage != 5 {
		t.Errorf("stage counts %v, any %d, want %v and 5", got.StageCounts, got.AnyStage, want)
	}
	for target, want := range map[string][]string{
		"/beads?depot=line":               {"two", "five", "three", "one"},
		"/beads?stage=backlog":            {"two", "one"},
		"/beads?depot=line&stage=new":     {"three"},
		"/beads?depot=loose&stage=new":    {},
		"/beads?depot=nowhere":            {},
		"/beads?depot=loose&stage=inbox&": {"six"},
	} {
		var got web.Beads
		f.getJSON(target, &got)
		if !reflect.DeepEqual(titles(cards(got.Beads)), want) {
			t.Errorf("%s: %v, want %v", target, titles(cards(got.Beads)), want)
		}
	}
	// The stages a depot's filter offers are the depot's. (Read afresh:
	// decoding into the page above would keep its counts' keys.)
	got = web.Beads{}
	f.getJSON("/beads?depot=loose&stage=inbox", &got)
	if got.Depot != "loose" || got.Stage != "inbox" || !reflect.DeepEqual(got.Stages, []string{"inbox"}) {
		t.Errorf("filtered: %+v", got)
	}
	// A depot's count is under the stage kept, a stage's under the depot kept.
	if want := (map[string]int{"line": 0, "loose": 1}); !reflect.DeepEqual(got.DepotCounts, want) || got.AnyDepot != 1 ||
		!reflect.DeepEqual(got.StageCounts, map[string]int{"inbox": 1}) || got.AnyStage != 1 {
		t.Errorf("filtered counts: depots %v any %d, stages %v any %d", got.DepotCounts, got.AnyDepot, got.StageCounts, got.AnyStage)
	}
}

// chip is one choice of the filters of /beads as the page shows it: the link
// it follows, the number beside its name, and whether it is the current one.
type chip struct {
	label, href string
	count       int
	current     bool
}

var (
	filterRowRE = regexp.MustCompile(`<p><span class="wv-chips__label">(depot|stage)</span>(.*?)</p>`)
	chipRE      = regexp.MustCompile(`<a class="wv-pill" href="([^"]+)"( aria-current="true")?>([^<]*?) <span class="wv-num">(\d+)</span></a>`)
)

// filterChips reads the filters of a /beads page as a browser lays them out:
// the chips of each row, "depot" and "stage", in the order they stand. A
// link of the row that is no chip with its count is lost to it, so the
// rows a test compares fail when a count is dropped. The links carry the
// ampersand of a query as the markup does, which a browser reads back.
func filterChips(t *testing.T, body string) map[string][]chip {
	t.Helper()
	rows := map[string][]chip{}
	for _, row := range filterRowRE.FindAllStringSubmatch(squash(body), -1) {
		for _, c := range chipRE.FindAllStringSubmatch(row[2], -1) {
			n, err := strconv.Atoi(c[4])
			must(t, err)
			rows[row[1]] = append(rows[row[1]], chip{label: c[3], href: strings.ReplaceAll(c[1], "&amp;", "&"), count: n, current: c[2] != ""})
		}
	}
	return rows
}

// srowNameRE and srowEndRE are the title that opens a row of the stage and the
// age that closes it.
var (
	srowNameRE = regexp.MustCompile(`<span class="wv-srow__name">([^<]*)</span>`)
	srowEndRE  = regexp.MustCompile(`<span class="wv-srow__end"><span>([^<]*)</span></span></a>`)
)

// The page: a stage of the list kind with the beads as rows, the newest change
// first, each ending in the age of its last change; the filters as chips that
// keep each other, each with the number of beads its link lists; and the
// request it repeats when the stream says something happened.
func TestBeadsHTML(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	// A bead of no depot is in the list and in no depot's chip.
	_, err := f.st.CreateBead(context.Background(), store.NewBead{Title: "seven", Stage: "inbox"}, "me")
	must(t, err)
	head, err := f.st.Head(context.Background())
	must(t, err)
	r := f.get("/beads?depot=line&stage=new")
	if r.status != 200 {
		t.Fatalf("status %d", r.status)
	}
	three := f.byTitle()["three"]
	page := squash(r.body)
	for _, want := range []string{
		shellOpen(head, "seq"),
		leafOf("/beads?depot=line&stage=new", "innerHTML ignoreTitle:true"),
		// The stage draws its own head, in place of the shell's heading.
		`<article class="wv-page" data-kind="list"><header class="wv-head"><h1 class="wv-title">Beads</h1><p class="wv-meta">1 open, the newest change first</p></header>`,
		`<nav class="wv-chips" aria-label="Filters">`,
		// The row: the bead's ids and places on its line, and Decide's page
		// is where it leads, so the block that holds the rows swaps the whole list.
		`<ol hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list:outerHTML"><li><a class="wv-srow" href="/beads/` + three.ID + `">`,
		`<span class="wv-id">` + three.ID + `</span><span class="wv-sep">·</span><span class="wv-nw">line</span><span class="wv-sep">·</span>new<span class="wv-sep">·</span>task`,
		`<a class="wv-row" href="/beads" aria-current="page">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if strings.Contains(r.body, `class="page-head"`) {
		t.Error("the shell's heading stands over the list page's own head")
	}
	// A filter keeps the beads of its own: "two" is held, in backlog.
	if strings.Contains(stageOf(t, r.body), ">two<") {
		t.Error("the filtered page shows a bead of another stage")
	}
	// The chips are links outside the block of rows, which would otherwise
	// swap the whole list for a click that stays in the mode.
	if rows := strings.Index(page, `<ol hx-select-oob`); rows < 0 || strings.LastIndex(page, `class="wv-pill"`) > rows {
		t.Error("a chip stands in the block of rows that swaps the whole list")
	}
	// Only /flows moves cards one by one: here they listen to nothing.
	if strings.Contains(r.body, `sse-swap="bead/`) {
		t.Error("a row of the list listens to the stream")
	}
	// A filter that keeps nothing says so where the rows would stand, and a yard
	// with no depot and no bead has no chip to choose from, or a sentence for
	// the closed beads it would list.
	for target, want := range map[string]string{
		"/beads?depot=nowhere":         `<p class="wv-none">No open bead matches.</p>`,
		"/beads?all=1&depot=nowhere":   `<p class="wv-none">No bead matches.</p>`,
		"/beads?depot=loose&stage=new": `<p class="wv-none">No open bead matches.</p>`,
	} {
		body := squash(f.get(target).body)
		if !strings.Contains(body, want) || strings.Contains(body, "<ol") {
			t.Errorf("%s lacks %s, or draws rows", target, want)
		}
	}
	bare := squash(newFixture(t).get("/beads").body)
	if !strings.Contains(bare, `<p class="wv-meta">0 open, the newest change first</p></header><p class="wv-none">No open bead.</p>`) || strings.Contains(bare, "wv-chips") {
		t.Errorf("an empty yard's page: %s", stageOf(t, bare))
	}

	// The rows are the newest change first: by updated_at, as the JSON says it,
	// and the page draws them in that order, each ending in how long ago.
	var all web.Beads
	f.getJSON("/beads", &all)
	for i := 1; i < len(all.Beads); i++ {
		if all.Beads[i].UpdatedAt.After(all.Beads[i-1].UpdatedAt) {
			t.Errorf("%s changed after %s, which stands before it", all.Beads[i].Title, all.Beads[i-1].Title)
		}
	}
	whole := squash(stageOf(t, f.get("/beads").body))
	var shown []string
	for _, m := range srowNameRE.FindAllStringSubmatch(whole, -1) {
		shown = append(shown, m[1])
	}
	if want := []string{"seven", "two", "six", "five", "three", "one"}; !reflect.DeepEqual(shown, want) || !reflect.DeepEqual(titles(cards(all.Beads)), want) {
		t.Errorf("the page draws %v and its JSON %v, want the newest change first: %v", shown, titles(cards(all.Beads)), want)
	}
	if ages := srowEndRE.FindAllStringSubmatch(whole, -1); len(ages) != len(shown) {
		t.Errorf("%d of %d rows end in an age", len(ages), len(shown))
	} else {
		for _, m := range ages {
			if !regexp.MustCompile(`^\d+[smhd]( \d+[mh])?$`).MatchString(m[1]) {
				t.Errorf("a row ends in %q, want the age of its last change", m[1])
			}
		}
	}

	// The chips of the known list, by the filters of the page: the links are
	// the ones Link builds (the closed beads' flag kept), a depot counts under
	// the stage kept, a stage under the depot kept, and each row's all under
	// the other filter alone, so it counts the bead of no depot that no depot
	// does. Where the stage kept lists nothing, no stage is the current one.
	type rows struct{ depot, stage []chip }
	targets := []struct {
		target string
		want   rows
	}{
		{"/beads", rows{
			depot: []chip{{"all", "/beads", 6, true}, {"line", "/beads?depot=line", 4, false}, {"loose", "/beads?depot=loose", 1, false}},
			stage: []chip{{"all", "/beads", 6, true}, {"backlog", "/beads?stage=backlog", 2, false}, {"inbox", "/beads?stage=inbox", 2, false},
				{"new", "/beads?stage=new", 1, false}, {"triage", "/beads?stage=triage", 1, false}},
		}},
		{"/beads?depot=line&stage=new", rows{
			depot: []chip{{"all", "/beads?stage=new", 1, false}, {"line", "/beads?depot=line&stage=new", 1, true}, {"loose", "/beads?depot=loose&stage=new", 0, false}},
			stage: []chip{{"all", "/beads?depot=line", 4, false}, {"backlog", "/beads?depot=line&stage=backlog", 2, false},
				{"new", "/beads?depot=line&stage=new", 1, true}, {"triage", "/beads?depot=line&stage=triage", 1, false}},
		}},
		{"/beads?depot=loose&stage=new", rows{
			depot: []chip{{"all", "/beads?stage=new", 1, false}, {"line", "/beads?depot=line&stage=new", 1, false}, {"loose", "/beads?depot=loose&stage=new", 0, true}},
			stage: []chip{{"all", "/beads?depot=loose", 1, false}, {"inbox", "/beads?depot=loose&stage=inbox", 1, false}},
		}},
		{"/beads?all=1&depot=line", rows{
			depot: []chip{{"all", "/beads?all=1", 7, false}, {"line", "/beads?all=1&depot=line", 5, true}, {"loose", "/beads?all=1&depot=loose", 1, false}},
			stage: []chip{{"all", "/beads?all=1&depot=line", 5, true}, {"backlog", "/beads?all=1&depot=line&stage=backlog", 2, false},
				{"closed", "/beads?all=1&depot=line&stage=closed", 1, false},
				{"new", "/beads?all=1&depot=line&stage=new", 2, false}, {"triage", "/beads?all=1&depot=line&stage=triage", 1, false}},
		}},
	}
	opens := map[string]int{}
	for _, tc := range targets {
		got := filterChips(t, f.get(tc.target).body)
		if !reflect.DeepEqual(got["depot"], tc.want.depot) || !reflect.DeepEqual(got["stage"], tc.want.stage) {
			t.Errorf("%s: chips\n depot %+v\n stage %+v\nwant\n depot %+v\n stage %+v", tc.target, got["depot"], got["stage"], tc.want.depot, tc.want.stage)
		}
		for _, c := range append(slices.Clone(tc.want.depot), tc.want.stage...) {
			opens[c.href] = c.count
		}
	}
	// Whatever the chip says is what its link lists.
	for href, count := range opens {
		var listed web.Beads
		f.getJSON(href, &listed)
		if len(listed.Beads) != count {
			t.Errorf("%s lists %d beads, a chip counts %d", href, len(listed.Beads), count)
		}
	}
}

// whole gives the yard a train with two children, one blocking the other,
// in a flow with outcomes and a stage file, with notes and a session, and
// returns the blocked child.
func whole(t *testing.T, f *fixture) (train, blocker, b *store.Bead) {
	t.Helper()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	fl, err := store.ParseFlow("new -> review on done\nnew -> decide on question\nreview -> merged\nmerged terminal\n")
	must(t, err)
	fl.Name = "code"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "line", "", "code", "me"))
	file := filepath.Join(store.HomeStageDir(f.home, "code"), "new.md")
	must(t, os.MkdirAll(filepath.Dir(file), 0o755))
	// The summary line is for the map in yardr prime: the page leaves it out.
	must(t, os.WriteFile(file, []byte("summary: Built here.\n\nA builder implements the bead here.\n"), 0o644))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 2}, "me"))
	train, err = f.st.CreateBead(ctx, store.NewBead{Title: "the shed", Type: store.TypeTrain, Stage: "new", Depot: "line"}, "me")
	must(t, err)
	blocker, err = f.st.CreateBead(ctx, store.NewBead{Title: "the posts", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	b, err = f.st.CreateBead(ctx, store.NewBead{Title: "a door", Body: "Hang it.\n\n- on two hinges", Stage: "new", Depot: "line",
		Priority: 1, Labels: []string{"wood"}}, "me")
	must(t, err)
	// Claimed first: a blocked bead is not handed to anyone.
	_, err = f.st.Claim(ctx, b.ID, "builders", "new", "dispatcher")
	must(t, err)
	must(t, f.st.AddDep(ctx, train.ID, blocker.ID, store.DepParent, "", "", "me"))
	must(t, f.st.AddDep(ctx, train.ID, b.ID, store.DepParent, "", "", "me"))
	must(t, f.st.AddDep(ctx, blocker.ID, b.ID, store.DepBlocks, "", "", "me"))
	must(t, f.st.AddNote(ctx, b.ID, "the hinge is missing", "builder"))
	must(t, f.st.AddNote(ctx, b.ID, "found one", "passer"))
	b, err = f.st.GetBead(ctx, b.ID)
	must(t, err)
	return train, blocker, b
}

// A bead's page as JSON: what `yardr prime --bead` knows.
func TestBeadJSON(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	train, blocker, b := whole(t, f)
	head, err := f.st.Head(context.Background())
	must(t, err)
	var got web.BeadPage
	f.getJSON("/beads/"+b.ID, &got)
	if got.Bead == nil || got.Bead.ID != b.ID || got.Bead.Title != "a door" || got.Bead.Body != "Hang it.\n\n- on two hinges" ||
		got.Bead.Stage != "new" || got.Bead.Type != "task" || got.Head != head {
		t.Fatalf("the bead: %+v, head %d (want %d)", got.Bead, got.Head, head)
	}
	if got.Flow == nil || *got.Flow != (web.BeadFlow{Name: "code", Source: "store"}) {
		t.Errorf("flow %+v", got.Flow)
	}
	if got.Base != store.Branch(train.ID) || got.Train == nil || got.Train.ID != train.ID || got.Train.Title != "the shed" {
		t.Errorf("base %q, train %+v", got.Base, got.Train)
	}
	if len(got.Blockers) != 1 || got.Blockers[0].ID != blocker.ID || got.Blockers[0].Title != "the posts" {
		t.Errorf("blockers %+v", got.Blockers)
	}
	if got.StageFile == nil || got.StageFile.Body() != "A builder implements the bead here.\n" ||
		got.StageFile.Path != filepath.Join(store.HomeStageDir(f.home, "code"), "new.md") {
		t.Errorf("stage file %+v", got.StageFile)
	}
	if want := []web.BeadOutcome{
		{Name: "done", To: "review", Command: "yardr bead done " + b.ID},
		{Name: "question", To: "decide", Command: "yardr bead done " + b.ID + " --outcome question"},
	}; !reflect.DeepEqual(got.Outcomes, want) {
		t.Errorf("outcomes %+v, want %+v", got.Outcomes, want)
	}
	if !reflect.DeepEqual(got.Next, []string{"review", "decide"}) {
		t.Errorf("next %v", got.Next)
	}
	// Oldest first.
	if len(got.Notes) != 2 || got.Notes[0].Text != "the hinge is missing" || got.Notes[0].Author != "builder" || got.Notes[1].Text != "found one" {
		t.Errorf("notes %+v", got.Notes)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Group != "builders" || got.Sessions[0].Stage != "new" || got.Sessions[0].Bead != b.ID {
		t.Errorf("sessions %+v", got.Sessions)
	}
	if len(got.Deps) < 2 {
		t.Errorf("deps %+v", got.Deps)
	}
	// The train's page has its children; a bead no flow governs says so by
	// having none.
	var ep web.BeadPage
	f.getJSON("/beads/"+train.ID, &ep)
	if ids := []string{ep.Children[0].ID, ep.Children[1].ID}; len(ep.Children) != 2 || !reflect.DeepEqual(ids, []string{blocker.ID, b.ID}) || ep.Train != nil {
		t.Errorf("the train's children %+v", ep.Children)
	}
}

// seePolicy hides the beads it names from the viewers it names, each an
// (actor, bead) pair, and asks the core for the rest.
type seePolicy struct {
	hide map[[2]string]bool
}

func (p seePolicy) May(ctx context.Context, tx *bolt.Tx, actor string, act store.Action, obj store.Object) error {
	if act == store.ActSee && obj.Kind == store.KindBead && p.hide[[2]string{actor, obj.Name}] {
		return errors.New("hidden from " + actor)
	}
	return store.CorePolicy.May(ctx, tx, actor, act, obj)
}

// The page, for a person; an unknown bead is a 404 in both forms. A viewer
// the policy refuses reads a bead as missing, while one it allows reads it.
func TestBeadHTML(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	train, blocker, b := whole(t, f)
	head, err := f.st.Head(context.Background())
	must(t, err)
	r := f.get("/beads/" + b.ID)
	if r.status != 200 {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	for _, want := range []string{
		shellOpen(head, "seq"),
		leafOf("/beads/"+b.ID, "innerHTML ignoreTitle:true"),
		`<title>a door · depot</title>`,
		`<article class="wv-page" data-kind="bead">`,
		`<a class="wv-back" href="/">`,
		`<h1 class="wv-title">a door</h1>`,
		`<p class="wv-meta"><span class="wv-id">` + b.ID + `</span><span class="wv-sep">·</span>task<span class="wv-sep">·</span>P1<span class="wv-sep">·</span>line<span class="wv-sep">·</span>flow code<span class="wv-sep">·</span><span class="wv-run">working</span></p>`,
		`<dt>flow</dt><dd>code <span class="wv-dim">(store)</span></dd>`,
		`<dt>base</dt><dd>yard/` + train.ID + `</dd>`,
		`<dt>train</dt><dd><a href="/beads/` + train.ID + `">` + train.ID + `</a> the shed</dd>`,
		`<dt>labels</dt><dd>wood</dd>`,
		`<h2 class="wv-sh"><span>Blocked by</span><span class="wv-sh__count">1</span></h2>`,
		`<a class="wv-srow" href="/beads/` + blocker.ID + `">`,
		`<div class="wv-prose"><p>Hang it.</p></div>`,
		"<pre class=\"wv-text\">Hang it.\n\n- on two hinges</pre>",
		"<pre class=\"wv-text wv-file\">A builder implements the bead here.\n</pre>",
		`<code>yardr bead done ` + b.ID + ` --outcome question</code>`,
		`<p>the hinge is missing</p>`,
		`builders · stage new`,
	} {
		if !strings.Contains(r.body, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if first, second := strings.Index(r.body, "the hinge is missing"), strings.Index(r.body, "found one"); first < 0 || second < first {
		t.Error("the notes are not oldest first")
	}
	if r := f.get("/beads/line-none"); r.status != 404 || !strings.Contains(r.body, `class="item held"`) {
		t.Errorf("an unknown bead: status %d", r.status)
	}
	if r := f.get("/beads/line-none", "Accept", "application/json"); r.status != 404 || !strings.Contains(r.body, `"error"`) {
		t.Errorf("an unknown bead as JSON: status %d: %s", r.status, r.body)
	}
	ctx := context.Background()
	must(t, func() error { _, err := f.st.AddUser(ctx, "jo", "jo"); return err }())
	must(t, func() error { _, err := f.st.AddUser(ctx, "ada", "ada"); return err }())
	jo, ada := f.as(t, "jo"), f.as(t, "ada")
	f.own.SetPolicy(seePolicy{hide: map[[2]string]bool{{"jo", b.ID}: true, {"", b.ID}: true}})
	if r := jo.get("/beads/" + b.ID); r.status != http.StatusNotFound {
		t.Errorf("jo's page with the bead hidden: status %d, want 404", r.status)
	}
	if r := ada.get("/beads/" + b.ID); r.status != http.StatusOK {
		t.Errorf("ada's page with the bead hidden from jo: status %d, want 200", r.status)
	}
	if r := f.get("/beads/" + b.ID); r.status != http.StatusNotFound {
		t.Errorf("the bare view's page with the bead hidden: status %d, want 404", r.status)
	}
	if r := f.get("/beads/" + train.ID); r.status != http.StatusOK {
		t.Errorf("the bare view's page for a shown bead: status %d, want 200", r.status)
	}
}

// The bead's page says how far into a round limit it is, once it has gone
// round, and where each way of ending its stage takes it now: the stage the
// limit sends it to once the rounds are used, in JSON and on the page.
func TestBeadPageSaysWhereARoundGoes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b, report := roundLoop(t, f, 3, "anna")
	page := func() (web.BeadPage, string) {
		t.Helper()
		var got web.BeadPage
		f.getJSON("/beads/"+b.ID, &got)
		return got, f.get("/beads/" + b.ID).body
	}
	changes := func(got web.BeadPage) web.BeadOutcome {
		t.Helper()
		for _, o := range got.Outcomes {
			if o.Name == "changes" {
				return o
			}
		}
		t.Fatalf("no changes among the outcomes %+v", got.Outcomes)
		return web.BeadOutcome{}
	}

	// The first review has no rounds counted: no row, the first round named.
	report("done")
	got, html := page()
	if len(got.RoundsLines) != 0 || strings.Contains(html, `<p class="wv-eta">rounds: `) {
		t.Errorf("a bead on its first review shows rounds: %v", got.RoundsLines)
	}
	if o := changes(got); o.To != "new" || o.Note != "round 1 of 3" {
		t.Errorf("changes on the first review = %+v, want new, round 1 of 3", o)
	}

	// Two rounds used: the next changes is round 3 and goes back to new.
	for range 2 {
		report("changes")
		report("done")
	}
	got, html = page()
	if want := []string{"new 2 of 3 (the next changes from review goes to new)"}; !reflect.DeepEqual(got.RoundsLines, want) {
		t.Errorf("rounds lines = %v, want %v", got.RoundsLines, want)
	}
	if o := changes(got); o.To != "new" || o.Note != "round 3 of 3" {
		t.Errorf("changes after two rounds = %+v, want new, round 3 of 3", o)
	}
	for _, want := range []string{
		`<p class="wv-eta">rounds: new 2 of 3 (the next changes from review goes to new)</p>`,
		`<td class="wv-name">changes</td><td>new</td><td><code>yardr bead done ` + b.ID + ` --outcome changes</code> <span class="wv-dim">round 3 of 3</span></td>`,
	} {
		if !strings.Contains(squashLines(html), want) {
			t.Errorf("the page lacks %s", want)
		}
	}

	// All three used: the next changes goes to decide, and the page says so.
	report("changes")
	report("done")
	got, html = page()
	if want := []string{"new 3 of 3 (the next changes from review goes to decide)"}; !reflect.DeepEqual(got.RoundsLines, want) {
		t.Errorf("rounds lines = %v, want %v", got.RoundsLines, want)
	}
	if o := changes(got); o.To != "decide" || o.Note != "the 3 rounds back to new are used" {
		t.Errorf("changes at the limit = %+v, want decide, the rounds used", o)
	}
	for _, want := range []string{
		`<p class="wv-eta">rounds: new 3 of 3 (the next changes from review goes to decide)</p>`,
		`<td class="wv-name">changes</td><td>decide</td><td><code>yardr bead done ` + b.ID + ` --outcome changes</code> <span class="wv-dim">the 3 rounds back to new are used</span></td>`,
	} {
		if !strings.Contains(squashLines(html), want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	// The bead's own rounds are as the store has them.
	if want := map[string]int{"new": 3, "review": 3}; !reflect.DeepEqual(got.Bead.Rounds, want) {
		t.Errorf("bead rounds = %v, want %v", got.Bead.Rounds, want)
	}
}

// A bead found from another names it, and the origin's page lists what it
// led to, closed beads too.
func TestBeadDiscoveredFrom(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	origin, err := f.st.CreateBead(ctx, store.NewBead{Title: "the origin"}, "me")
	must(t, err)
	found, err := f.st.CreateBead(ctx, store.NewBead{Title: "a loose end", DiscoveredFrom: origin.ID}, "me")
	must(t, err)
	_, err = f.st.CloseBead(ctx, found.ID, "fixed", store.Expect{}, "me")
	must(t, err)

	var got web.BeadPage
	f.getJSON("/beads/"+found.ID, &got)
	if len(got.DiscoveredFrom) != 1 || got.DiscoveredFrom[0].ID != origin.ID || got.DiscoveredFrom[0].Title != "the origin" || len(got.LedTo) != 0 {
		t.Errorf("the found bead: discovered from %+v, led to %+v", got.DiscoveredFrom, got.LedTo)
	}
	got = web.BeadPage{}
	f.getJSON("/beads/"+origin.ID, &got)
	if len(got.LedTo) != 1 || got.LedTo[0].ID != found.ID || got.LedTo[0].Status != "closed" || len(got.DiscoveredFrom) != 0 {
		t.Errorf("the origin: led to %+v, discovered from %+v", got.LedTo, got.DiscoveredFrom)
	}
	if want, body := `<dt>discovered from</dt><dd><a href="/beads/`+origin.ID+`">`+origin.ID+`</a> the origin</dd>`, f.get("/beads/"+found.ID).body; !strings.Contains(body, want) {
		t.Errorf("the found bead's page lacks %s", want)
	}
	body := f.get("/beads/" + origin.ID).body
	// What a bead led to stands in its Linked beads fold, as a row of the stage.
	if at := strings.Index(body, `<h3>led to</h3>`); at < 0 || !strings.Contains(body[at:], `<a class="wv-srow" href="/beads/`+found.ID+`">`) ||
		!strings.Contains(body, `<span class="wv-fold__label">Linked beads</span><span class="wv-fold__value">led to 1 · 1 edge</span>`) {
		t.Errorf("the origin's page does not list %s under led to", found.ID)
	}
	if strings.Contains(f.get("/beads/"+found.ID).body, `<span class="wv-fold__label">Linked beads</span>`) && len(got.Deps) == 0 {
		t.Errorf("a bead with no link has a Linked beads fold")
	}
}

// A bead's title, body, notes and stage file are text someone wrote. Every
// page and the card stream write them as text, never as markup; JSON
// carries them as they are.
func TestBeadEscapes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, _, b := whole(t, f)
	ctx := context.Background()
	const evil = `<script>alert(1)</script><img src=x onerror=alert(2)>`
	file := filepath.Join(store.HomeStageDir(f.home, "code"), "new.md")
	must(t, os.WriteFile(file, []byte("stage "+evil), 0o644))
	head, err := f.st.Head(ctx)
	must(t, err)
	title, body := "title "+evil, "body "+evil
	_, err = f.st.UpdateBead(ctx, b.ID, store.BeadPatch{Title: &title, Body: &body}, store.Expect{}, "me")
	must(t, err)
	must(t, f.st.AddNote(ctx, b.ID, "note "+evil, "author "+evil))

	clean := func(what, html string) {
		t.Helper()
		if strings.Contains(html, "<script>alert") || strings.Contains(html, "<img") {
			t.Errorf("%s carries what someone wrote as markup", what)
		}
	}
	page := f.get("/beads/" + b.ID).body
	clean("the bead's page", page)
	const escaped = `&lt;script&gt;alert(1)&lt;/script&gt;&lt;img src=x onerror=alert(2)&gt;`
	for _, part := range []string{"title ", "body ", "note ", "stage ", "author "} {
		if !strings.Contains(page, part+escaped) {
			t.Errorf("the bead's page does not show the %sas text", part)
		}
	}
	for _, target := range []string{"/flows", "/beads", "/"} {
		p := f.get(target).body
		clean(target, p)
		if !strings.Contains(p, "title "+escaped) {
			t.Errorf("%s does not show the title as text", target)
		}
	}
	s := f.stream(t, fmt.Sprintf("/events?after=%d&format=card", head))
	if c := put(s.next(t)); !strings.Contains(c, "title "+escaped) {
		t.Errorf("the card stream does not show the title as text: %s", c)
	} else {
		clean("the card stream", c)
	}
	var got web.BeadPage
	f.getJSON("/beads/"+b.ID, &got)
	if got.Bead.Body != body || got.Notes[len(got.Notes)-1].Text != "note "+evil || got.StageFile.Text != "stage "+evil {
		t.Errorf("JSON changed what was written: %+v", got)
	}
}

// A note by a name of another yard says whose word it is on the bead's
// page, as mail does: (as <user>) where a link trusts the name, the
// foreign line otherwise. A note of this yard's own carries neither, and
// the page's JSON names the author as the note has it.
func TestBeadPageMarksTrustedNoteAuthor(t *testing.T) {
	t.Parallel()
	st, own := storetest.NewOwned(t)
	ctx := context.Background()
	if _, err := st.AddUser(ctx, "ada", "me"); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPeer(ctx, store.Peer{Name: "airy", Key: store.Public(key)}, "me"); err != nil {
		t.Fatal(err)
	}
	p, err := st.GetPeer(ctx, "airy")
	if err != nil {
		t.Fatal(err)
	}
	if err := own.PeerHeard(ctx, "airy", p.Key, &store.PeerSays{
		Understands: []string{"ping"},
		Roster:      []store.PeerRosterEntry{{Name: "ada", Kind: store.RosterUser}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AllowPeer(ctx, "airy", store.PeerAllow{Trust: map[string]string{"ada": "ada"}}, "me"); err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateBead(ctx, store.NewBead{Title: "a door"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	page, err := st.PutBlob(ctx, pageHTML)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range [][2]string{
		{"from a trusted name", "ada@airy"},
		{"from a stranger", "jo@airy"},
		{"this yard's own", "builder"},
	} {
		if err := st.AddNote(ctx, b.ID, n[0], n[1], store.BlobFile{Hash: page.Hash}); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(t.TempDir(), ".depot")
	srv, err := web.NewServer(st, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: st, ts: ts, home: home}

	body := f.get("/beads/" + b.ID).body
	for _, want := range []string{
		"ada@airy (as ada)",
		"jo@airy (" + store.OutsideMail + ")",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the bead's page lacks %q", want)
		}
	}
	if strings.Contains(body, "builder (") {
		t.Errorf("a note of this yard is marked on the page")
	}
	boardAt, notesAt := strings.Index(body, `id="pages"`), strings.Index(body, `id="notes"`)
	if boardAt < 0 || notesAt < boardAt {
		t.Fatalf("the page has no board above the notes:\n%s", body)
	}
	board := body[boardAt:notesAt]
	for _, want := range []string{
		"ada@airy (as ada)",
		"jo@airy (" + store.OutsideMail + ")",
	} {
		if !strings.Contains(board, want) {
			t.Errorf("the note board lacks %q", want)
		}
	}
	if strings.Contains(board, "builder (") {
		t.Errorf("a note of this yard is marked on the board")
	}

	var got web.BeadPage
	f.getJSON("/beads/"+b.ID, &got)
	for _, n := range got.Notes {
		if strings.Contains(n.Author, "(") {
			t.Errorf("the bead's JSON marks the author: %+v", n)
		}
	}
}

// The history marks its actor as the notes mark their author: (as
// <user>) where a link trusts the name, the foreign line otherwise,
// and neither for a name of this yard.
func TestBeadPageMarksHistoryActor(t *testing.T) {
	t.Parallel()
	st, own := storetest.NewOwned(t)
	ctx := context.Background()
	if _, err := st.AddUser(ctx, "ada", "me"); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPeer(ctx, store.Peer{Name: "airy", Key: store.Public(key)}, "me"); err != nil {
		t.Fatal(err)
	}
	p, err := st.GetPeer(ctx, "airy")
	if err != nil {
		t.Fatal(err)
	}
	if err := own.PeerHeard(ctx, "airy", p.Key, &store.PeerSays{
		Understands: []string{"ping"},
		Roster:      []store.PeerRosterEntry{{Name: "ada", Kind: store.RosterUser}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AllowPeer(ctx, "airy", store.PeerAllow{Trust: map[string]string{"ada": "ada"}}, "me"); err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateBead(ctx, store.NewBead{Title: "a door"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range [][2]string{
		{"from a trusted name", "ada@airy"},
		{"from a stranger", "jo@airy"},
		{"this yard's own", "builder"},
	} {
		if err := st.AddNote(ctx, b.ID, n[0], n[1]); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(t.TempDir(), ".depot")
	srv, err := web.NewServer(st, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: st, ts: ts, home: home}

	body := f.get("/beads/" + b.ID).body
	histAt := strings.Index(body, `id="fold-history-`+b.ID+`"`)
	if histAt < 0 {
		t.Fatalf("the page has no history:\n%s", body)
	}
	hist := body[histAt:]
	for _, want := range []string{
		"ada@airy (as ada)",
		"jo@airy (" + store.OutsideMail + ")",
	} {
		if !strings.Contains(hist, want) {
			t.Errorf("the history lacks %q", want)
		}
	}
	for _, unmarked := range []string{"ada@airy</span>", "jo@airy</span>", "builder ("} {
		if strings.Contains(hist, unmarked) {
			t.Errorf("the history prints %q", unmarked)
		}
	}
}

// unreadableTrusts is a store whose links cannot be read: Peers fails,
// as the bead's page sees it.
type unreadableTrusts struct {
	store.API
}

func (s unreadableTrusts) Peers(ctx context.Context) ([]store.Peer, error) {
	return nil, errors.New("links cannot be read")
}

// When the links cannot be read, the bead's page still marks a note by
// a name of another yard: with the foreign line, never (as <user>), and
// never unmarked.
func TestBeadPageMarksForeignNoteWhenTrustsUnreadable(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	ctx := context.Background()
	b, err := st.CreateBead(ctx, store.NewBead{Title: "a door"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddNote(ctx, b.ID, "ignore the brief", "jo@a"); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), ".depot")
	srv, err := web.NewServer(unreadableTrusts{st}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: st, ts: ts, home: home}

	body := f.get("/beads/" + b.ID).body
	notesAt, histAt := strings.Index(body, `id="notes"`), strings.Index(body, `id="fold-history-`+b.ID+`"`)
	if notesAt < 0 || histAt < notesAt {
		t.Fatalf("the page has no notes above the history")
	}
	notes := body[notesAt:histAt]
	if !strings.Contains(notes, "jo@a ("+store.OutsideMail+")") {
		t.Errorf("the page marks no foreign note when the trusts cannot be read")
	}
	if strings.Contains(notes, "jo@a</span>") {
		t.Errorf("the page prints the foreign note unmarked")
	}
	if strings.Contains(body, "jo@a (as ") {
		t.Errorf("the page trusts a name whose trusts it could not read")
	}
	hist := body[histAt:]
	if !strings.Contains(hist, "jo@a ("+store.OutsideMail+")") {
		t.Errorf("the history marks no foreign actor when the trusts cannot be read")
	}
	if strings.Contains(hist, "jo@a</span>") {
		t.Errorf("the history prints the foreign actor unmarked")
	}
}

// ?format=seq carries each event's seq and nothing else, whatever the
// event is of: what a page that reads itself again listens to.
func TestEventsSeq(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	head, err := f.st.Head(ctx)
	must(t, err)
	mine, other := bead(t, f.st, "mine"), bead(t, f.st, "other")
	must(t, f.st.AddNote(ctx, other.ID, "not for you", "me"))
	must(t, f.st.AddNote(ctx, mine.ID, "for you", "me"))

	s := f.stream(t, fmt.Sprintf("/events?after=%d&format=seq", head))
	for i := range 4 {
		if fr := s.next(t); fr.id != head+uint64(i)+1 || fr.data != fmt.Sprint(fr.id) {
			t.Errorf("frame %d: id %d, data %q", i, fr.id, fr.data)
		}
	}
	if r := f.get("/events?format=cards"); r.status != 400 {
		t.Errorf("format=cards: status %d, want 400", r.status)
	}
}

// A bead a manual group has claimed is a person's turn: its card has no
// working dot and says no "working" on /flows, on /beads, on its own page
// and as a child of its train, as JSON and as the page, and its session is
// listed as claimed, not running. A bead an agent's session has claimed is
// working everywhere, as before.
func TestManualClaimIsNotWorking(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	idea := backlogYard(t, f, 1)[0]
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake", Limit: 5}, "me"))
	door, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	sess, err := f.st.Claim(ctx, door.ID, "builders", "new", "yard")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "door-new"}))
	train, err := f.st.CreateBead(ctx, store.NewBead{Title: "the shed", Type: store.TypeTrain, Stage: "new", Depot: "line"}, "me")
	must(t, err)
	must(t, f.st.AddDep(ctx, train.ID, idea.ID, store.DepParent, "", "", "me"))
	must(t, f.st.AddDep(ctx, train.ID, door.ID, store.DepParent, "", "", "me"))

	// working is what cards say of the two beads, by title.
	working := func(cs []web.Card) map[string]bool {
		out := map[string]bool{}
		for _, c := range cs {
			if c.ID == idea.ID || c.ID == door.ID {
				out[c.Title] = c.Working
			}
		}
		return out
	}
	want := map[string]bool{"idea a": false, "a door": true}
	var flows web.Flows
	flowsJSON := f.getJSON("/flows", &flows)
	if got := working(flows.Depots[0].Elsewhere); !reflect.DeepEqual(got, want) {
		t.Errorf("/flows: working %v, want %v", got, want)
	}
	var beads web.Beads
	beadsJSON := f.getJSON("/beads", &beads)
	if got := working(cards(beads.Beads)); !reflect.DeepEqual(got, want) {
		t.Errorf("/beads: working %v, want %v", got, want)
	}
	var ep, claimed, built web.BeadPage
	trainJSON := f.getJSON("/beads/"+train.ID, &ep)
	if got := working(ep.Children); !reflect.DeepEqual(got, want) {
		t.Errorf("the train's children: working %v, want %v", got, want)
	}
	f.getJSON("/beads/"+idea.ID, &claimed)
	f.getJSON("/beads/"+door.ID, &built)
	if claimed.Working || !built.Working {
		t.Errorf("the beads' pages: working %v for the manual claim, %v for the agent's, want false and true", claimed.Working, built.Working)
	}
	if r := f.get("/beads/"+idea.ID, "Accept", "application/json"); !strings.Contains(r.body, `"working": false`) {
		t.Errorf("the manual claim's page as JSON lacks \"working\":false: %s", r.body)
	}
	assertCardWorking := func(label string, body, id string, want bool) {
		t.Helper()
		var root any
		if err := json.Unmarshal([]byte(body), &root); err != nil {
			t.Fatalf("%s: decode JSON: %v", label, err)
		}
		var find func(any) (bool, bool)
		find = func(v any) (bool, bool) {
			switch v := v.(type) {
			case map[string]any:
				if v["id"] == id {
					got, ok := v["working"].(bool)
					return got, ok
				}
				for _, child := range v {
					if got, ok := find(child); ok {
						return got, true
					}
				}
			case []any:
				for _, child := range v {
					if got, ok := find(child); ok {
						return got, true
					}
				}
			}
			return false, false
		}
		if got, ok := find(root); !ok || got != want {
			t.Errorf("%s: card %s working = %v (present %v), want %v", label, id, got, ok, want)
		}
	}
	for _, check := range []struct {
		label, body string
	}{
		{"/flows JSON", flowsJSON.body}, {"/beads JSON", beadsJSON.body}, {"train page JSON", trainJSON.body},
	} {
		assertCardWorking(check.label, check.body, idea.ID, false)
		assertCardWorking(check.label, check.body, door.ID, true)
	}

	// cardOf is the bead's card on a page, "" when the page has none.
	cardOf := func(body, id string) string {
		at := strings.Index(body, `<article class="card" id="bead/`+id+`"`)
		if at < 0 {
			return ""
		}
		return body[at : at+strings.Index(body[at:], "</article>")]
	}
	// /flows still draws its beads as cards.
	flowsPage := f.get("/flows").body
	if c := cardOf(flowsPage, idea.ID); c == "" || strings.Contains(c, "working") || !strings.Contains(c, `<span class="dot"></span>`) {
		t.Errorf("/flows: the manual claim's card: %q", c)
	}
	if c := cardOf(flowsPage, door.ID); !strings.Contains(c, `<span class="dot working"></span>`) || !strings.Contains(c, "· working") {
		t.Errorf("/flows: the agent's bead's card: %q", c)
	}
	// /beads has them as rows of the stage, and the train's page as the rows of
	// its Linked beads fold.
	rowOf := func(body, id string) string {
		at := strings.Index(body, `<a class="wv-srow" href="/beads/`+id+`">`)
		if at < 0 {
			return ""
		}
		return body[at : at+strings.Index(body[at:], "</a>")]
	}
	for page, body := range map[string]string{
		"/beads":           stageOf(t, f.get("/beads").body),
		"the train's page": foldOf(t, f.get("/beads/"+train.ID).body, train.ID, "links"),
	} {
		if r := rowOf(body, idea.ID); r == "" || strings.Contains(r, "working") || strings.Contains(r, "wv-dot--run") {
			t.Errorf("%s: the manual claim's row: %q", page, r)
		}
		if r := rowOf(body, door.ID); !strings.Contains(r, `<span class="wv-dot wv-dot--run"></span>`) || !strings.Contains(r, "working") {
			t.Errorf("%s: the agent's bead's row: %q", page, r)
		}
	}
	// A manual group's claim is a person's turn: the meta line says no
	// working, and nothing on the stage is green but what the claim is not.
	body := squash(f.get("/beads/" + idea.ID).body)
	for _, want := range []string{
		`<span class="wv-sep">·</span>task<span class="wv-sep">·</span>P0<span class="wv-sep">·</span>line<span class="wv-sep">·</span>no flow</p>`,
		`<span>backlog · stage backlog · manual</span>`,
		`<p><span class="wv-dot"></span>claimed <span class="wv-faint wv-mono">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the manual claim's page lacks %s", want)
		}
	}
	if stage := stageOf(t, body); strings.Contains(stage, "wv-run") || strings.Contains(stage, "wv-dot--run") || strings.Contains(stage, ">working<") {
		t.Errorf("the manual claim's page says it is worked: %s", stage)
	}
	// An agent's session works the bead: the meta line says so in green, and
	// the session stands running, with the dot that says it.
	body = squash(f.get("/beads/" + door.ID).body)
	for _, want := range []string{
		`<span class="wv-sep">·</span><span class="wv-run">working</span></p>`,
		`<p><span class="wv-dot wv-dot--run"></span>running <span class="wv-faint wv-mono">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the agent's bead's page lacks %s", want)
		}
	}
}

// The card stream places a bead a manual group claims as not working, and
// one an agent's session claims as working: what a reload shows.
func TestFlowsStreamManualClaim(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "backlog", Runner: "manual"}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake"}, "me"))
	idea, err := f.st.CreateBead(ctx, store.NewBead{Title: "an idea", Stage: "backlog", Depot: "line"}, "me")
	must(t, err)
	door, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	head, err := f.st.Head(ctx)
	must(t, err)
	s := f.stream(t, fmt.Sprintf("/events?after=%d&format=card", head))

	_, err = f.st.Claim(ctx, idea.ID, "backlog", "backlog", "yard")
	must(t, err)
	if c := put(s.next(t)); !strings.Contains(c, `id="bead/`+idea.ID+`"`) || strings.Contains(c, "working") {
		t.Errorf("the manual claim's card: %s", c)
	}
	_, err = f.st.Claim(ctx, door.ID, "builders", "new", "yard")
	must(t, err)
	if c := put(s.next(t)); !strings.Contains(c, `id="bead/`+door.ID+`"`) || !strings.Contains(c, `<span class="dot working"></span>`) || !strings.Contains(c, "· working") {
		t.Errorf("the agent's bead's card: %s", c)
	}
	s.quiet(t)
}

// asked counts what the view asks the store for.
type asked struct {
	store.API
	mu sync.Mutex
	n  map[string]int
}

func (a *asked) count(what string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n[what]++
}

func (a *asked) reset() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	was := a.n
	a.n = map[string]int{}
	return was
}

func (a *asked) ListBeads(ctx context.Context, f store.BeadFilter) ([]store.Bead, error) {
	a.count("ListBeads")
	return a.API.ListBeads(ctx, f)
}

func (a *asked) Sessions(ctx context.Context, all bool) ([]store.Session, error) {
	if all {
		a.count("Sessions, ended ones too")
	} else {
		a.count("Sessions")
	}
	return a.API.Sessions(ctx, all)
}

func (a *asked) ListSessions(ctx context.Context, f store.SessionFilter) ([]store.Session, error) {
	a.count("ListSessions of " + f.Bead)
	return a.API.ListSessions(ctx, f)
}

// A bead's page reads that bead's sessions, ended ones too, and the live
// ones: never every session the yard keeps, which took a second at 20,000
// beads (yardr-wav4).
func TestBeadPageReadsItsOwnSessions(t *testing.T) {
	t.Parallel()
	st := &asked{API: storetest.New(t), n: map[string]int{}}
	srv, err := web.NewServer(st, filepath.Join(t.TempDir(), ".depot"), nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: st, ts: ts}
	ctx := context.Background()
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake", Limit: 5}, "me"))
	var beads []*store.Bead
	var ids []string
	for _, title := range []string{"a door", "a shed"} {
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Stage: "new"}, "me")
		must(t, err)
		beads = append(beads, b)
	}
	// The door is worked twice, its first session dead; the shed once.
	for _, b := range []*store.Bead{beads[0], beads[1], beads[0]} {
		sess, err := f.st.Claim(ctx, b.ID, "builders", "new", "yard")
		must(t, err)
		ids = append(ids, sess.ID)
		if len(ids) == 1 {
			must(t, f.st.EndSession(ctx, sess.ID, "died", "gone"))
		}
	}
	st.reset()
	var page web.BeadPage
	f.getJSON("/beads/"+beads[0].ID, &page)
	got := st.reset()
	// The live sessions twice: the page's own read and the estimates'
	// shared queue snapshot, each once per response, never per bead.
	if got["ListSessions of "+beads[0].ID] != 1 || got["Sessions"] != 2 || got["Sessions, ended ones too"] != 0 {
		t.Errorf("the bead's page asked the store for %v, want its own sessions once and the live ones twice", got)
	}
	if len(page.Sessions) != 2 || page.Sessions[0].ID != ids[0] || page.Sessions[1].ID != ids[2] {
		t.Errorf("the bead's sessions = %+v, want %s then %s", page.Sessions, ids[0], ids[2])
	}
	if !page.Working {
		t.Error("the bead has a live session and the page does not say it is worked")
	}
}

func (a *asked) Events(ctx context.Context, bead string, limit int) ([]store.Event, error) {
	a.count("Events")
	return a.API.Events(ctx, bead, limit)
}

// /beads costs what its open beads cost, and each of them once: the list
// and the header's count are made from one listing and the live sessions,
// and a bead's events are read only where the waits say something from
// them: who held a bead, whether an agent's session waits on a prompt. A
// manual group's claim runs nothing that could, and a yard has one on
// every parked bead.
func TestBeadsReadsTheYardOnce(t *testing.T) {
	t.Parallel()
	st := &asked{API: storetest.New(t), n: map[string]int{}}
	srv, err := web.NewServer(st, filepath.Join(t.TempDir(), ".depot"), nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: st, ts: ts}
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "loose", Base: "origin/main"}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Limit: 5}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "fake", Limit: 5}, "me"))
	_, err = f.st.AddRoute(ctx, store.Route{Stage: "decide", Group: "decisions"}, "me")
	must(t, err)
	bead := func(title, stage, depot string) *store.Bead {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Stage: stage, Depot: depot}, "me")
		must(t, err)
		return b
	}
	for _, title := range []string{"which colour", "which size", "which wood"} {
		sess, err := f.st.Claim(ctx, bead(title, "decide", "line").ID, "decisions", "decide", "yard")
		must(t, err)
		must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"manual": true}))
	}
	door := bead("a door", "new", "line")
	sess, err := f.st.Claim(ctx, door.ID, "builders", "new", "yard")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "door-new"}))
	must(t, f.st.LogDispatch(ctx, door.ID, "session_blocked", "yard", map[string]any{"session": sess.ID}))
	_, err = f.st.SetHold(ctx, bead("a shed", "inbox", "loose").ID, true, store.Expect{}, "me")
	must(t, err)
	// A bead of no depot is in the list and in no depot's count.
	bead("a thought", "inbox", "")

	home := f.get("/").body
	// The line keeps the waits in its centre and no aside beside them.
	whole := f.get("/line").body
	if strings.Contains(whole, "<aside") {
		t.Errorf("/line keeps an aside beside the centre that says the same: %s", whole)
	}
	st.reset()
	r := f.get("/beads")
	got := st.reset()
	// The live sessions twice: the page's own read and the estimates'
	// shared queue snapshot, each once per response, never per bead.
	if want := map[string]int{"ListBeads": 1, "Sessions": 2, "Events": 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("/beads asked the store for %v, want %v", got, want)
	}
	// /beads counts what waits in the rail's badge instead: the blocked
	// bead, the held one and the three parked, and no aside.
	if tile := tileOf(t, r.body, "/"); !strings.Contains(tile, `title="5 wait for you"`) {
		t.Errorf("/beads counts no waiting bead in its rail: %s", tile)
	}
	if strings.Contains(r.body, "<aside") {
		t.Errorf("/beads keeps a column the window has no place for: %s", r.body)
	}
	// The line's list holds the five that wait, each with its reason, and its
	// subline the open beads.
	waits := listRowsOf(t, f, "/line", "Waits for you")
	if len(waits) != 5 {
		t.Errorf("the line's list holds %+v, want the five that wait", waits)
	}
	reasons := map[string]bool{}
	for _, r := range waits {
		reasons[reasonLine(r)] = true
		if len(r.lines) > 0 {
			reasons[r.lines[0]] = true
		}
	}
	for _, want := range []string{"its session waits on a prompt: door-new", "held by me"} {
		if !reasons[want] {
			t.Errorf("the line's list lacks %q: %+v", want, waits)
		}
	}
	if sub := squash(stageOf(t, whole)); !strings.Contains(sub, `<a href="/flows">5 open</a>`) {
		t.Errorf("the line's subline lacks the 5 open beads: %s", sub)
	}
	// Home has no aside: the line's numbers are its subline's.
	homeText := squash(stageOf(t, home))
	for _, not := range []string{`<dt>open</dt>`, `Parked · 3`, `<article class="wait">`, "<aside"} {
		if strings.Contains(homeText, not) {
			t.Errorf("home has %s: %s", not, homeText)
		}
	}
	if waits := section2(t, listSections(t, home), "Waits for you"); waits.count != "5" {
		t.Errorf("Decide's list holds %+v, want the five that wait", waits)
	}
	var list web.Beads
	st.reset()
	f.getJSON("/beads", &list)
	if want := []string{"a thought", "a shed", "a door", "which wood", "which size", "which colour"}; !reflect.DeepEqual(titles(cards(list.Beads)), want) {
		t.Errorf("beads %v, want %v", titles(cards(list.Beads)), want)
	}
	if got := st.reset(); !reflect.DeepEqual(got, map[string]int{"ListBeads": 1, "Sessions": 2}) {
		t.Errorf("/beads as JSON asked the store for %v, want the beads once and the live sessions twice", got)
	}
	f.getJSON("/beads?depot=loose", &list)
	if want := []string{"a shed"}; !reflect.DeepEqual(titles(cards(list.Beads)), want) || !reflect.DeepEqual(list.Stages, []string{"inbox"}) {
		t.Errorf("loose: beads %v (want %v), stages %v", titles(cards(list.Beads)), want, list.Stages)
	}
}
