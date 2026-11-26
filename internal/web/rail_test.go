package web_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/web"
)

// tileOf is the anchor of the rail's tile that opens href.
func tileOf(t *testing.T, body, href string) string {
	t.Helper()
	rail := railOf(t, body)
	at := strings.Index(rail, `<a class="wv-tile" href="`+href+`"`)
	if at < 0 {
		t.Fatalf("the rail has no tile for %s: %s", href, rail)
	}
	end := strings.Index(rail[at:], `</a>`)
	return rail[at : at+end]
}

// countingInbox wraps a store and counts the inbox reads: the shell reads
// the acting user's inbox once per request.
type countingInbox struct {
	store.API
	n *int
}

func (s countingInbox) UserInbox(ctx context.Context, name string, limit int, actor string) (*store.Mailbox, error) {
	*s.n++
	return s.API.UserInbox(ctx, name, limit, actor)
}

// The shell reads the acting user's inbox once per request: the talk page
// passes its own read through for the Crew badge and its list's marks
// instead of a second scan, and a page that reads none costs one.
func TestRailReadsInboxOnce(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/talk", "/crew", "/"} {
		st := storetest.New(t)
		var n int
		home := filepath.Join(t.TempDir(), ".depot")
		srv, err := web.NewServer(countingInbox{API: st, n: &n}, home, nil)
		must(t, err)
		token := srv.ActAs("jo")
		ts := httptest.NewServer(srv)
		t.Cleanup(func() { shut(ts) })
		f := &fixture{t: t, client: testClient(t), st: st, ts: ts, home: home}
		f.signIn(srv, token)
		ctx := context.Background()
		must(t, func() error { _, err := st.AddUser(ctx, "jo", "jo"); return err }())
		must(t, func() error {
			_, err := st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr"}, "jo")
			return err
		}())
		if r := f.get(path); r.status != 200 || strings.Contains(r.body, `class="wv-badge"`) {
			t.Errorf("%s has a badge with nothing waiting and no unread mail: %s", path, r.body)
		}
		n = 0
		must(t, st.Mail(ctx, "jo", "check the gate", "yardmaster"))
		r := f.get(path)
		if r.status != 200 {
			t.Fatalf("%s: status %d: %s", path, r.status, r.body)
		}
		if !strings.Contains(r.body, `title="Crew — 1 unread letter" aria-label="Crew, 1 unread letter"`) {
			t.Errorf("%s renders no Crew count from its one inbox read: %s", path, railOf(t, r.body))
		}
		if n != 1 {
			t.Errorf("%s reads the inbox %d times, want once", path, n)
		}
		// The list's marks come from the same read: the member who wrote
		// is marked on a page that stands in Crew.
		if path != "/" && !strings.Contains(listOf(t, r.body), `<span class="wv-row__end wv-row__end--wait">unread mail</span>`) {
			t.Errorf("%s does not mark the unread mail in Crew's list: %s", path, listOf(t, r.body))
		}
	}
	// Where the view acts as no user the shell reads no inbox at all.
	st := storetest.New(t)
	var n int
	home := filepath.Join(t.TempDir(), ".depot")
	srv, err := web.NewServer(countingInbox{API: st, n: &n}, home, nil)
	must(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: st, ts: ts, home: home}
	for _, path := range []string{"/", "/crew"} {
		if r := f.get(path); r.status != 200 {
			t.Fatalf("%s: status %d: %s", path, r.status, r.body)
		}
	}
	if n != 0 {
		t.Errorf("a view that acts as no one reads the inbox %d times, want none", n)
	}
}

// The rail is the four modes in its order, Settings at the foot, the yard's
// chip at its head, which opens Decide: each tile opens its mode's root page
// and says its name, and the open one stands marked.
func TestRailTiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := f.get("/").body
	rail := railOf(t, body)
	tiles := regexp.MustCompile(`<a class="wv-tile" href="([^"]*)" title="([^"]*)" aria-label="([^"]*)"( aria-current="true")?>`)
	var got []string
	for _, m := range tiles.FindAllStringSubmatch(rail, -1) {
		got = append(got, m[1]+" "+m[2]+" "+m[3]+m[4])
	}
	want := []string{
		`/ Decide Decide aria-current="true"`,
		`/flows Lines Lines`,
		`/crew Crew Crew`,
		`/config Settings Settings`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the rail's tiles are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Settings stands after the foot's spacer, apart from the three modes.
	foot, settings := strings.Index(rail, `<span class="wv-rail__foot"></span>`), strings.Index(rail, `href="/config"`)
	if foot < 0 || settings < foot || strings.Index(rail, `href="/crew"`) > foot {
		t.Errorf("Settings does not stand at the rail's foot: %s", rail)
	}
	// The chip is the yard's first two letters, named in full, and opens
	// Decide. The fixture's yard is named for its home, depot.
	if want := `<a class="wv-chip" href="/" title="depot" aria-label="depot">de</a>`; !strings.Contains(rail, want) {
		t.Errorf("the rail lacks %s: %s", want, rail)
	}
	// Under 900 px the chip stands in the stage's top bar beside the mode's
	// name, and the list opens by its link.
	top := regionOf(t, body, `<div class="wv-top">`, `</div>`)
	for _, want := range []string{`<a class="wv-chip" href="/" title="depot" aria-label="depot">de</a>`,
		`<span class="wv-top__mode">Decide</span>`, `<a class="wv-top__list" href="#drawer">List`} {
		if !strings.Contains(top, want) {
			t.Errorf("the top bar lacks %s: %s", want, top)
		}
	}
	if !strings.Contains(body, `<a class="wv-scrim" href="#stage" aria-label="Close the list"></a>`) {
		t.Errorf("the page has no scrim that shuts the drawer")
	}
	// The tiles open each mode's root: a page of the mode marks its own.
	for path, tile := range map[string]string{"/flows": "/flows", "/crew": "/crew", "/talk": "/crew", "/config": "/config", "/beads": "/flows"} {
		if got := marked(railOf(t, f.get(path).body), "aria-current", "true"); len(got) != 1 || got[0] != tile {
			t.Errorf("%s opens the tile %v, want %s", path, got, tile)
		}
	}
}

// Decide's badge counts what waits for the person, the number the list's
// head says, and its title and aria-label say it in words; Crew's counts the
// unread letters, 9+ past nine; Lines and Settings carry none. No badge
// stands at zero, and none on an error page.
func TestRailBadges(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	body := f.get("/beads").body
	decide := tileOf(t, body, "/")
	for _, want := range []string{`title="2 wait for you" aria-label="Decide, 2 wait for you"`, `<span class="wv-badge" aria-hidden="true">2</span>`} {
		if !strings.Contains(decide, want) {
			t.Errorf("Decide's tile lacks %s: %s", want, decide)
		}
	}
	// The list's head, on a page that stands in Decide, says the same.
	if head := regionOf(t, f.get("/").body, `<header class="wv-list__head" id="list-head">`, `</header>`); !strings.Contains(head, "2 wait for you") {
		t.Errorf("the head does not say the badge's count: %s", head)
	}
	crew := tileOf(t, body, "/crew")
	for _, want := range []string{`title="Crew — 1 unread letter" aria-label="Crew, 1 unread letter"`, `<span class="wv-badge" aria-hidden="true">1</span>`} {
		if !strings.Contains(crew, want) {
			t.Errorf("Crew's tile lacks %s: %s", want, crew)
		}
	}
	for _, href := range []string{"/flows", "/config"} {
		if tile := tileOf(t, body, href); strings.Contains(tile, "wv-badge") {
			t.Errorf("the tile for %s carries a badge: %s", href, tile)
		}
	}
	// Past nine the numeral is 9+ and the words keep the count.
	ctx := context.Background()
	for i := range 10 {
		must(t, f.st.Mail(ctx, "mckean", "letter "+string(rune('a'+i)), "scribe"))
	}
	crew = tileOf(t, f.get("/config").body, "/crew")
	if !strings.Contains(crew, `<span class="wv-badge" aria-hidden="true">9&#43;</span>`) || !strings.Contains(crew, "11 unread letters") {
		t.Errorf("Crew's tile past nine: %s", crew)
	}
	// Nothing waits on an error page, which stands under no mode.
	r := f.get("/nowhere")
	if r.status != 404 || strings.Contains(railOf(t, r.body), "wv-badge") || strings.Contains(railOf(t, r.body), "aria-current") {
		t.Errorf("the error page's rail: %d %s", r.status, railOf(t, r.body))
	}
	// A yard where nothing waits has no Decide badge, and its head says none.
	quiet := newFixture(t)
	if r := quiet.get("/"); strings.Contains(r.body, "wv-badge") || strings.Contains(listOf(t, r.body), "wv-list__count") {
		t.Errorf("a quiet yard carries a count: %s", r.body)
	}
}

// An error page keeps the shell bare: the rail with no open tile and no
// count, the list's head alone, and the sentence on the stage; its stream is
// none, and its answer is the one a missing page has as JSON.
func TestErrorPageIsBare(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	r := f.get("/nowhere")
	if r.status != 404 || !strings.Contains(stageOf(t, r.body), "no page at /nowhere") {
		t.Fatalf("status %d, want 404 with the sentence on the stage: %s", r.status, r.body)
	}
	if listTitle(t, r.body) != "depot" || strings.Contains(listOf(t, r.body), "wv-row") || strings.Contains(listOf(t, r.body), "<details") {
		t.Errorf("the list is not its head alone: %s", listOf(t, r.body))
	}
	if strings.Contains(r.body, "sse-connect") {
		t.Errorf("an error page opens a stream")
	}
	if n := len(startTags(railOf(t, r.body), "a")); n != 5 {
		t.Errorf("the bare rail has %d links, want the chip and four tiles", n)
	}
	// A bead that is gone is the same page, from its own address.
	if r := f.get("/beads/none-0000"); r.status != 404 || strings.Contains(r.body, "wv-row") {
		t.Errorf("a missing bead: %d %s", r.status, listOf(t, r.body))
	}
}
