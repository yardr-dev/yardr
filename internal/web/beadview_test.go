package web_test

import (
	"context"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// Tests of the bead's page as a stage of its own kind (templates/bead.html):
// what each part of it holds and where it stands. The pages' other tests read
// what they need of it through these cuts.

// foldOf is the bead's fold name on its page, after its id up to its closing
// tag. The folds hold no fold of their own.
func foldOf(t *testing.T, body, bead, name string) string {
	t.Helper()
	return regionOf(t, body, `id="fold-`+name+`-`+bead+`"`, `</details>`)
}

// stop is one stop of the route strip as the page draws it.
type stop struct {
	name      string
	past, now bool
}

var stopRE = regexp.MustCompile(`<li class="wv-stop( wv-stop--past)?( wv-stop--now)?"( aria-current="step")?><span class="wv-stop__dot"></span>([^<]*)</li>`)

// routeOf are the stops of the page's route strip, in the order it draws them.
func routeOf(t *testing.T, body string) []stop {
	t.Helper()
	var out []stop
	for _, m := range stopRE.FindAllStringSubmatch(body, -1) {
		if (m[2] != "") != (m[3] != "") {
			t.Errorf("the stop %s is current without saying so, or says so without being: %q", m[4], m[0])
		}
		out = append(out, stop{name: m[4], past: m[1] != "", now: m[2] != ""})
	}
	return out
}

// strayStore reads every bead as standing in limbo, a stage no flow has.
type strayStore struct{ store.API }

func (s strayStore) GetBead(ctx context.Context, id string) (*store.Bead, error) {
	b, err := s.API.GetBead(ctx, id)
	if err != nil {
		return nil, err
	}
	stray := *b
	stray.Stage = "limbo"
	return &stray, nil
}

// The route strip draws one stop to each stage of the bead's flow, in the
// flow's order with its terminals last, the bead's own current and the stages
// before it past. A bead whose stage its flow lacks, or that no flow governs,
// has its own stage as a last stop, so the strip always says where it is. The
// JSON carries the order as the page reads it.
func TestBeadPageRouteStrip(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, _, b := whole(t, f)
	ctx := context.Background()
	// The flow is "new -> review on done, new -> decide on question, review ->
	// merged, merged terminal": its stages as written, its terminal last.
	got := routeOf(t, f.get("/beads/"+b.ID).body)
	want := []stop{{"new", false, true}, {"review", false, false}, {"decide", false, false}, {"merged", false, false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the route is %v, want %v", got, want)
	}
	var page web.BeadPage
	f.getJSON("/beads/"+b.ID, &page)
	if !slices.Equal(page.Route, []string{"new", "review", "decide", "merged"}) {
		t.Errorf("the page's JSON route is %v", page.Route)
	}
	// Moved on, the stops before its own are past.
	_, err := f.st.Advance(ctx, b.ID, "review", store.Expect{}, "me")
	must(t, err)
	got = routeOf(t, f.get("/beads/"+b.ID).body)
	want = []stop{{"new", true, false}, {"review", false, true}, {"decide", false, false}, {"merged", false, false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the move the route is %v, want %v", got, want)
	}
	// A bead in a stage its flow lacks (a flow written again leaves one behind
	// where the yard has no open bead in it).
	srv, err := web.NewServer(strayStore{f.st}, f.home, nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	stray := &fixture{t: t, client: testClient(t), st: f.st, ts: ts, home: f.home}
	got = routeOf(t, stray.get("/beads/"+b.ID).body)
	want = []stop{{"new", true, false}, {"review", true, false}, {"decide", true, false}, {"merged", true, false}, {"limbo", false, true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a bead the flow lacks has the route %v, want %v", got, want)
	}
	// A bead no flow governs: its stage alone.
	loose, err := f.st.CreateBead(ctx, store.NewBead{Title: "a thought", Stage: "inbox"}, "me")
	must(t, err)
	if got, want := routeOf(t, f.get("/beads/"+loose.ID).body), []stop{{"inbox", false, true}}; !reflect.DeepEqual(got, want) {
		t.Errorf("a bead no flow governs has the route %v, want %v", got, want)
	}
}

// The meta line says where the bead stands after its ids and places: held in
// red first, then waits for you in rust where it has an ask for the viewer or a
// move they may make, else working in green where a session works it; a closed
// bead says so with its reason, and one with none of them says nothing.
func TestBeadPageMeta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _ := talkFixture(t, "jo")
	b, _ := actYard(t, f, pickAsk())
	meta := func(g *fixture, id string) string {
		t.Helper()
		return regionOf(t, g.get("/beads/"+id).body, `<p class="wv-meta">`, `</p>`)
	}
	const sep = `<span class="wv-sep">·</span>`
	ids := `<span class="wv-id">` + b.ID + `</span>` + sep + `task` + sep + `P0` + sep + `home` + sep + `no flow`
	if got, want := meta(f, b.ID), ids+sep+`<span class="wv-wait">waits for you</span>`; got != want {
		t.Errorf("a bead asked of the viewer: %q, want %q", got, want)
	}
	_, err := f.st.SetHold(ctx, b.ID, true, store.Expect{}, "jo")
	must(t, err)
	if got, want := meta(f, b.ID), ids+sep+`<span class="wv-fault">held</span>`+sep+`<span class="wv-wait">waits for you</span>`; got != want {
		t.Errorf("a held bead asked of the viewer: %q, want %q", got, want)
	}
	// Seen by a view that acts as no one, the ask is nobody's to answer.
	g := newFixture(t)
	must(t, g.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
	plain, err := g.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "home"}, "me")
	must(t, err)
	if got, want := meta(g, plain.ID), `<span class="wv-id">`+plain.ID+`</span>`+sep+`task`+sep+`P0`+sep+`home`+sep+`no flow`; got != want {
		t.Errorf("a bead that waits for no one: %q, want %q", got, want)
	}
	_, err = g.st.CloseBead(ctx, plain.ID, "fixed", store.Expect{}, "me")
	must(t, err)
	if got, want := meta(g, plain.ID), `<span class="wv-id">`+plain.ID+`</span>`+sep+`task`+sep+`P0`+sep+`home`+sep+`no flow`+sep+`closed: fixed`; got != want {
		t.Errorf("a closed bead: %q, want %q", got, want)
	}
}

// Blocked by stands for the open beads that must close before the bead is
// worked, as rows of the stage, and only while something blocks it: a blocker
// that closed, and a bead that blocks, have none. It stands above the asks,
// the form that sends the bead on, and the words.
func TestBeadPageBlockedBy(t *testing.T) {
	t.Parallel()
	f, _ := talkFixture(t, "jo")
	b, _ := actYard(t, f, pickAsk())
	actFlow(t, f)
	ctx := context.Background()
	_, err := f.st.Advance(ctx, b.ID, "decide", store.Expect{}, "jo")
	must(t, err)
	body := "Hang it on two hinges."
	_, err = f.st.UpdateBead(ctx, b.ID, store.BeadPatch{Body: &body}, store.Expect{}, "jo")
	must(t, err)
	posts, err := f.st.CreateBead(ctx, store.NewBead{Title: "the posts", Stage: "new", Depot: "home"}, "jo")
	must(t, err)
	must(t, f.st.AddDep(ctx, posts.ID, b.ID, store.DepBlocks, "", "", "jo"))

	page := f.get("/beads/" + b.ID).body
	if want := `<h2 class="wv-sh"><span>Blocked by</span><span class="wv-sh__count">1</span></h2>`; !strings.Contains(page, want) {
		t.Fatalf("the blocked bead's page lacks %s:\n%s", want, page)
	}
	if want := `<a class="wv-srow" href="/beads/` + posts.ID + `">`; !strings.Contains(page, want) {
		t.Errorf("the blocker is no row of the stage: no %s", want)
	}
	// The page stands in the order a person acts on it.
	last := -1
	for _, part := range []string{`class="wv-head"`, `<span>Blocked by</span>`, `class="wv-ask"`, `<span>Send on as jo</span>`,
		`<span>The bead</span>`, `<span>Notes</span>`, `id="folds"`} {
		at := strings.Index(page, part)
		if at < 0 || at < last {
			t.Errorf("%s stands at %d, not after what the page has before it (%d)", part, at, last)
		}
		last = at
	}
	// A bead that blocks, and one whose blocker has closed, have none.
	if strings.Contains(f.get("/beads/"+posts.ID).body, "Blocked by") {
		t.Error("the bead that blocks is blocked")
	}
	_, err = f.st.CloseBead(ctx, posts.ID, "built", store.Expect{}, "jo")
	must(t, err)
	if strings.Contains(f.get("/beads/"+b.ID).body, "Blocked by") {
		t.Error("a bead whose blocker closed is still blocked")
	}
}

// The folds stand in this order: Facts, Linked beads, Stage <name>, Sessions,
// History. Each has an id of its own, naming the bead, by which a page that
// reads itself again keeps an open one open (yard.js); a fold that would hide
// nothing is not drawn.
func TestBeadPageFoldsInOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	train, _, b := whole(t, f)
	// The train has children, a stage and a file; the bead a session.
	for _, id := range []string{train.ID, b.ID} {
		body := f.get("/beads/" + id).body
		folds := regionOf(t, body, `id="folds"`, `</section>`)
		var labels []string
		for _, m := range regexp.MustCompile(`<details class="wv-fold" id="(fold-[a-z]+-[^"]+)"><summary><svg[^>]*><path[^>]*/></svg><span class="wv-fold__label">([^<]*)</span>`).FindAllStringSubmatch(squash(folds), -1) {
			name := strings.Split(m[1], "-")[1]
			if m[1] != "fold-"+name+"-"+id {
				t.Errorf("the fold %s has the id %s, which does not name the bead %s", m[2], m[1], id)
			}
			labels = append(labels, m[2])
		}
		if want := []string{"Facts", "Linked beads", "Stage new", "Sessions", "History"}; !slices.Equal(labels, want) {
			t.Errorf("%s: the folds are %v, want %v", id, labels, want)
		}
		if n := strings.Count(folds, "<details"); n != 5 {
			t.Errorf("%s: %d folds, want 5", id, n)
		}
	}
	// A bead with no link and no stage file has the others only: nothing is
	// folded that hides nothing.
	lone, err := f.st.CreateBead(context.Background(), store.NewBead{Title: "a thought", Stage: "inbox"}, "me")
	must(t, err)
	folds := regionOf(t, f.get("/beads/"+lone.ID).body, `id="folds"`, `</section>`)
	for _, absent := range []string{"Linked beads", "Stage inbox"} {
		if strings.Contains(folds, absent) {
			t.Errorf("a bead with nothing to fold has a %s fold", absent)
		}
	}
	for _, present := range []string{"Facts", "Sessions", "History"} {
		if !strings.Contains(folds, `<span class="wv-fold__label">`+present+`</span>`) {
			t.Errorf("the lone bead's page lacks the %s fold", present)
		}
	}
	// A fold a person opened stays open when the page reads itself again, by
	// the id each fold has: yard.js does it.
	js := string(readAsset(t, "yard.js"))
	for _, want := range []string{`document.addEventListener("htmx:beforeSwap"`, `details.wv-fold[open][id]`, `fold.open = true`} {
		if !strings.Contains(js, want) {
			t.Errorf("yard.js does not keep an open fold open: no %s", want)
		}
	}
}

// The body is its first paragraph with its lines kept, and the whole under
// Read it whole where more follows. A body of one paragraph has no fold, and
// a bead with none has no section.
func TestBeadPageBody(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	for _, c := range []struct {
		name, body string
		prose      string // the first paragraph as the page draws it
		fold       string // what Read it whole says, "" for no fold
		count      string
	}{
		{"two paragraphs", "Hang it.\n\n- on two hinges", "Hang it.", "3 lines", "3 lines"},
		{"one paragraph of lines", "Hang it\non two hinges.", "Hang it\non two hinges.", "", "2 lines"},
		{"one line", "Hang it.", "Hang it.", "", "1 line"},
		{"blank lines before it", "\n\n  \nHang it.\n\nThen oil it.\n", "Hang it.", "6 lines", "6 lines"},
		{"blank lines after it", "Hang it.\n\n\n", "Hang it.", "", "1 line"},
	} {
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: c.name, Body: c.body, Stage: "inbox"}, "me")
		must(t, err)
		page := f.get("/beads/" + b.ID).body
		section := regionOf(t, page, `id="body"`, `</section>`)
		if want := `<span class="wv-sh__count">` + c.count + `</span>`; !strings.Contains(section, want) {
			t.Errorf("%s: the section lacks %s: %s", c.name, want, section)
		}
		if want := `<div class="wv-prose"><p>` + c.prose + `</p></div>`; !strings.Contains(section, want) {
			t.Errorf("%s: the section lacks %q: %s", c.name, want, section)
		}
		if c.fold == "" {
			if strings.Contains(section, "Read it whole") {
				t.Errorf("%s: the body has a fold that hides nothing", c.name)
			}
			continue
		}
		want := `<span class="wv-fold__label">Read it whole</span><span class="wv-fold__value">` + c.fold + `</span>`
		if !strings.Contains(section, want) || !strings.Contains(section, `<pre class="wv-text">`+strings.TrimRight(c.body, "\n")) {
			t.Errorf("%s: the section lacks %s and the whole body: %s", c.name, want, section)
		}
	}
	empty, err := f.st.CreateBead(ctx, store.NewBead{Title: "no words", Stage: "inbox"}, "me")
	must(t, err)
	if strings.Contains(f.get("/beads/"+empty.ID).body, `id="body"`) {
		t.Error("a bead with no body has a section for it")
	}
}

// standsInDecide fails the test where the page does not stand in Decide, with
// the bead's row open where the viewer's list has one (row), or reads itself
// again at an address other than the bead's own.
func standsInDecide(t *testing.T, label, body, bead string, row bool) {
	t.Helper()
	if got := marked(railOf(t, body), "aria-current", "true"); !slices.Equal(got, []string{"/"}) {
		t.Errorf("%s: the rail opens %v, want Decide", label, got)
	}
	if got := listTitle(t, body); got != "Decide" {
		t.Errorf("%s: the list is %s's, want Decide's", label, got)
	}
	var want []string
	if row {
		want = []string{"/beads/" + bead}
	}
	if got := marked(listOf(t, body), "aria-current", "page"); !slices.Equal(got, want) {
		t.Errorf("%s: the list opens %v, want %v", label, got, want)
	}
	for _, leaf := range startTags(stageOf(t, body), "i") {
		if leaf["hx-get"] != "/beads/"+bead {
			t.Errorf("%s: the page reads %q again, want the bead's own address", label, leaf["hx-get"])
		}
	}
}

// htmx keeps what a person typed (hx-preserve) by looking the field up as the
// selector #id, and a swap whose selector does not parse throws and leaves the
// page half drawn. So every id on the bead's page is a plain identifier, none
// of them with a slash or a dot of a name of the yard in it, as the ids
// built for the page are by elementID.
func TestBeadPageIDsAreSelectors(t *testing.T) {
	t.Parallel()
	ident := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	check := func(label, body string) {
		t.Helper()
		seen := 0
		for _, tag := range startTags(stageOf(t, body), "[a-z]+") {
			if id, ok := tag["id"]; ok {
				seen++
				if !ident.MatchString(id) {
					t.Errorf("%s: the id %q is no plain identifier, which htmx finds by a selector", label, id)
				}
			}
		}
		if seen < 5 {
			t.Errorf("%s: %d ids on the page: the check reads nothing", label, seen)
		}
	}
	f := newFixture(t)
	train, _, b := whole(t, f)
	check("the train's page", f.get("/beads/"+train.ID).body)
	check("the bead's page", f.get("/beads/"+b.ID).body)
	// A page that acts: a card, and the form that sends the bead on.
	g, _ := talkFixture(t, "jo")
	asked, _ := actYard(t, g, pickAsk())
	actFlow(t, g)
	_, err := g.st.Advance(context.Background(), asked.ID, "decide", store.Expect{}, "jo")
	must(t, err)
	page := g.get("/beads/" + asked.ID).body
	check("the page of a bead that acts", page)
	if !strings.Contains(page, `hx-preserve="true" name="note"`) || !strings.Contains(page, `hx-preserve="true" name="words"`) {
		t.Errorf("the page keeps no typed words:\n%s", page)
	}
	// Whatever the names are, the ids built from them are identifiers.
	for _, c := range []struct {
		parts []string
		want  string
	}{
		{[]string{"facts", "yard-1a2b"}, "fold-facts-yard-1a2b"},
		{[]string{"note", "my.depot-1a2b", "re/view"}, "fold-note-my_depot-1a2b-re_view"},
	} {
		if got := web.ElementID("fold", c.parts...); got != c.want {
			t.Errorf("ElementID(%q) = %q, want %q", c.parts, got, c.want)
		}
	}
}
