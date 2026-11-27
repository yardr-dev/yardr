package web_test

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/telemetry"
	"yardr.dev/yardr/internal/web"
)

// The window is the rail of four modes, the open mode's list and the stage
// (docs/web.md, The window). What these tests read of a page is the three
// regions the shell has by id, so each helper below cuts one out of the HTML
// the server sent.

// regionOf is the markup of the element that opens with open, up to its
// closing tag. The three regions have no element of their own tag inside.
func regionOf(t *testing.T, body, open, closing string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, open)
	if !ok {
		t.Fatalf("the page has no %s:\n%s", open, body)
	}
	got, _, ok := strings.Cut(rest, closing)
	if !ok {
		t.Fatalf("the page's %s never closes", open)
	}
	return got
}

func railOf(t *testing.T, body string) string {
	t.Helper()
	return regionOf(t, body, `<nav class="wv-rail" id="rail"`, `</nav>`)
}

func listOf(t *testing.T, body string) string {
	t.Helper()
	return regionOf(t, body, `<nav class="wv-list" id="list"`, `</nav>`)
}

func stageOf(t *testing.T, body string) string {
	t.Helper()
	return regionOf(t, body, `<main class="wv-stage" id="stage">`, `</main>`)
}

// startTags are the attributes of every start tag named name in markup, with
// the values as a browser reads them. A value may hold a '>' (hx-select does).
func startTags(markup, name string) []map[string]string {
	tag := regexp.MustCompile(`<` + name + `\b((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	attr := regexp.MustCompile(`([A-Za-z_:][-A-Za-z0-9_:.]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)
	var out []map[string]string
	for _, m := range tag.FindAllStringSubmatch(markup, -1) {
		attrs := map[string]string{}
		for _, a := range attr.FindAllStringSubmatch(m[1], -1) {
			attrs[a[1]] = html.UnescapeString(a[2] + a[3] + a[4])
		}
		out = append(out, attrs)
	}
	return out
}

// marked are the hrefs of the links of markup that carry mark.
func marked(markup, mark, value string) []string {
	var out []string
	for _, a := range startTags(markup, "a") {
		if v, ok := a[mark]; ok && v == value {
			out = append(out, a["href"])
		}
	}
	return out
}

// listTitle is the list's head: the open mode's name.
func listTitle(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`<h2 id="list-title">([^<]*)</h2>`).FindStringSubmatch(listOf(t, body))
	if m == nil {
		t.Fatalf("the list has no head:\n%s", listOf(t, body))
	}
	return html.UnescapeString(m[1])
}

// sameWhole is body without what a second request moves: the digits of ages
// and of the stream's head.
func sameWhole(body string) string {
	return regexp.MustCompile(`[0-9]+`).ReplaceAllString(body, "#")
}

// windowYard is a yard with something in every mode's list: mckean, the
// operator the view acts as, with a bead that asks them a question, a bead
// held, a session running, jo and the crew members yardmaster and scribe
// with one mail from the yardmaster unread, and the depot line with its
// flows.
func windowYard(t *testing.T) (f *fixture, asked *store.Bead) {
	t.Helper()
	ctx := context.Background()
	api, own := storetest.NewOwned(t)
	own.SetTelemetrySwitch(telemetry.Switch{})
	// The fixture names the home the server runs in, as talkFixture's does: a
	// source's stand-in starts there.
	home := filepath.Join(t.TempDir(), ".depot")
	srv, err := web.NewServer(api, home, nil)
	must(t, err)
	token := srv.ActAs("mckean")
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f = &fixture{t: t, client: testClient(t), st: api, own: own, ts: ts, home: home}
	f.signIn(srv, token)
	talkYard(t, f)
	_, err = f.st.AddUser(ctx, "mckean", "jo")
	must(t, err)
	must(t, f.st.SetSetting(ctx, store.OperatorSetting, "mckean", "jo"))
	must(t, f.st.Mail(ctx, "mckean", "the shed is painted", "yardmaster"))
	f.populate()
	three := f.byTitle()["three"]
	asked = &three
	_, err = f.st.AddAsk(ctx, asked.ID, pickAsk(), "yardmaster")
	must(t, err)
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 5}, "me"))
	run := f.byTitle()["one"]
	sess, err := f.st.Claim(ctx, run.ID, "builders", "backlog", "dispatcher")
	must(t, err)
	must(t, f.st.MarkRunning(ctx, sess.ID, map[string]any{"agent": "one"}))
	return f, asked
}

// shellOpen is the opening tag of the shell of a page whose stream starts
// after head in format ("" for the feed's entries): the swap's contract of
// docs/web.md, Swaps, which every page carries.
func shellOpen(head uint64, format string) string {
	stream := "/events?after=" + strconv.FormatUint(head, 10)
	if format != "" {
		stream += "&amp;format=" + format
	}
	return `<div class="wv-shell" hx-ext="sse" sse-connect="` + stream + `" hx-boost="true" hx-push-url="true" hx-target="#stage" hx-select="#stage > *" ` +
		`hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list-head:innerHTML,#list-rows:innerHTML" hx-swap="innerHTML scroll:top">`
}

// leafOf is a live page's leaf, reading path again.
func leafOf(path, swap string) string {
	return `<i class="wv-live" hidden hx-get="` + html.EscapeString(path) + `" hx-trigger="sse:message" hx-push-url="false" hx-swap="` + swap +
		`" hx-select-oob="#rail:outerHTML,#list-head:innerHTML,#list-rows:innerHTML"></i>`
}

// windowRoute is one row of the page map: the address, the tile it stands
// under, the list's head, the row of the list it opens, "" where none, and the
// stage kind the page draws itself as (docs/web.md, The stage kinds).
type windowRoute struct{ path, tile, list, open, kind string }

// Every page of the page map answers the whole window: the rail with the
// tile of its mode open, the mode's list with its head and the row the page
// opens marked, and the stage. The mode and the row belong to the address,
// not to where the link was.
func TestEveryPageIsTheWholeShell(t *testing.T) {
	t.Parallel()
	f, asked := windowYard(t)
	routes := []windowRoute{
		{"/", "/", "Decide", "", "overview"},
		{"/line", "/", "Decide", "", "overview"},
		{"/beads/" + asked.ID, "/", "Decide", "/beads/" + asked.ID, "bead"},
		{"/beads/" + f.byTitle()["six"].ID, "/", "Decide", "", "bead"},
		{"/flows", "/flows", "Lines", "/flows", "flow"},
		{"/depots", "/flows", "Lines", "/depots", "table"},
		{"/depots/line", "/flows", "Lines", "/depots/line", "flow"},
		{"/depots/line/code", "/flows", "Lines", "/depots/line/code", "flow"},
		{"/beads", "/flows", "Lines", "/beads", "list"},
		{"/sessions", "/flows", "Lines", "/sessions", "list"},
		{"/previews", "/flows", "Lines", "/previews", "list"},
		{"/deps", "/flows", "Lines", "/deps", "list"},
		{"/crew", "/crew", "Crew", "/talk?with=yardmaster", "talk"},
		{"/talk", "/crew", "Crew", "/talk?with=yardmaster", "talk"},
		{"/talk?with=jo", "/crew", "Crew", "/talk?with=jo", "talk"},
		{"/config", "/config", "Settings", "/config", "table"},
		{"/groups", "/config", "Settings", "/groups", "table"},
		{"/routes", "/config", "Settings", "/routes", "table"},
		{"/peers", "/config", "Settings", "/peers", "table"},
		{"/limits", "/config", "Settings", "/limits", "table"},
		{"/telemetry", "/config", "Settings", "/telemetry", "table"},
	}
	check := func(t *testing.T, get func(path string) resp, rt windowRoute) {
		t.Helper()
		r := get(rt.path)
		if r.status != http.StatusOK {
			t.Errorf("%s: status %d", rt.path, r.status)
			return
		}
		rail := railOf(t, r.body)
		if got := marked(rail, "aria-current", "true"); !slices.Equal(got, []string{rt.tile}) {
			t.Errorf("%s: the rail opens %v, want the tile %s: %s", rt.path, got, rt.tile, rail)
		}
		if n := strings.Count(rail, `class="wv-tile"`); n != 4 {
			t.Errorf("%s: the rail has %d tiles, want Decide, Lines, Crew and Settings: %s", rt.path, n, rail)
		}
		if got := listTitle(t, r.body); got != rt.list {
			t.Errorf("%s: the list is %s's, want %s's", rt.path, got, rt.list)
		}
		var want []string
		if rt.open != "" {
			want = []string{rt.open}
		}
		if got := marked(listOf(t, r.body), "aria-current", "page"); !slices.Equal(got, want) {
			t.Errorf("%s: the list marks %v open, want %v: %s", rt.path, got, want, listOf(t, r.body))
		}
		if !strings.Contains(listOf(t, r.body), `id="list-rows"`) || !strings.Contains(listOf(t, r.body), `id="list-head"`) {
			t.Errorf("%s: the list has no head and rows", rt.path)
		}
		if !strings.Contains(r.body, `<span id="drawer" class="wv-flag"></span>`) {
			t.Errorf("%s: the page has no drawer marker", rt.path)
		}
		if !strings.Contains(r.body, `<main class="wv-stage" id="stage">`) {
			t.Errorf("%s: the page has no stage", rt.path)
		}
		if strings.Contains(r.body, "<aside") || strings.Contains(r.body, "needs-count") {
			t.Errorf("%s: the page draws a column or a count the window has no place for", rt.path)
		}
		// Every page of the map draws itself as its stage kind, with a head of
		// its own: the shell's heading is for a page that is in no row.
		if got := regexp.MustCompile(`<article class="[^"]*" data-kind="([a-z]+)"`).FindStringSubmatch(stageOf(t, r.body)); got == nil || got[1] != rt.kind {
			t.Errorf("%s: the stage draws itself as %v, want the kind %s", rt.path, got, rt.kind)
		}
		if strings.Contains(r.body, `class="page-head"`) {
			t.Errorf("%s: the shell's heading stands over a page that has a head of its own", rt.path)
		}
	}
	for _, rt := range routes {
		check(t, func(path string) resp { return f.get(path) }, rt)
	}

	// Behind the gateway: a delegated record, which is Crew's, with the
	// row of the member the record names open, and a pack's file page,
	// which is Decide's with its bead's row open.
	y, bead, packFile := delegatedPages(t)
	for _, rt := range []windowRoute{
		{"/delegations/1", "/crew", "Crew", "/talk?with=yardmaster", "talk"},
		{packFile, "/", "Decide", "/beads/" + bead, "bead"},
	} {
		check(t, func(path string) resp { return y.getNoFollow(path, y.token("jo@example.net")) }, rt)
	}
}

// delegatedPages is a yard behind the gateway with one delegated record, at
// /delegations/1, and the first file page of a pack's full text, whose address
// it returns after the id of the bead that asks for the pack.
func delegatedPages(t *testing.T) (y *approveYard, bead, packFile string) {
	t.Helper()
	ctx := context.Background()
	y = newApproveYard(t, true)
	must(t, y.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Config: map[string]any{"kind": "claude"}}, "jo"))
	must(t, y.st.Mail(ctx, "yardmaster", "put the builders on codex", "jo"))
	mail, err := y.st.Head(ctx)
	must(t, err)
	dctx := store.WithDelegation(ctx, store.DelegatedCall{For: "jo", Mail: mail, Command: "yardr group set builders kind=codex"})
	_, err = y.st.SetGroup(dctx, "builders", store.GroupEdit{Set: map[string]any{"kind": "codex"}}, "yardmaster")
	must(t, err)
	starter, _ := starterReading()
	y.own.SetPackReader(func(_ context.Context, name, depot string, _ store.Mappings, _ store.API) (*store.PackReading, error) {
		if name != "starter" {
			return nil, errors.New("no pack " + name)
		}
		r := *starter
		r.Depot = depot
		return &r, nil
	})
	b, err := y.st.CreateBead(ctx, store.NewBead{Title: "install starter", Stage: "new", Depot: "home"}, "yardmaster")
	must(t, err)
	seq, err := y.st.AddAsk(ctx, b.ID, store.Ask{Question: "Install starter?", To: "jo", Context: []string{"the depot has no way of working"},
		Run: &store.AskRun{Argv: []string{"pack apply starter --depot home"}}}, "yardmaster")
	must(t, err)
	return y, b.ID, "/beads/" + b.ID + "/asks/" + strconv.FormatUint(seq, 10) + "/files/1"
}

// An htmx request gets the whole page, as a plain one does: the server has
// no partial templates, and hx-select picks the stage. The same holds for a
// history restore (back and forward), which htmx asks for with its own header
// as an htmx request too, and puts in the body: a direct load, a reload and a
// restore all need the rail, the list and the stage in the answer.
func TestHtmxRequestGetsTheWholePage(t *testing.T) {
	t.Parallel()
	f, asked := windowYard(t)
	y, _, packFile := delegatedPages(t)
	type route struct {
		path string
		get  func(path string, headers ...string) resp
	}
	acting := func(path string, headers ...string) resp { return f.get(path, headers...) }
	gateway := func(path string, headers ...string) resp {
		return y.getNoFollow(path, y.token("jo@example.net"), headers...)
	}
	routes := []route{
		{"/", acting}, {"/line", acting}, {"/beads/" + asked.ID, acting}, {"/flows", acting}, {"/beads?depot=line", acting},
		{"/depots/line", acting}, {"/depots/line/code", acting}, {"/crew", acting}, {"/talk?with=jo", acting},
		{"/config", acting}, {"/groups", acting}, {"/nowhere", acting},
		{"/delegations/1", gateway}, {packFile, gateway},
	}
	for _, rt := range routes {
		plain := rt.get(rt.path)
		for _, h := range []struct {
			name    string
			headers []string
		}{
			{"an htmx request", []string{"HX-Request", "true"}},
			{"a boosted link", []string{"HX-Request", "true", "HX-Boosted", "true", "HX-Target", "stage"}},
			{"a history restore", []string{"HX-Request", "true", "HX-History-Restore-Request", "true"}},
			{"a history restore with no HX-Request", []string{"HX-History-Restore-Request", "true"}},
		} {
			got := rt.get(rt.path, h.headers...)
			if got.status != plain.status {
				t.Errorf("%s as %s: status %d, want the plain request's %d", rt.path, h.name, got.status, plain.status)
				continue
			}
			for name, region := range map[string]func(*testing.T, string) string{"rail": railOf, "list": listOf, "stage": stageOf} {
				if a, b := sameWhole(region(t, got.body)), sameWhole(region(t, plain.body)); a != b {
					t.Errorf("%s as %s: the %s differs from the plain page's:\n%s\nwant\n%s", rt.path, h.name, name, a, b)
				}
			}
			if !strings.HasPrefix(got.body, "<!doctype html>") || sameWhole(got.body) != sameWhole(plain.body) {
				t.Errorf("%s as %s: not the whole page the plain request gets", rt.path, h.name)
			}
			if v := got.header.Get("Vary"); !strings.Contains(v, "Accept") || got.header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s as %s: Vary %q, Cache-Control %q", rt.path, h.name, v, got.header.Get("Cache-Control"))
			}
		}
	}
}

// Nothing needs script to be read or used: every tile, list row and link of
// the stage is an <a href> to a page that answers 200, every form has a
// method and an action, and the drawer opens by #drawer in the address
// alone, by the marker and the stylesheet.
func TestNoScriptNeeded(t *testing.T) {
	t.Parallel()
	f, asked := windowYard(t)
	pages := []string{"/", "/line", "/beads/" + asked.ID, "/flows", "/depots", "/depots/line", "/depots/line/code", "/beads", "/sessions",
		"/previews", "/deps", "/crew", "/talk", "/talk?with=jo", "/config", "/groups", "/routes", "/peers", "/limits", "/telemetry"}
	seen := map[string]bool{}
	for _, page := range pages {
		body := f.get(page).body
		for _, region := range []struct{ name, markup string }{{"rail", railOf(t, body)}, {"list", listOf(t, body)}, {"stage", stageOf(t, body)}} {
			links := startTags(region.markup, "a")
			if region.name != "stage" && len(links) == 0 {
				t.Errorf("%s: the %s has no link", page, region.name)
			}
			for _, a := range links {
				href, ok := a["href"]
				if !ok || href == "" {
					t.Errorf("%s: a link of the %s has no href: %v", page, region.name, a)
					continue
				}
				if strings.HasPrefix(href, "#") || seen[href] {
					continue
				}
				seen[href] = true
				if !strings.HasPrefix(href, "/") {
					t.Errorf("%s: the %s links %q, which leaves the view", page, region.name, href)
					continue
				}
				// A download answers 200 or the page it was made from.
				if strings.HasPrefix(href, "/blobs/") {
					continue
				}
				if r := f.get(href); r.status != http.StatusOK {
					t.Errorf("%s: the %s links %s, which answers %d", page, region.name, href, r.status)
				}
			}
			if region.name != "stage" && strings.Contains(region.markup, "<form") {
				t.Errorf("%s: the %s holds a form", page, region.name)
			}
		}
		for _, form := range startTags(body, "form") {
			if form["method"] == "" || form["action"] == "" {
				t.Errorf("%s: a form has no method and action: %v", page, form)
			}
		}
		for _, bad := range []string{"<script", "onclick=", "onsubmit=", "javascript:"} {
			if strings.Contains(stageOf(t, body)+listOf(t, body)+railOf(t, body), bad) {
				t.Errorf("%s: the window carries %s", page, bad)
			}
		}
		// The phone's drawer is the list, opened by the link that names
		// the marker and shut by the ones that name the stage.
		if !slices.Contains(marked(stageOf(t, body), "href", "#drawer"), "#drawer") {
			t.Errorf("%s: the stage has no link that opens the drawer", page)
		}
		if got := marked(body, "href", "#stage"); len(got) != 2 {
			t.Errorf("%s: %d links shut the drawer, want its Close and the scrim", page, len(got))
		}
	}
	css := string(readAsset(t, "yard.css"))
	for _, want := range []string{
		":has(#drawer:target) .wv-list {",
		":has(#drawer:target) .wv-scrim {",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks %s: the drawer opens by #drawer alone", want)
		}
	}
}

// A page that follows the yard has one leaf: a childless element that reads
// the page's own address again at every event of the stream, and puts the
// stage, the rail and the list's head and rows in place. It is childless
// because htmx's attributes inherit: put on the page's root they would reach
// every link in it and stop them pushing an address. A snapshot has none, and
// no other element of any page listens to the stream's messages.
func TestOneLiveLeaf(t *testing.T) {
	t.Parallel()
	f, asked := windowYard(t)
	leaf := regexp.MustCompile(`<i class="wv-live" hidden [^>]*></i>`)
	live := []struct{ path, swap string }{
		{"/", "none ignoreTitle:true"},
		{"/?all=1", "none ignoreTitle:true"},
		{"/line", "innerHTML ignoreTitle:true"},
		{"/beads/" + asked.ID, "innerHTML ignoreTitle:true"},
		{"/beads", "innerHTML ignoreTitle:true"},
		{"/beads?depot=line&stage=backlog", "innerHTML ignoreTitle:true"},
		{"/sessions", "innerHTML ignoreTitle:true"},
		{"/previews", "innerHTML ignoreTitle:true"},
		{"/deps", "innerHTML ignoreTitle:true"},
		{"/depots", "innerHTML ignoreTitle:true"},
		{"/groups", "innerHTML ignoreTitle:true"},
		{"/routes", "innerHTML ignoreTitle:true"},
		{"/peers", "innerHTML ignoreTitle:true"},
		{"/limits", "innerHTML ignoreTitle:true"},
		{"/crew", "innerHTML ignoreTitle:true"},
		{"/talk?with=jo", "innerHTML ignoreTitle:true"},
	}
	for _, p := range live {
		body := f.get(p.path).body
		stage := stageOf(t, body)
		leaves := leaf.FindAllString(stage, -1)
		if len(leaves) != 1 || strings.Count(body, `class="wv-live"`) != 1 {
			t.Errorf("%s: %d leaves, want one: %s", p.path, len(leaves), stage)
			continue
		}
		tag := startTags(leaves[0], "i")[0]
		if tag["hx-push-url"] != "false" || tag["hx-trigger"] != "sse:message" || tag["hx-swap"] != p.swap {
			t.Errorf("%s: the leaf is %v, want hx-push-url false, hx-trigger sse:message and hx-swap %s", p.path, tag, p.swap)
		}
		if tag["hx-get"] != p.path {
			t.Errorf("%s: the leaf reads %q, want the page's own address", p.path, tag["hx-get"])
		}
		for _, want := range []string{"#rail:outerHTML", "#list-head:innerHTML", "#list-rows:innerHTML"} {
			if !strings.Contains(tag["hx-select-oob"], want) {
				t.Errorf("%s: the leaf's out-of-band select %q lacks %s", p.path, tag["hx-select-oob"], want)
			}
		}
		if strings.Contains(tag["hx-select-oob"], "#drawer") || strings.Contains(tag["hx-select-oob"], "#list:") {
			t.Errorf("%s: a live read must leave the drawer's marker and the list's element: %q", p.path, tag["hx-select-oob"])
		}
		// Nothing else in the window listens to the stream's messages but
		// the shell's own connection and, on home, the feed's fragments.
		if n := strings.Count(body, `hx-trigger="sse:message"`); n != 1 {
			t.Errorf("%s: %d elements read the page on an event, want its leaf alone", p.path, n)
		}
	}
	snapshots := []string{"/flows", "/depots/line", "/depots/line/code", "/config", "/telemetry", "/nowhere"}
	older, err := f.st.Head(context.Background())
	must(t, err)
	snapshots = append(snapshots, "/?before="+strconv.FormatUint(older, 10))
	for _, path := range snapshots {
		body := f.get(path).body
		if strings.Contains(body, "wv-live") || strings.Contains(body, `hx-trigger="sse:message"`) {
			t.Errorf("%s: a snapshot has a leaf: %s", path, stageOf(t, body))
		}
	}
	y, _, packFile := delegatedPages(t)
	for _, path := range []string{"/delegations/1", packFile} {
		if body := y.getNoFollow(path, y.token("jo@example.net")).body; strings.Contains(body, "wv-live") {
			t.Errorf("%s: a snapshot has a leaf", path)
		}
	}
}

// readAsset is a file of the view's assets as the server embeds it.
func readAsset(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("assets/" + name)
	must(t, err)
	return b
}

// What swaps the fragments the stream sends into itself says so: the shell's
// target is the stage, which such an element would inherit, and a card told to
// delete itself would delete the stage. Home's feed and /flows' sections and
// cards are the elements, and the fragment the stream brings carries it too.
func TestStreamedFragmentsTargetThemselves(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	seen := 0
	for _, path := range []string{"/", "/flows"} {
		for _, tag := range startTags(f.get(path).body, "[a-z]+") {
			if _, ok := tag["sse-swap"]; !ok {
				continue
			}
			seen++
			if tag["hx-target"] != "this" {
				t.Errorf("%s: an element that swaps the stream's fragments has hx-target %q, want this: %v", path, tag["hx-target"], tag)
			}
		}
	}
	if seen < 4 {
		t.Errorf("found %d elements that swap the stream's fragments, want the feed, the sections and the cards", seen)
	}
}
