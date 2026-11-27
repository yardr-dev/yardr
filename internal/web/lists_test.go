package web_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/limits"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// printed is v as a command prints it with --json, read back: what a client
// of either has.
func printed(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	must(t, err)
	var out any
	must(t, json.Unmarshal(raw, &out))
	return out
}

// listOf reads a route's answer: the list under key and the head.
func (f *fixture) listOf(target, key string, headers ...string) (list any, head uint64) {
	f.t.Helper()
	r := f.get(target, headers...)
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.body), &got); err != nil || r.status != 200 {
		f.t.Fatalf("%s: status %d, %v: %s", target, r.status, err, r.body)
	}
	if ct := r.header.Get("Content-Type"); ct != "application/json" {
		f.t.Errorf("%s: Content-Type %q", target, ct)
	}
	if got[key] == nil || got["head"] == nil {
		f.t.Fatalf("%s: no %q or no head: %s", target, key, r.body)
	}
	must(f.t, json.Unmarshal(got[key], &list))
	must(f.t, json.Unmarshal(got["head"], &head))
	return list, head
}

// The lists answer what their list commands print with --json, under the
// route's name, with the yard's head, and an empty list for a yard that
// has none. Each has a page, so only a client that asks for JSON gets it.
// Peers are the operator's to read, so the lists are read as the
// operator's.
func TestLists(t *testing.T) {
	t.Parallel()
	f, _ := talkFixture(t, "ada")
	ctx := context.Background()
	must(t, f.st.SetSetting(ctx, store.OperatorSetting, "ada", "ada"))
	routes := map[string]func() any{
		"depots": func() any { v, err := f.st.Depots(ctx); must(t, err); return v },
		"groups": func() any { v, err := f.st.Groups(ctx); must(t, err); return v },
		"routes": func() any { v, err := f.st.Routes(ctx); must(t, err); store.SortRoutes(v); return v },
		"peers":  func() any { v, err := f.st.Peers(ctx); must(t, err); return v },
		"deps": func() any {
			v, err := f.st.AllDeps(ctx)
			must(t, err)
			byID := map[string]store.Bead{}
			for _, b := range f.byTitle() {
				byID[b.ID] = b
			}
			rows := make([]web.DepRow, 0, len(v))
			for _, d := range v {
				end := func(id string) web.DepEnd {
					b := byID[id]
					return web.DepEnd{Title: b.Title, Stage: b.Stage, Status: b.Status}
				}
				rows = append(rows, web.DepRow{Dep: d, FromBead: end(d.From), ToBead: end(d.To)})
			}
			return rows
		},
	}
	paged := map[string]bool{"depots": true, "groups": true, "routes": true, "peers": true, "deps": true}
	for key := range routes {
		if list, _ := f.listOf("/"+key, key, "Accept", "application/json"); !reflect.DeepEqual(list, []any{}) {
			t.Errorf("/%s of an empty yard: %v, want an empty list", key, list)
		}
		if paged[key] {
			if r := f.get("/" + key); r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
				t.Errorf("/%s of an empty yard: status %d, type %q, want the page", key, r.status, r.header.Get("Content-Type"))
			}
		}
	}

	f.populate()
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 3, Config: map[string]any{"kind": "claude"}}, "ada"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Members: []string{"jo"}}, "me"))
	line, bug := "line", "bug"
	for _, r := range []store.Route{
		{Stage: "new", Group: "builders"},
		{Stage: "new", Depot: &line, Type: &bug, Group: "decisions"},
	} {
		_, err := f.st.AddRoute(ctx, r, "me")
		must(t, err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	must(t, err)
	_, err = f.st.AddPeer(ctx, store.Peer{Name: "north", Key: store.Public(key), URL: "http://127.0.0.1:1"}, "ada")
	must(t, err)
	beads := f.byTitle()
	must(t, f.st.AddDep(ctx, beads["one"].ID, beads["three"].ID, store.DepBlocks, "", "", "me"))
	must(t, f.st.AddDep(ctx, beads["three"].ID, beads["five"].ID, store.DepDiscoveredFrom, "", "", "me"))

	head, err := f.st.Head(ctx)
	must(t, err)
	for key, read := range routes {
		// No Accept: a list with no page answers its JSON instead.
		jsonOnly := [][]string{nil, {"Accept", "application/json"}, {"Accept", "text/html"}}
		if paged[key] {
			jsonOnly = [][]string{{"Accept", "application/json"}}
		}
		for _, headers := range jsonOnly {
			list, got := f.listOf("/"+key, key, headers...)
			if want := printed(t, read()); !reflect.DeepEqual(list, want) || len(list.([]any)) == 0 {
				t.Errorf("/%s %v\n got %v\nwant %v", key, headers, list, want)
			}
			if got != head {
				t.Errorf("/%s: head %d, want %d", key, got, head)
			}
		}
		if paged[key] {
			// A browser, plain curl and */* get the page.
			for _, headers := range [][]string{nil, {"Accept", "text/html"}, {"Accept", "*/*"}} {
				r := f.get("/"+key, headers...)
				if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
					t.Errorf("/%s %v: status %d, type %q, want the page", key, headers, r.status, r.header.Get("Content-Type"))
				}
			}
		}
	}

	// What a second front end picks from them is there by name.
	var groups web.Groups
	f.getJSON("/groups", &groups)
	if g := groups.Groups[0]; g.Name != "builders" || g.Runner != "herdr" || g.Limit != 3 || g.Config["kind"] != "claude" ||
		!slices.Equal(groups.Groups[1].Members, []string{"jo"}) {
		t.Errorf("groups: %+v", groups.Groups)
	}
	// The routes in match order: the one that asks more first.
	var rs web.Routes
	f.getJSON("/routes", &rs)
	if len(rs.Routes) != 2 || rs.Routes[0].Group != "decisions" || *rs.Routes[0].Type != "bug" {
		t.Errorf("routes: %+v", rs.Routes)
	}
	var depots web.Depots
	f.getJSON("/depots", &depots)
	if len(depots.Depots) != 2 || depots.Depots[0].Name != "line" || depots.Depots[0].Base != "origin/main" {
		t.Errorf("depots: %+v", depots.Depots)
	}
	// Every edge once, though each is in the list of both its beads.
	var deps web.Deps
	f.getJSON("/deps", &deps)
	var edges []string
	for _, d := range deps.Deps {
		edges = append(edges, d.From+" "+d.Kind+" "+d.To)
	}
	want := []string{beads["one"].ID + " blocks " + beads["three"].ID, beads["three"].ID + " discovered-from " + beads["five"].ID}
	if !slices.Equal(edges, want) {
		t.Errorf("deps: %v, want %v", edges, want)
	}
	// A list is read only, as every page.
	if r := f.do("POST", "/groups"); r.status != 405 {
		t.Errorf("POST /groups: status %d", r.status)
	}
}

// The depot, group and route pages render the rows their list commands
// print: `yardr depot list`, `yardr group list` and `yardr route list`.
// The routes stand in match order, as the command prints them.
func TestConfigPages(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Path: "/tmp/line", Base: "origin/main", Test: "make test"}, "me"))
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "docs", Path: "/tmp/docs", Kind: store.DepotDir}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 3,
		Config: map[string]any{"kind": "claude", "model": "opus"}}, "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual", Members: []string{"jo", "mo"}}, "me"))
	line, bug := "line", "bug"
	plain, err := f.st.AddRoute(ctx, store.Route{Stage: "new", Group: "builders"}, "me")
	must(t, err)
	narrow, err := f.st.AddRoute(ctx, store.Route{Stage: "new", Depot: &line, Type: &bug, Label: &bug, Group: "decisions"}, "me")
	must(t, err)

	// The depots are a table, the fourth stage kind, a row to each depot as
	// `yardr depot list` prints it; a directory depot has no base and no test
	// command, and its cells say so, not leave a row of facts out.
	depots := squash(f.get("/depots").body)
	for _, want := range []string{
		`<article class="wv-page" data-kind="table">`,
		`<h1 class="wv-title">Depots</h1><p class="wv-meta">3 depots</p>`,
		`<thead><tr><th>name</th><th>kind</th><th>path</th><th>base</th><th>test</th></tr></thead>`,
		`<tr><td class="wv-name">line</td><td>git</td><td><code>/tmp/line</code></td><td><code>origin/main</code></td><td><code>make test</code></td></tr>`,
		`<tr><td class="wv-name">docs</td><td>dir</td><td><code>/tmp/docs</code></td><td><span class="wv-nil">–</span></td><td><span class="wv-nil">–</span></td></tr>`,
		`<a class="wv-row" href="/depots" aria-current="page">`,
	} {
		if !strings.Contains(depots, want) {
			t.Errorf("/depots lacks %s", want)
		}
	}
	// Depots is a row of Lines' list, so the page has no way back to the hub.
	if strings.Contains(depots, "‹ Config") {
		t.Error("/depots links back to the hub")
	}

	// The groups are a table, a row to each group as `yardr group list` prints
	// them; a group's runner settings fold under its row, one key a line, and a
	// group with none has no fold.
	groups := squash(f.get("/groups").body)
	for _, want := range []string{
		`<article class="wv-page" data-kind="table">`,
		`<h1 class="wv-title">Groups</h1><p class="wv-meta">2 groups</p>`,
		`<thead><tr><th>name</th><th>runner</th><th class="wv-num">limit</th><th>members</th></tr></thead>`,
		`<tr><td class="wv-name">builders</td><td>herdr</td><td class="wv-num">3</td><td><span class="wv-nil">–</span></td></tr>` +
			`<tr class="wv-table__fold"><td colspan="4"><details class="wv-fold"><summary>` + chevron +
			`<span class="wv-fold__label">config</span><span class="wv-fold__value">kind claude · model opus</span></summary>`,
		`<dl class="wv-facts"><dt>kind</dt><dd>claude</dd><dt>model</dt><dd>opus</dd></dl>`,
		`<tr><td class="wv-name">decisions</td><td>manual</td><td class="wv-num">50</td><td>jo, mo</td></tr>`,
		`<a class="wv-row" href="/groups" aria-current="page">`,
	} {
		if !strings.Contains(groups, want) {
			t.Errorf("/groups lacks %s", want)
		}
	}
	// The group without a config has no fold: one, for builders.
	if n := strings.Count(groups, `<tr class="wv-table__fold">`); n != 1 {
		t.Errorf("/groups has %d config folds, want one (for builders)", n)
	}

	// The routes are a table in match order, a row to each route as `yardr
	// route list` prints them, the id quiet at the end, and the head says the
	// rule the order is read by.
	routes := squash(f.get("/routes").body)
	for _, want := range []string{
		`<article class="wv-page" data-kind="table">`,
		`<h1 class="wv-title">Routes</h1><p class="wv-meta">2 routes, in match order: the first that matches takes the bead</p>`,
		`<th>stage</th><th>type</th><th>depot</th><th>label</th><th>beads</th><th>group</th><th>id</th>`,
		`<tr><td class="wv-name">new</td><td>bug</td><td>line</td><td>bug</td><td><span class="wv-nil">–</span></td><td>decisions</td><td class="wv-id">` + narrow.ID + `</td></tr>`,
		`<tr><td class="wv-name">new</td><td><span class="wv-nil">–</span></td><td><span class="wv-nil">–</span></td><td><span class="wv-nil">–</span></td><td><span class="wv-nil">–</span></td><td>builders</td><td class="wv-id">` + plain.ID + `</td></tr>`,
		`<a class="wv-row" href="/routes" aria-current="page">`,
	} {
		if !strings.Contains(routes, want) {
			t.Errorf("/routes lacks %s", want)
		}
	}
	// Routes carry no priority, so the view shows none, and the table is the
	// one layout at every width: no stacked cards for a phone.
	for _, gone := range []string{"<dt>priority</dt>", "routes-cards", "route-id"} {
		if strings.Contains(routes, gone) {
			t.Errorf("/routes still draws %s", gone)
		}
	}
	// One table row per route, besides the header's.
	if n := strings.Count(routes, `<tr><td class="wv-name">`); n != 2 {
		t.Errorf("/routes has %d table rows, want one per route", n)
	}
	// In match order: the higher priority first.
	if strings.Index(routes, narrow.ID) > strings.Index(routes, plain.ID) {
		t.Error("/routes does not stand in match order")
	}

	// An empty yard says so on each page.
	empty := newFixture(t)
	for path, want := range map[string]string{
		"/depots": "This yard has no depots.",
		"/groups": "This yard has no groups.",
		"/routes": "This yard has no routes.",
	} {
		if body := empty.get(path).body; !strings.Contains(body, want) {
			t.Errorf("%s of an empty yard lacks %q", path, want)
		}
	}
}

// The peer and edge pages render the rows their list commands print:
// `yardr peer list` and every bead's `yardr dep list`, each edge once, a
// staged edge with the stages it names. Peers are the operator's to read,
// so both pages are read as the operator's.
func TestConfigPagesPeersDeps(t *testing.T) {
	t.Parallel()
	f, _ := talkFixture(t, "ada")
	f.populate()
	ctx := context.Background()
	must(t, f.st.SetSetting(ctx, store.OperatorSetting, "ada", "ada"))
	_, key, err := ed25519.GenerateKey(nil)
	must(t, err)
	_, err = f.st.AddPeer(ctx, store.Peer{Name: "north", Key: store.Public(key), URL: "http://127.0.0.1:1"}, "ada")
	must(t, err)
	_, key, err = ed25519.GenerateKey(nil)
	must(t, err)
	_, err = f.st.AddPeer(ctx, store.Peer{Name: "laptop", Key: store.Public(key)}, "ada")
	must(t, err)
	_, err = f.st.AllowPeer(ctx, "north", store.PeerAllow{Send: []string{"ping", "mail"}, Receive: []string{"ping"}}, "ada")
	must(t, err)

	// The peers are a table, a row to each peer as `yardr peer list` prints
	// them: the url of a peer that cannot be called says it calls this yard.
	peers := squash(f.get("/peers").body)
	for _, want := range []string{
		`<article class="wv-page" data-kind="table">`,
		`<h1 class="wv-title">Peers</h1><p class="wv-meta">2 peers</p>`,
		`<thead><tr><th>peer</th><th>url</th><th>send</th><th>receive</th><th class="wv-num">waiting</th></tr></thead>`,
		`<tr><td class="wv-name">north</td><td><code>http://127.0.0.1:1</code></td><td>mail, ping</td><td>ping</td><td class="wv-num">0</td></tr>`,
		`<tr><td class="wv-name">laptop</td><td><span class="wv-nil">none — it calls this yard</span></td><td><span class="wv-nil">–</span></td><td><span class="wv-nil">–</span></td><td class="wv-num">0</td></tr>`,
		`<a class="wv-row" href="/peers" aria-current="page">`,
	} {
		if !strings.Contains(peers, want) {
			t.Errorf("/peers lacks %s", want)
		}
	}

	beads := f.byTitle()
	one, three, five := beads["one"].ID, beads["three"].ID, beads["five"].ID
	must(t, f.st.AddDep(ctx, one, three, store.DepBlocks, "", "", "me"))
	must(t, f.st.AddDep(ctx, one, three, store.DepBlocks, "review", "", "me"))
	must(t, f.st.AddDep(ctx, three, five, store.DepDiscoveredFrom, "", "", "me"))
	deps := squash(f.get("/deps").body)
	for _, want := range []string{
		// The stage's own head, with the edges it lists counted.
		`<article class="wv-page" data-kind="list"><header class="wv-head"><h1 class="wv-title">Deps</h1><p class="wv-meta">3 edges with an open end, the oldest first</p></header>`,
		// The chips: the open ends by default, every edge behind all.
		`<nav class="wv-chips" aria-label="Edges"><p><span class="wv-chips__label">edges</span><a class="wv-pill" href="/deps" aria-current="true">open</a><a class="wv-pill" href="/deps?all=1">all</a></p></nav>`,
		// Each blocked bead as a row of the stage, with the edges that hold it
		// under it: the bead, the kind, its title cut short, its stage. A staged
		// edge names the stages it names. The links leave the mode, so their
		// block swaps the whole list.
		`<ol hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list:outerHTML"><li><a class="wv-srow" href="/beads/` + three + `">`,
		`<span class="wv-id">` + three + `</span><span class="wv-sep">·</span><span class="wv-nw">line</span><span class="wv-sep">·</span>new<span class="wv-sep">·</span>task`,
		`<span class="wv-srow__name">three</span>`,
		`<ul class="wv-waits"><li><a href="/beads/` + one + `">` + one + `</a><span class="wv-dim">blocks</span><span class="wv-waits__title">one</span><span class="wv-dim">backlog</span></li>`,
		`<li><a href="/beads/` + one + `">` + one + `</a><span class="wv-dim">blocks</span><span class="wv-dim">at review</span><span class="wv-waits__title">one</span><span class="wv-dim">backlog</span></li>`,
		`<span class="wv-id">` + five + `</span><span class="wv-sep">·</span><span class="wv-nw">line</span><span class="wv-sep">·</span>triage<span class="wv-sep">·</span>bug`,
		`<li><a href="/beads/` + three + `">` + three + `</a><span class="wv-dim">discovered-from</span><span class="wv-waits__title">three</span><span class="wv-dim">new</span></li>`,
		`<a class="wv-row" href="/deps" aria-current="page">`,
	} {
		if !strings.Contains(deps, want) {
			t.Errorf("/deps lacks %s", want)
		}
	}
	if strings.Contains(deps, `class="page-head"`) {
		t.Error("the shell's heading stands over the list page's own head")
	}
	// Grouped by the blocked bead, the longest waiting first: three's
	// oldest edge came before five's.
	if strings.Index(deps, `<span class="wv-srow__name">three</span>`) > strings.Index(deps, `<span class="wv-srow__name">five</span>`) {
		t.Error("/deps does not group by the blocked bead, longest waiting first")
	}

	// An empty yard says so on each page.
	empty, _ := talkFixture(t, "ada")
	must(t, empty.st.SetSetting(context.Background(), store.OperatorSetting, "ada", "ada"))
	for path, want := range map[string]string{
		"/peers": "This yard has no peers.",
		"/deps":  "This yard has no edges.",
	} {
		if body := empty.get(path).body; !strings.Contains(body, want) {
			t.Errorf("%s of an empty yard lacks %q", path, want)
		}
	}
	// With no edges at all, no toggle to all of them.
	if body := empty.get("/deps").body; strings.Contains(body, `aria-label="Edges"`) {
		t.Error("/deps of an empty yard shows the open/all toggle with nothing behind it")
	}
}

// /peers is the operator's to read: any other reader is refused, in HTML
// and in JSON, and told no URL and no key.
func TestPeersRefusedWithoutOperator(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.SetSetting(ctx, store.OperatorSetting, "ada", "ada"))
	must(t, func() error { _, err := f.st.AddUser(ctx, "sam", "ada"); return err }())
	_, key, err := ed25519.GenerateKey(nil)
	must(t, err)
	pub := store.Public(key)
	_, err = f.st.AddPeer(ctx, store.Peer{Name: "north", Key: pub, URL: "http://127.0.0.1:1"}, "ada")
	must(t, err)
	member := f.as(t, "sam")
	for _, v := range []struct {
		name string
		f    *fixture
	}{
		{"nobody", f},
		{"a member", member},
	} {
		for _, accept := range []string{"text/html", "application/json"} {
			r := v.f.get("/peers", "Accept", accept)
			if r.status != http.StatusForbidden {
				t.Errorf("%s (%s): /peers is %d, want 403", v.name, accept, r.status)
			}
			for _, told := range []string{"north", "http://127.0.0.1:1", pub} {
				if strings.Contains(r.body, told) {
					t.Errorf("%s (%s): the refusal tells %q", v.name, accept, told)
				}
			}
			// The page is the shell's 403 page: the sentence on a bare stage,
			// which has no row of Settings' list for a page the reader is
			// refused.
			if accept == "text/html" && (!strings.Contains(r.body, "the peers list is the operator&#39;s to read") ||
				strings.Contains(r.body, `href="/peers"`) || strings.Contains(r.body, `data-kind=`)) {
				t.Errorf("%s: the refusal is not the 403 page with the sentence and no row:\n%s", v.name, r.body)
			}
		}
		// Settings' list holds no row for either page of the operator's, on a
		// page the reader may open.
		if list := listOf(t, v.f.get("/groups").body); strings.Contains(list, `href="/peers"`) || strings.Contains(list, `href="/telemetry"`) {
			t.Errorf("%s: Settings' list has a row for a page the reader is refused: %s", v.name, list)
		}
	}
	if r := f.as(t, "ada").get("/peers"); r.status != 200 {
		t.Errorf("the operator: /peers is %d, want 200: %s", r.status, r.body)
	}
}

// By default /deps shows only the edges with an open end: an edge between
// two closed beads waits on nothing, and sits behind the link to all. An
// edge with one open end shows, whichever end is open, the closed end
// marked.
func TestDepsOpenEnds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	all, err := f.st.ListBeads(ctx, store.BeadFilter{All: true})
	must(t, err)
	ids := map[string]string{}
	for _, b := range all {
		ids[b.Title] = b.ID
	}
	one, three, four, five := ids["one"], ids["three"], ids["four"], ids["five"]
	must(t, f.st.AddDep(ctx, one, three, store.DepBlocks, "", "", "me"))
	must(t, f.st.AddDep(ctx, three, five, store.DepDiscoveredFrom, "", "", "me"))
	_, err = f.st.CloseBead(ctx, five, "dropped", store.Expect{}, "me")
	must(t, err)
	must(t, f.st.AddDep(ctx, four, five, store.DepBlocks, "", "", "me"))

	// The open-open and open-closed edges, grouped by the blocked bead;
	// the closed-closed edge is nowhere on the page.
	deps := squash(f.get("/deps").body)
	for _, want := range []string{
		`<span class="wv-srow__name">three</span>`,
		`<a href="/beads/` + one + `">` + one + `</a><span class="wv-dim">blocks</span><span class="wv-waits__title">one</span><span class="wv-dim">backlog</span></li>`,
		`<span class="wv-srow__name">five</span>`,
		`<a href="/beads/` + three + `">` + three + `</a><span class="wv-dim">discovered-from</span><span class="wv-waits__title">three</span><span class="wv-dim">new</span></li>`,
		leafOf("/deps", "innerHTML ignoreTitle:true"),
		`<a class="wv-pill" href="/deps?all=1">all</a>`,
	} {
		if !strings.Contains(deps, want) {
			t.Errorf("/deps lacks %s", want)
		}
	}
	// Five is closed, so its row says so; four's edge is behind the link.
	if !strings.Contains(deps, `<span class="wv-sep">·</span>bug<span class="wv-sep">·</span>closed</span>`) {
		t.Error("/deps does not mark the closed blocked bead closed")
	}
	if centre := stageOf(t, f.get("/deps").body); strings.Contains(centre, four) {
		t.Error("/deps shows the edge between two closed beads")
	}

	var got web.Deps
	f.getJSON("/deps", &got)
	if got.All || got.Total != 3 || len(got.Deps) != 2 || len(got.Groups) != 2 {
		t.Fatalf("/deps: all %v, total %d, %d edges in %d groups, want false, 3, 2, 2",
			got.All, got.Total, len(got.Deps), len(got.Groups))
	}
	if got.Groups[0].Bead.ID != three || len(got.Groups[0].Deps) != 1 ||
		got.Groups[1].Bead.ID != five || len(got.Groups[1].Deps) != 1 {
		t.Errorf("/deps groups: %+v", got.Groups)
	}
	if d := got.Deps[1]; d.From != three || d.To != five ||
		d.FromBead.Title != "three" || d.FromBead.Stage != "new" ||
		d.ToBead.Title != "five" || d.ToBead.Stage != "triage" || d.ToBead.Status != "closed" {
		t.Errorf("/deps carries %+v", d)
	}

	// Behind the link, every edge, the closed ends marked, and the way back.
	every := squash(f.get("/deps?all=1").body)
	for _, want := range []string{
		`<a href="/beads/` + four + `">` + four + `</a><span class="wv-dim">blocks</span><span class="wv-waits__title">four</span><span class="wv-dim">new · closed</span></li>`,
		leafOf("/deps?all=1", "innerHTML ignoreTitle:true"),
		`<p class="wv-meta">3 edges, closed ends included, the oldest first</p>`,
		`<a class="wv-pill" href="/deps">open</a>`,
		`<a class="wv-pill" href="/deps?all=1" aria-current="true">all</a>`,
	} {
		if !strings.Contains(every, want) {
			t.Errorf("/deps?all=1 lacks %s", want)
		}
	}
	f.getJSON("/deps?all=1", &got)
	if !got.All || got.Total != 3 || len(got.Deps) != 3 || len(got.Groups) != 2 {
		t.Fatalf("/deps?all=1: all %v, total %d, %d edges in %d groups, want true, 3, 3, 2",
			got.All, got.Total, len(got.Deps), len(got.Groups))
	}
	if len(got.Groups[1].Deps) != 2 || got.Groups[1].Deps[1].From != four {
		t.Errorf("/deps?all=1 groups: %+v", got.Groups)
	}

	// A parent edge points from the train to its child, but the train is
	// what waits for the child's work. Group it under the train and show
	// the child as the other end.
	train, err := f.st.CreateBead(ctx, store.NewBead{Title: "train", Type: store.TypeTrain, Depot: "line"}, "me")
	must(t, err)
	must(t, f.st.AddDep(ctx, train.ID, one, store.DepParent, "", "", "me"))
	parent := squash(f.get("/deps").body)
	if !strings.Contains(parent, `<span class="wv-srow__name">train</span>`) ||
		!strings.Contains(parent, `<a href="/beads/`+one+`">`+one+`</a><span class="wv-dim">parent</span><span class="wv-waits__title">one</span><span class="wv-dim">backlog</span></li>`) {
		t.Error("/deps does not group a parent edge under its waiting train with the child as its other end")
	}
	if strings.Contains(parent, `<span class="wv-srow__name">one</span>`) {
		t.Error("/deps groups the parent edge under its child instead of its waiting train")
	}

	// Only closed-only edges: the default says so, with the link to all.
	closed := newFixture(t)
	closed.populate()
	cbs, err := closed.st.ListBeads(ctx, store.BeadFilter{All: true})
	must(t, err)
	cids := map[string]string{}
	for _, b := range cbs {
		cids[b.Title] = b.ID
	}
	_, err = closed.st.CloseBead(ctx, cids["three"], "dropped", store.Expect{}, "me")
	must(t, err)
	must(t, closed.st.AddDep(ctx, cids["four"], cids["three"], store.DepBlocks, "", "", "me"))
	none := squash(closed.get("/deps").body)
	for _, want := range []string{
		`No edges with an open end.`,
		`<a class="wv-pill" href="/deps?all=1">all</a>`,
	} {
		if !strings.Contains(none, want) {
			t.Errorf("/deps without an open end lacks %s", want)
		}
	}
	if !strings.Contains(squash(closed.get("/deps?all=1").body), `<span class="wv-srow__name">three</span>`) {
		t.Error("/deps?all=1 without an open end shows no group")
	}
}

// What someone wrote for a group's config is shown as text, never as
// markup; JSON carries it as it is.
func TestConfigEscapes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	const evil = `<script>alert(1)</script>`
	must(t, f.st.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr",
		Config: map[string]any{"kind": "claude", "prompt": "read " + evil}}, "me"))
	page := f.get("/groups").body
	if strings.Contains(page, "<script>alert") {
		t.Error("/groups carries what someone wrote as markup")
	}
	if !strings.Contains(page, `read &lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Error("/groups does not show the config as text")
	}
	var got web.Groups
	f.getJSON("/groups", &got)
	if got.Groups[0].Config["prompt"] != "read "+evil {
		t.Errorf("JSON changed what was written: %+v", got.Groups[0].Config)
	}
}

// The hub is a sheet of the pages of Settings, with Peers and Telemetry for
// the operator, each by name with what it lists, and for the operator the
// delegation switch; each page stands under Settings with its own row of the
// list open and no link back to the hub, which is the list's first row. The
// hub is a snapshot with no live part of its own.
func TestConfigHub(t *testing.T) {
	t.Parallel()
	f, _ := talkFixture(t, "ada")
	must(t, f.st.SetSetting(context.Background(), store.OperatorSetting, "ada", "ada"))
	f.populate()
	hub := squash(f.get("/config").body)
	for _, want := range []string{
		`<article class="wv-page" data-kind="table">`,
		`<h1 class="wv-title">Config</h1><p class="wv-meta">The yard's configuration, one table a page</p>`,
		`<thead><tr><th>page</th><th>what it holds</th></tr></thead>`,
		`<tr><td class="wv-name"><a href="/groups">Groups</a></td><td>who works a stage</td></tr>`,
		`<tr><td class="wv-name"><a href="/routes">Routes</a></td><td>which group a stage sends its beads to, first match wins</td></tr>`,
		`<tr><td class="wv-name"><a href="/peers">Peers</a></td><td>the yards this one is linked with</td></tr>`,
		`<tr><td class="wv-name"><a href="/limits">Limits</a></td><td>what the subscriptions the yard burns still carry</td></tr>`,
		`<tr><td class="wv-name"><a href="/telemetry">Telemetry</a></td><td>the yard&#39;s opt-in telemetry switch</td></tr>`,
		`<a class="wv-row" href="/config" aria-current="page">`,
	} {
		if !strings.Contains(hub, want) {
			t.Errorf("/config lacks %s", want)
		}
	}
	// Depots and Deps are rows of Lines' list, so the hub links neither.
	stage := stageOf(t, f.get("/config").body)
	for _, gone := range []string{`href="/depots"`, `href="/deps"`, `hx-get="/config"`} {
		if strings.Contains(stage, gone) {
			t.Errorf("/config still has %s", gone)
		}
	}
	var got web.Config
	f.getJSON("/config", &got)
	var paths []string
	for _, l := range got.Links {
		paths = append(paths, l.Path)
	}
	if want := []string{"/groups", "/routes", "/peers", "/limits", "/telemetry"}; !slices.Equal(paths, want) || got.Head == 0 {
		t.Errorf("the hub as JSON links %v, want %v: %+v", paths, want, got)
	}
	// A page of Settings stands under it with its own row open (Depots and
	// Deps are Lines' rows), and has no link back to the hub: Config is the
	// first row of Settings' list.
	for path, tile := range map[string]string{
		"/config": "/config", "/depots": "/flows", "/groups": "/config", "/routes": "/config", "/peers": "/config",
		"/deps": "/flows", "/limits": "/config",
	} {
		body := f.get(path).body
		if want := `<a class="wv-row" href="` + path + `" aria-current="page">`; !strings.Contains(body, want) {
			t.Errorf("%s lacks %s", path, want)
		}
		if got := marked(railOf(t, body), "aria-current", "true"); len(got) != 1 || got[0] != tile {
			t.Errorf("%s opens the tiles %v, want %s", path, got, tile)
		}
		if strings.Contains(body, "‹ Config") {
			t.Errorf("%s links back to the hub", path)
		}
		// A page of the fourth stage kind draws its own head; the shell's
		// stands over a page that does not.
		if kind := strings.Contains(body, `data-kind="table"`) || strings.Contains(body, `data-kind="list"`); !kind || strings.Contains(body, `class="page-head"`) {
			t.Errorf("%s is no page of a stage kind, or has the shell's heading over its own", path)
		}
	}
	// Settings' list has a row for each page the reader may open: Peers and
	// Telemetry for the operator alone.
	if rows := listOf(t, f.get("/config").body); !strings.Contains(rows, `href="/peers"`) || !strings.Contains(rows, `href="/telemetry"`) {
		t.Errorf("the operator's list lacks Peers or Telemetry: %s", rows)
	}
}

// /beads has every field `yardr bead list --json` prints of a bead, and
// with ?all=1 the closed beads too, as bead list --all.
func TestBeadsAll(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.populate()
	ctx := context.Background()
	one := f.byTitle()["one"]
	_, err := f.st.UpdateBead(ctx, one.ID, store.BeadPatch{AddLabels: []string{"door"}, Priority: ptr(4)}, store.Expect{}, "me")
	must(t, err)

	for target, all := range map[string]bool{"/beads": false, "/beads?all=1": true, "/beads?all=0": false} {
		listed, err := f.st.ListBeads(ctx, store.BeadFilter{All: all})
		must(t, err)
		var got struct {
			All   bool             `json:"all"`
			Beads []map[string]any `json:"beads"`
		}
		f.getJSON(target, &got)
		if got.All != all || len(got.Beads) != len(listed) {
			t.Fatalf("%s: all %v, %d beads, want %v and %d", target, got.All, len(got.Beads), all, len(listed))
		}
		closed := 0
		for _, b := range listed {
			i := slices.IndexFunc(got.Beads, func(row map[string]any) bool { return row["id"] == b.ID })
			if i < 0 {
				t.Fatalf("%s lacks %s (%s)", target, b.ID, b.Title)
			}
			row := got.Beads[i]
			if b.Status == "closed" {
				closed++
			}
			// The card's own two fields beside the bead's.
			if row["working"] != false || (row["held"] == true) != b.Hold {
				t.Errorf("%s: %s working %v, held %v", target, b.Title, row["working"], row["held"])
			}
			delete(row, "working")
			delete(row, "held")
			if want := printed(t, b); !reflect.DeepEqual(any(row), want) {
				t.Errorf("%s: %s\n got %v\nwant %v", target, b.Title, row, want)
			}
		}
		if (closed == 1) != all {
			t.Errorf("%s: %d closed beads", target, closed)
		}
	}

	// What bead list prints and the cards did not have.
	var got web.Beads
	f.getJSON("/beads?all=1&stage=backlog", &got)
	i := slices.IndexFunc(got.Beads, func(r web.BeadRow) bool { return r.ID == one.ID })
	if i < 0 || !slices.Equal(got.Beads[i].Labels, []string{"door"}) || got.Beads[i].Priority != 4 || got.Beads[i].CreatedAt.IsZero() {
		t.Errorf("one: %+v", got.Beads)
	}
	if len(got.Beads) != 2 || got.Beads[1].Title != "two" || !got.Beads[1].Hold || !got.Beads[1].Held {
		t.Errorf("the held bead: %+v", got.Beads)
	}
	// The flow list's closed group uses a status filter: it keeps the closed
	// bead even though its actual stage is not named closed.
	f.getJSON("/beads?all=1&depot=line&stage=closed", &got)
	if len(got.Beads) != 1 || got.Beads[0].Title != "four" || got.Stage != "closed" {
		t.Errorf("closed group link lists %+v, want four closed at its own stage", got.Beads)
	}
	if got.StageCounts["closed"] != 1 || got.DepotCounts["line"] != 1 {
		t.Errorf("closed filter counts: stages %v, depots %v, want one closed in line", got.StageCounts, got.DepotCounts)
	}
	// The newest change first, the closed bead among them; a filter keeps all.
	f.getJSON("/beads?all=1&depot=line", &got)
	if want := []string{"one", "four", "two", "five", "three"}; !reflect.DeepEqual(titles(cards(got.Beads)), want) {
		t.Errorf("all of line: %v, want %v", titles(cards(got.Beads)), want)
	}
	text := squash(f.get("/beads?all=1&depot=line").body)
	for _, part := range []string{
		`hx-get="/beads?all=1&amp;depot=line"`,
		`<p class="wv-meta">5 beads, closed ones included, the newest change first</p>`,
		`<a class="wv-pill" href="/beads?all=1&amp;depot=line&amp;stage=new">new <span class="wv-num">2</span></a>`,
	} {
		if !strings.Contains(text, part) {
			t.Errorf("the page of all beads lacks %q", part)
		}
	}
	// The closed bead's row says so, after the words that place it.
	four := got.Beads[slices.IndexFunc(got.Beads, func(r web.BeadRow) bool { return r.Title == "four" })].ID
	if row := srowOf(text, four); !strings.Contains(row, `<span class="wv-sep">·</span>closed</span></span><span class="wv-srow__end">`) {
		t.Errorf("the closed bead's row does not say it is closed: %q", row)
	}
	if text := f.get("/beads").body; strings.Contains(text, "four") || strings.Contains(text, "all=1") {
		t.Error("the page of the open beads shows the closed one, or links to all")
	}
}

func ptr[T any](v T) *T { return &v }

// /sessions has the sessions as `yardr session list --json` prints them
// under list, with the wait for a usage limit; with ?all=1 the
// ended ones too, with how they ended.
func TestSessionsList(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	b, sess := blockedYard(t, f)
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	must(t, f.st.SetHandle(ctx, sess.ID, map[string]any{"agent": "door-new",
		"harness_reset_at": until.Format(time.RFC3339Nano), "harness_reset_window": "5h", "harness_reset_model": "opus"}))
	gone, err := f.st.CreateBead(ctx, store.NewBead{Title: "ended", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	ended, err := f.st.Claim(ctx, gone.ID, "builders", "new", "yard")
	must(t, err)
	must(t, f.st.EndSession(ctx, ended.ID, "died", "the pane closed"))

	for target, all := range map[string]bool{"/sessions": false, "/sessions?all=1": true} {
		ss, err := f.st.ListSessions(ctx, store.SessionFilter{All: all})
		must(t, err)
		list, _ := f.listOf(target, "list", "Accept", "application/json")
		if want := printed(t, dispatch.Listed(ss)); !reflect.DeepEqual(list, want) {
			t.Errorf("%s\n got %v\nwant %v", target, list, want)
		}
		var got web.Sessions
		f.getJSON(target, &got)
		// The page's own rows are the live ones either way.
		if len(got.Sessions) != 1 || got.Sessions[0].ID != sess.ID || got.Sessions[0].Runner != "fake door-new" {
			t.Errorf("%s: sessions %+v", target, got.Sessions)
		}
		if want := map[bool]int{false: 1, true: 2}[all]; len(got.List) != want {
			t.Fatalf("%s: %d listed, want %d", target, len(got.List), want)
		}
		for _, s := range got.List {
			switch s.ID {
			case sess.ID:
				if s.Bead != b.ID || s.Group != "builders" || s.State != "running" || s.Runner != "fake" || s.EndedAt != nil ||
					s.ResetWait == nil || !s.ResetWait.Until.Equal(until) || s.ResetWait.Model != "opus" {
					t.Errorf("%s: the running session: %+v (wait %+v)", target, s, s.ResetWait)
				}
			case ended.ID:
				if s.Bead != gone.ID || s.State != "died" || s.EndedAt == nil || s.StartedAt.IsZero() {
					t.Errorf("%s: the ended session: %+v", target, s)
				}
			default:
				t.Errorf("%s lists %+v", target, s)
			}
		}
	}
}

// /crew says what a crew member's session is doing, as crew list --json.
func TestCrewStatus(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Enabled: true, Config: map[string]any{"kind": "codex", "model": "o3"}}, "me")
	must(t, err)
	must(t, f.st.CrewStarted(ctx, "yardmaster", map[string]any{"agent": "yardmaster"}))
	must(t, f.st.CrewStatus(ctx, "yardmaster", "idle"))
	var got web.Crew
	f.getJSON("/crew", &got)
	if len(got.Crew) != 1 || got.Crew[0].Status != "idle" || got.Crew[0].Kind != "codex" || got.Crew[0].Model != "o3" ||
		got.Crew[0].Agent != "herdr · codex · o3" {
		t.Errorf("crew: %+v", got.Crew)
	}
	rows, err := f.st.Crew(ctx)
	must(t, err)
	if len(got.Crew) == 1 {
		pageFields := map[string]bool{"running": true, "since": true, "up": true, "agent": true, "provider": true, "unread": true, "restart": true}
		var want, actual map[string]any
		want = printed(t, rows[0]).(map[string]any)
		actual = printed(t, got.Crew[0]).(map[string]any)
		for key := range pageFields {
			delete(actual, key)
		}
		if !reflect.DeepEqual(actual, want) {
			t.Errorf("/crew command fields\n got %v\nwant %v", actual, want)
		}
	}
}

// /groups and /crew carry each row's resolved provider: the provider
// setting where set, even where no source answers for it, else the kind's
// mapping through the source that lists it; none where neither answers, for
// a group or crew member with no kind, or for another runner. Resolving
// runs no command.
func TestRowsCarryProvider(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	asked := limitsSource(t, f, limitsAnswer())
	for _, g := range []store.Group{
		{Name: "mapped", Runner: "herdr", Config: map[string]any{"kind": "kimi"}},
		{Name: "override", Runner: "herdr", Config: map[string]any{"kind": "kimi", "provider": "zai"}},
		{Name: "override-answered", Runner: "herdr", Config: map[string]any{"kind": "kimi", "provider": "claude"}},
		{Name: "unmapped", Runner: "herdr", Config: map[string]any{"kind": "codex"}},
		{Name: "nokind", Runner: "herdr"},
		{Name: "decisions", Runner: "manual", Members: []string{"jo"}},
		{Name: "built", Runner: "exec", Config: map[string]any{"cmd": "make"}},
	} {
		must(t, f.st.PutGroup(ctx, g, "me"))
	}
	for _, m := range []store.CrewMember{
		{Name: "mapped", Runner: "herdr", Config: map[string]any{"kind": "claude"}},
		{Name: "override", Runner: "herdr", Config: map[string]any{"kind": "kimi", "provider": "zai"}},
		{Name: "unmapped", Runner: "herdr", Config: map[string]any{"kind": "codex"}},
		{Name: "hand", Runner: "manual"},
	} {
		_, err := f.st.PutCrew(ctx, m, "me")
		must(t, err)
	}

	var groups web.Groups
	f.getJSON("/groups", &groups)
	got := map[string]string{}
	for _, g := range groups.Groups {
		got[g.Name] = g.Provider
	}
	want := map[string]string{
		"mapped": "moonshot", "override": "zai", "override-answered": "claude",
		"unmapped": "", "nokind": "", "decisions": "", "built": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("/groups providers\n got %v\nwant %v", got, want)
	}
	// The setting stays inside config too, as the command prints it.
	i := slices.IndexFunc(groups.Groups, func(g web.GroupRow) bool { return g.Name == "override" })
	if i < 0 || groups.Groups[i].Config["provider"] != "zai" || groups.Groups[i].Config["kind"] != "kimi" {
		t.Errorf("the override row lost the stored config: %+v", groups.Groups)
	}
	// Absent means absent: no provider key on the row.
	var raw struct {
		Groups []map[string]any `json:"groups"`
	}
	f.getJSON("/groups", &raw)
	for _, g := range raw.Groups {
		_, has := g["provider"]
		if want[g["name"].(string)] == "" && has {
			t.Errorf("%s carries an empty provider: %v", g["name"], g)
		}
		if want[g["name"].(string)] != "" && g["provider"] != want[g["name"].(string)] {
			t.Errorf("%s carries %v, want %s", g["name"], g["provider"], want[g["name"].(string)])
		}
	}

	var crew web.Crew
	f.getJSON("/crew", &crew)
	got = map[string]string{}
	for _, m := range crew.Crew {
		got[m.Name] = m.Provider
	}
	want = map[string]string{"mapped": "claude", "override": "zai", "unmapped": "", "hand": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("/crew providers\n got %v\nwant %v", got, want)
	}
	var rawCrew struct {
		Crew []map[string]any `json:"crew"`
	}
	f.getJSON("/crew", &rawCrew)
	for _, m := range rawCrew.Crew {
		provider, has := m["provider"]
		if want[m["name"].(string)] == "" && has {
			t.Errorf("crew member %s carries an empty provider: %v", m["name"], m)
		}
		if want[m["name"].(string)] != "" && provider != want[m["name"].(string)] {
			t.Errorf("crew member %s carries %v, want %s", m["name"], provider, want[m["name"].(string)])
		}
	}

	// Neither route ran the source's command.
	if got := askedFor(t, asked); got != "" {
		t.Errorf("asked = %q, want nothing run", got)
	}
}

// answerStore is a yard's sources as Answerers reads them, without the
// store's one-source-per-kind refusal, so two sources may list one kind.
type answerStore []store.Source

func (s answerStore) Sources(context.Context) ([]store.Source, error) { return s, nil }

// The kind's mapping comes through the first source by name that lists it,
// as Ask picks. One source answers for a kind, so the store holds no such
// yard: this is the rows' resolution alone, against two sources that both
// list pi.
func TestProviderFirstByName(t *testing.T) {
	t.Parallel()
	st := answerStore{
		{Name: "zeta", Text: "name: zeta\nlimits:\n  command: [\"/bin/true\", \"{provider}\"]\n  kinds: { pi: openai }\n"},
		{Name: "alpha", Text: "name: alpha\nlimits:\n  command: [\"/bin/true\", \"{provider}\"]\n  kinds: { pi: zai }\n"},
	}
	as, err := limits.Answerers(context.Background(), st, "")
	must(t, err)
	if len(as) != 3 || as[0].Source != "alpha" || as[1].Source != "machine" || !as[1].Builtin || as[2].Source != "zeta" {
		t.Fatalf("Answerers = %+v, want alpha first by name, the built-in between", as)
	}
	if got := web.ProviderOf("herdr", map[string]any{"kind": "pi"}, as); got != "zai" {
		t.Errorf("pi resolves to %q, want alpha's zai", got)
	}
}
