package web_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"html"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// testRow is a row of the list as the page says it.
type testRow struct {
	href string
	// dot is the dot's colour (wait, run, idle, fault), "" for none.
	dot, name, end string
	lines          []string
	current        bool
	unread         bool
}

// testSection is a section of the list: a fold with its title, or the bare
// rows of a mode's places, which have no title.
type testSection struct {
	title, count string
	open         bool
	current      bool
	rows         []testRow
}

var (
	rowRe   = regexp.MustCompile(`(?s)<a class="wv-row([^"]*)" href="([^"]*)"([^>]*)>(.*?)\n?</a>`)
	dotRe   = regexp.MustCompile(`<span class="wv-dot(?: wv-dot--([a-z]+))?">`)
	sentRe  = regexp.MustCompile(`<span class="wv-row__(name|meta|end)[^"]*">`)
	tagsRe  = regexp.MustCompile(`<[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// listRows are the rows of markup, each with its dot, its name, its quiet
// lines and its end read out of the tags.
func listRows(markup string) []testRow {
	var out []testRow
	for _, m := range rowRe.FindAllStringSubmatch(markup, -1) {
		row := testRow{href: html.UnescapeString(m[2]), unread: strings.Contains(m[1], "wv-row--unread"),
			current: strings.Contains(m[3], `aria-current="page"`)}
		inner := m[4]
		if d := dotRe.FindStringSubmatch(inner); d != nil {
			row.dot = d[1]
		}
		inner = sentRe.ReplaceAllStringFunc(inner, func(s string) string {
			return "\x00" + sentRe.FindStringSubmatch(s)[1][:1]
		})
		for _, part := range strings.Split(inner, "\x00")[1:] {
			text := strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagsRe.ReplaceAllString(part[1:], "")), " "))
			switch part[0] {
			case 'n':
				row.name = text
			case 'm':
				row.lines = append(row.lines, text)
			case 'e':
				row.end = text
			}
		}
		out = append(out, row)
	}
	return out
}

// listSections are the sections of the list, the bare rows first when it has
// them.
func listSections(t *testing.T, body string) []testSection {
	t.Helper()
	list := regionOf(t, body, `<div class="wv-list__rows" id="list-rows">`, `</nav>`)
	var out []testSection
	head, rest, _ := strings.Cut(list, `<details class="wv-sect"`)
	if strings.Contains(head, `<ul class="wv-places"`) {
		out = append(out, testSection{rows: listRows(head)})
	}
	for _, part := range strings.Split(rest, `<details class="wv-sect"`) {
		if !strings.Contains(part, "<summary>") {
			continue
		}
		sec := testSection{open: strings.HasPrefix(part, " open>"), rows: listRows(part)}
		summary, _, _ := strings.Cut(part, "</summary>")
		sec.current = strings.Contains(summary, `aria-current="page"`)
		if m := regexp.MustCompile(`</svg>(?:<a href="[^"]*"[^>]*>|<span>)([^<]*)<`).FindStringSubmatch(summary); m != nil {
			sec.title = html.UnescapeString(m[1])
		}
		if m := regexp.MustCompile(`<span class="wv-sect__count">([^<]*)</span>`).FindStringSubmatch(summary); m != nil {
			sec.count = m[1]
		}
		out = append(out, sec)
	}
	return out
}

// section is the section of the list named title.
func section2(t *testing.T, secs []testSection, title string) testSection {
	t.Helper()
	for _, s := range secs {
		if s.title == title {
			return s
		}
	}
	t.Fatalf("the list has no section %q: %+v", title, secs)
	return testSection{}
}

// Decide's list holds what waits for the person, what runs and the depots: a
// wait is a row of three lines, an ask by its question, a fault by its bead
// and its reason; the section's count is the head's and the badge's. /line
// adds what runs late.
func TestDecideList(t *testing.T) {
	t.Parallel()
	f, asked := windowYard(t)
	body := f.get("/").body
	if got := listTitle(t, body); got != "Decide" {
		t.Fatalf("the list is %s's", got)
	}
	head := regionOf(t, body, `<header class="wv-list__head" id="list-head">`, `</header>`)
	if !strings.Contains(head, `<span class="wv-list__count"><span class="wv-wait">2 wait for you</span></span>`) {
		t.Errorf("the head does not say what waits, in rust: %s", head)
	}
	secs := listSections(t, body)
	var titles []string
	for _, s := range secs {
		titles = append(titles, s.title)
	}
	if want := []string{"Waits for you", "Running", "Depots"}; strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Fatalf("Decide's sections are %v, want %v", titles, want)
	}

	waits := section2(t, secs, "Waits for you")
	if !waits.open || waits.count != "2" || len(waits.rows) != 2 {
		t.Fatalf("Waits for you: %+v, want 2 rows, open", waits)
	}
	// The held bead stands first (the line's order puts what stopped first),
	// by its title with the fault in red and the note after it; the asked
	// one by its question, the bead and its title, and who asked.
	byHref := map[string]testRow{}
	for _, r := range waits.rows {
		byHref[r.href] = r
	}
	ask := byHref["/beads/"+asked.ID]
	if ask.name != "Move the links now?" || ask.dot != "wait" || len(ask.lines) != 2 ||
		ask.lines[0] != asked.ID+" · three" || ask.lines[1] != "asked by yardmaster" {
		t.Errorf("the ask's row is %+v", ask)
	}
	if !strings.Contains(listOf(t, body), `<span class="wv-row__end wv-row__end--wait">`) {
		t.Errorf("an ask's age does not end its row in rust")
	}
	held := byHref["/beads/"+f.byTitle()["two"].ID]
	if held.name != "two" || held.dot != "wait" || len(held.lines) == 0 || !strings.HasPrefix(held.lines[0], "held") {
		t.Errorf("the held bead's row is %+v", held)
	}
	if !strings.Contains(listOf(t, body), `<span class="wv-fault">held`) {
		t.Errorf("a fault does not stand in red")
	}

	// What runs, with its stage and group, the dot green.
	running := section2(t, secs, "Running")
	if running.count != "1 working" || len(running.rows) != 1 || running.rows[0].name != "one" || running.rows[0].dot != "run" ||
		running.rows[0].lines[0] != f.byTitle()["one"].ID+" · backlog · builders" {
		t.Errorf("Running: %+v", running)
	}

	// The depots, each with its flows and their counts, and the depot's
	// open count at the end; a rust dot where an ask waits.
	depots := section2(t, secs, "Depots")
	if depots.count != "5 open" || len(depots.rows) != 2 {
		t.Fatalf("Depots: %+v", depots)
	}
	if r := depots.rows[0]; r.href != "/depots/line" || r.name != "line" || r.dot != "wait" || r.end != "4" ||
		len(r.lines) != 1 || r.lines[0] != "code 3 · bugs 1" {
		t.Errorf("the depot line's row is %+v", r)
	}
	if r := depots.rows[1]; r.href != "/depots/loose" || r.dot != "" || r.end != "1" || len(r.lines) != 0 {
		t.Errorf("the depot loose's row is %+v", r)
	}
	if !strings.Contains(listOf(t, body), `<ul hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list:outerHTML">`) {
		t.Errorf("the depot rows, which leave the mode, do not swap the whole list")
	}

	// Home leaves Late out and /line adds it after what runs.
	if late := listSections(t, f.get("/line").body); late[len(late)-2].title != "Late" || late[len(late)-1].title != "Depots" {
		t.Errorf("/line has no Late section before the depots: %+v", late)
	}
	for _, s := range secs {
		if s.title == "Late" {
			t.Errorf("home's list has a Late section")
		}
	}
}

// The beads parked where they start, and those the reader answered, stand in
// a section of their own, folded; the beads past their flow's first stage that
// wait on a manual group stand among the waits.
func TestDecideListParked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _ := windowYard(t)
	must(t, f.st.PutGroup(ctx, store.Group{Name: "deciders", Runner: "manual", Members: []string{"mckean"}, Limit: 5}, "me"))
	_, err := f.st.AddRoute(ctx, store.Route{Stage: "triage", Group: "deciders"}, "me")
	must(t, err)
	five := f.byTitle()["five"]
	secs := listSections(t, f.get("/").body)
	parked := section2(t, secs, "Parked")
	if parked.open || parked.count != "1" || len(parked.rows) != 1 || parked.rows[0].name != "five" ||
		parked.rows[0].dot != "" || parked.rows[0].lines[0] != five.ID+" · triage" ||
		!strings.Contains(parked.rows[0].lines[1], "waits for deciders") {
		t.Errorf("Parked: %+v, want the bead parked where it starts, folded", parked)
	}
	if waits := section2(t, secs, "Waits for you"); waits.count != "2" {
		t.Errorf("a bead parked where it starts counts among the waits: %+v", waits)
	}
	// The same bead on its own page opens the fold it stands in.
	secs = listSections(t, f.get("/beads/"+five.ID).body)
	if parked := section2(t, secs, "Parked"); !parked.open || !parked.rows[0].current {
		t.Errorf("Parked stays shut over the open row: %+v", parked)
	}
}

// Lines' list opens with the six places no depot or flow opens, then every
// depot as a section that opens its page, its flows as rows with their
// counts per stage, and a rust dot where an ask waits in the flow.
func TestLinesList(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	body := f.get("/flows").body
	head := regionOf(t, body, `<header class="wv-list__head" id="list-head">`, `</header>`)
	for _, want := range []string{
		`<span class="wv-list__count">2 depots, 5 open</span>`,
		`<p class="wv-list__sub">backlog 2 · new 1 · triage 1 · inbox 1</p>`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("Lines' head lacks %s: %s", want, head)
		}
	}
	secs := listSections(t, body)
	places := secs[0]
	var got []string
	for _, r := range places.rows {
		got = append(got, r.href+" "+r.name+" "+r.end)
	}
	if want := "/flows Flows 2|/depots Depots 2|/beads Beads 5|/sessions Sessions 1|/previews Previews |/deps Deps "; strings.Join(got, "|") != want {
		t.Errorf("the places are %q, want %q", strings.Join(got, "|"), want)
	}
	if places.rows[0].current != true || places.rows[3].dot != "run" {
		t.Errorf("Flows is not open, or Sessions carries no running dot: %+v", places.rows)
	}
	line := section2(t, secs, "line")
	if line.count != "4 open" || !line.open || len(line.rows) != 2 {
		t.Fatalf("the depot line: %+v", line)
	}
	if r := line.rows[0]; r.href != "/depots/line/code" || r.name != "code" || r.dot != "wait" || r.end != "3" || r.lines[0] != "backlog 2 · new 1" {
		t.Errorf("the flow code is %+v", r)
	}
	if r := line.rows[1]; r.href != "/depots/line/bugs" || r.name != "bugs" || r.dot != "" || r.end != "1" || r.lines[0] != "triage 1" {
		t.Errorf("the flow bugs is %+v", r)
	}
	// A depot with no flow has its section and no rows, and says its
	// beads are open.
	loose := section2(t, secs, "loose")
	if loose.count != "1 open" || len(loose.rows) != 0 {
		t.Errorf("the depot loose: %+v", loose)
	}
	// Its rows stay in the mode: the swap takes the rows, not the list.
	if strings.Contains(listOf(t, body), "#list:outerHTML") {
		t.Errorf("Lines' rows, which stay in Lines, swap the whole list")
	}
}

// The list marks the depot the page stands on, by its section's link, and a
// flow page the flow's row; no other page marks one.
func TestLinesListOpenRow(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "empty", Base: "origin/main"}, f.depotActor(ctx)))

	depot := listSections(t, f.get("/depots/line").body)
	if !section2(t, depot, "line").current || section2(t, depot, "loose").current {
		t.Errorf("the depot page does not mark its depot's section alone: %+v", depot)
	}
	for _, s := range depot {
		for _, r := range s.rows {
			if r.current && r.href != "/depots" {
				t.Errorf("the depot page marks the row %+v", r)
			}
		}
	}
	flow := listSections(t, f.get("/depots/line/code").body)
	if section2(t, flow, "line").current || !section2(t, flow, "line").rows[0].current || section2(t, flow, "line").rows[1].current {
		t.Errorf("the flow page does not mark its flow's row alone: %+v", flow)
	}
	// A depot of no flow and no bead is a depot all the same.
	empty := listSections(t, f.get("/depots/empty").body)
	if s := section2(t, empty, "empty"); !s.current || s.count != "none open" || len(s.rows) != 0 {
		t.Errorf("the quiet depot's section: %+v", s)
	}
}

// Crew's list holds the crew with the agent each runs and its state, and the
// people to talk with; mail unread stands at the end of the row in rust, and
// the row of the conversation the page opens is marked.
func TestCrewList(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	ctx := context.Background()
	_, err := f.st.PutCrew(ctx, store.CrewMember{Name: "night", Runner: "herdr", Config: map[string]any{"kind": "claude"}}, "mckean")
	must(t, err)
	must(t, f.st.SetCrewEnabled(ctx, "night", false, "", "mckean"))
	body := f.get("/talk?with=jo").body
	head := regionOf(t, body, `<header class="wv-list__head" id="list-head">`, `</header>`)
	if !strings.Contains(head, `<span class="wv-list__count">0 running</span>`) {
		t.Errorf("Crew's head: %s", head)
	}
	secs := listSections(t, body)
	crew := section2(t, secs, "Crew")
	if len(crew.rows) != 3 {
		t.Fatalf("Crew: %+v", crew)
	}
	// In the store's order, by name.
	night, ym := crew.rows[0], crew.rows[2]
	if ym.href != "/talk?with=yardmaster" || ym.name != "yardmaster" || ym.end != "unread mail" || !ym.unread || ym.current ||
		ym.lines[0] != "herdr" || !strings.HasPrefix(ym.lines[1], "stopped") {
		t.Errorf("the yardmaster's row is %+v", ym)
	}
	if night.name != "night" || night.lines[0] != "herdr · claude" || !strings.Contains(night.lines[1], "disabled") || night.end != "" || night.unread {
		t.Errorf("night's row is %+v", night)
	}
	people := section2(t, secs, "People")
	if people.count != "1" || len(people.rows) != 1 {
		t.Fatalf("People: %+v, want jo alone: mckean is the one the view acts as", people)
	}
	if jo := people.rows[0]; jo.href != "/talk?with=jo" || jo.name != "jo" || !jo.current || jo.end != "" {
		t.Errorf("jo's row is %+v", jo)
	}
	// A view that acts as no user has no people to show and no mail to mark.
	plain := newFixture(t)
	talkYard(t, plain)
	for _, s := range listSections(t, plain.get("/crew").body) {
		if s.title == "People" {
			t.Errorf("a view that acts as no one lists people: %+v", s)
		}
		for _, r := range s.rows {
			if r.end != "" || r.current {
				t.Errorf("a view that acts as no one marks a row: %+v", r)
			}
		}
	}
}

// unreadablePeers is a yard whose peers cannot be read.
type unreadablePeers struct{ store.API }

func (unreadablePeers) Peers(context.Context) ([]store.Peer, error) {
	return nil, errors.New("the peers cannot be read")
}

// Settings' list is the pages by name, each with its blurb and, where it has
// one, its count at the end: the operator's have Peers and Telemetry, the
// others' have neither; Depots and Deps are Lines'. /pairing, which stands in
// no row of the map, opens none. The counts are the store's, and no page of
// Settings asks a source for them.
func TestSettingsList(t *testing.T) {
	t.Parallel()
	f, _ := windowYard(t)
	ctx := context.Background()
	names := func(body string) string {
		var out []string
		for _, r := range listSections(t, body)[0].rows {
			out = append(out, r.name)
		}
		return strings.Join(out, ",")
	}
	// ends is each row's name and its end, as the list says them.
	ends := func(body string) string {
		var out []string
		for _, r := range listSections(t, body)[0].rows {
			out = append(out, r.name+"="+r.end)
		}
		return strings.Join(out, ",")
	}
	if got := names(f.get("/config").body); got != "Config,Groups,Routes,Peers,Limits,Telemetry" {
		t.Errorf("the operator's Settings are %s", got)
	}
	// A count of none is left out, Limits draws the built-in machine alone,
	// and the switch reads off.
	if got := ends(f.get("/config").body); got != "Config=,Groups=1,Routes=,Peers=,Limits=1,Telemetry=off" {
		t.Errorf("the counts of a yard with its one group are %s", got)
	}
	if rows := listSections(t, f.get("/groups").body)[0].rows; rows[1].current != true || rows[1].lines[0] != "who works a stage" {
		t.Errorf("Groups does not stand open with its blurb: %+v", rows[1])
	}
	member, _ := talkFixture(t, "jo")
	_, err := member.st.AddUser(ctx, "jo", "jo")
	must(t, err)
	if got := names(member.get("/config").body); got != "Config,Groups,Routes,Limits" {
		t.Errorf("a member's Settings are %s, want no Peers and no Telemetry", got)
	}
	// Behind the gateway Settings has Pair device, and /pairing, a page in
	// no row of the map, stands under Settings and opens none.
	y := newApproveYard(t, true)
	jo := y.token("jo@example.net")
	gateway := y.getNoFollow("/config", jo).body
	if got := names(gateway); got != "Config,Groups,Routes,Peers,Limits,Telemetry,Pair device" {
		t.Errorf("the gateway's Settings are %s", got)
	}
	if got := ends(gateway); !strings.HasSuffix(got, ",Pair device=") {
		t.Errorf("the gateway's counts are %s, want Pair device with none", got)
	}
	pairing := y.getNoFollow("/pairing", jo)
	if got := marked(railOf(t, pairing.body), "aria-current", "true"); pairing.status != 200 || len(got) != 1 || got[0] != "/config" {
		t.Errorf("/pairing: status %d, the rail opens %v, want Settings", pairing.status, got)
	}
	for _, r := range listSections(t, pairing.body)[0].rows {
		if r.current {
			t.Errorf("/pairing marks the row %+v", r)
		}
	}

	// stock gives a yard what the list counts, and a source whose stand-in
	// would run a command if the list asked it: it returns the file the
	// stand-in appends to when it runs. Limits draws an entry for each
	// provider the yard burns, one a source answers for or not, and for each
	// source that answers for itself: claude (the default), moonshot (the
	// group), openai (the crew member's own setting, which no source names)
	// and the built-in machine.
	stock := func(f *fixture) (asked string) {
		t.Helper()
		// Settings, crew, routes and peers are the operator's to change.
		op := f.depotActor(ctx)
		asked = limitsSource(t, f, limitsAnswer())
		must(t, f.st.SetSetting(ctx, store.DefaultKindSetting, "claude", op))
		putGroup(t, f, "writers", map[string]any{"kind": "kimi"})
		_, err := f.st.PutCrew(ctx, store.CrewMember{Name: "night", Runner: "herdr", Config: map[string]any{"kind": "pi", "provider": "openai"}}, op)
		must(t, err)
		for _, stage := range []string{"new", "review", "pick"} {
			_, err := f.st.AddRoute(ctx, store.Route{Stage: stage, Group: "writers"}, op)
			must(t, err)
		}
		_, key, err := ed25519.GenerateKey(nil)
		must(t, err)
		_, err = f.st.AddPeer(ctx, store.Peer{Name: "north", Key: store.Public(key), URL: "http://127.0.0.1:1"}, op)
		must(t, err)
		must(t, f.st.SetSetting(ctx, store.TelemetrySetting, "true", op))
		return asked
	}
	// The operator's yard holds builders already, so it has two groups; the
	// member's has the one.
	askedOperator, askedMember := stock(f), stock(member)
	operator := ends(f.get("/config").body)
	for _, page := range []string{"/groups", "/routes", "/peers"} {
		if got := ends(f.get(page).body); got != operator {
			t.Errorf("%s: the list's counts are %s, want those of /config, %s", page, got, operator)
		}
	}
	memberEnds := ends(member.get("/config").body)
	for who, asked := range map[string]string{"the operator's": askedOperator, "the member's": askedMember} {
		if got := askedFor(t, asked); got != "" {
			t.Errorf("Settings ran %s source for %q: its list asks no source", who, got)
		}
	}
	// Limits' count is what /limits draws, and /limits does run the source:
	// the empty files above are no stand-ins that cannot start.
	var drawn, memberDrawn web.Limits
	f.getJSON("/limits", &drawn)
	member.getJSON("/limits", &memberDrawn)
	n, memberN := len(drawn.Limits)+len(drawn.Sources), len(memberDrawn.Limits)+len(memberDrawn.Sources)
	if n != 4 || memberN != 4 {
		t.Fatalf("/limits draws %d entries, a member's %d: want claude, moonshot, openai and the machine, 4 each", n, memberN)
	}
	if want := fmt.Sprintf("Config=,Groups=2,Routes=3,Peers=1,Limits=%d,Telemetry=on", n); operator != want {
		t.Errorf("the operator's counts are %s, want %s", operator, want)
	}
	if want := fmt.Sprintf("Config=,Groups=1,Routes=3,Limits=%d", memberN); memberEnds != want {
		t.Errorf("a member's counts are %s, want %s: no Peers and no Telemetry", memberEnds, want)
	}
	for _, asked := range []string{askedOperator, askedMember} {
		if askedFor(t, asked) == "" {
			t.Error("/limits ran no source: the list's empty file shows nothing")
		}
	}

	// A count the list cannot read fails the page, as Crew's list does, and
	// leaves no row without its count. /config reads no peers itself: the
	// list's Peers row does.
	srv, err := web.NewServer(unreadablePeers{f.st}, f.home, nil)
	must(t, err)
	token := srv.ActAs("mckean")
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	down := &fixture{t: t, client: testClient(t), st: f.st, ts: ts, home: f.home}
	down.signIn(srv, token)
	if r := down.get("/config"); r.status != 500 {
		t.Errorf("/config of an operator whose peers cannot be read: status %d, want 500", r.status)
	}
}
