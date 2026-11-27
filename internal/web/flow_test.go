package web_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// flowYard is the populated yard: line with the code and bugs flows, loose
// with none.
func flowYard(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.populate()
	return f
}

// stockFlow gives the flow page something to list: seven landed fresh,
// eight closed from backlog, and six more beads in new past the cap of six.
func stockFlow(t *testing.T) (*fixture, map[string]store.Bead) {
	t.Helper()
	ctx := context.Background()
	f := flowYard(t)
	seven, err := f.st.CreateBead(ctx, store.NewBead{Title: "seven", Stage: "review", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.AdvanceNote(ctx, seven.ID, "merged", "assembly: merged 70dabcd", store.Expect{}, "assembly")
	must(t, err)
	eight, err := f.st.CreateBead(ctx, store.NewBead{Title: "eight", Stage: "backlog", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.CloseBead(ctx, eight.ID, "dropped", store.Expect{}, "me")
	must(t, err)
	for i := range 6 {
		_, err := f.st.CreateBead(ctx, store.NewBead{Title: fmt.Sprintf("bulk-%d", i), Stage: "new", Depot: "line"}, "me")
		must(t, err)
	}
	// byTitle maps the open beads alone; the closed ones join from the All
	// read, as the page reads them.
	bs := f.byTitle()
	closed, err := f.st.ListBeads(ctx, store.BeadFilter{All: true})
	must(t, err)
	for _, b := range closed {
		if b.Status == "closed" {
			bs[b.Title] = b
		}
	}
	return f, bs
}

// TestFlowList regroups flows across their landing terminals: the open
// stages and next labels, empty fold, last-day log and every other closed
// bead, including one closed from backlog and an older landing.
func TestFlowList(t *testing.T) {
	t.Parallel()
	t.Run("merged", testFlowListMerged)
	t.Run("done", testFlowListDone)
	t.Run("live", testFlowListLive)
	t.Run("no landing", testFlowListNoLanding)
}

func testFlowListMerged(t *testing.T) {
	t.Parallel()
	now := time.Now()
	line, loose := "line", "loose"
	d := &web.HomeDepot{Name: "line", Kind: store.DepotGit, Flows: []web.HomeFlow{
		{Name: "code", Src: "repo@52e22d5",
			Stages:   []web.StageCount{{Stage: "backlog"}, {Stage: "new"}, {Stage: "review"}},
			Terminal: []string{"merged"}},
	}}
	open := []store.Bead{
		{ID: "line-b1", Title: "first", Stage: "backlog", Status: "open", Depot: &line, UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "line-b2", Title: "second", Stage: "backlog", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
		{ID: "line-x1", Title: "elsewhere", Stage: "new", Status: "open", Depot: &loose, UpdatedAt: now.Add(-time.Hour)},
	}
	for i := range 7 {
		open = append(open, store.Bead{ID: fmt.Sprintf("line-n%d", i), Title: fmt.Sprintf("n%d", i),
			Stage: "new", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Duration(i+1) * time.Hour)})
	}
	closedAt := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	all := []store.Bead{
		{ID: "line-m1", Title: "landed", Stage: "merged", Status: "closed", Depot: &line, ClosedAt: closedAt(2 * time.Hour)},
		{ID: "line-m2", Title: "older merge", Stage: "merged", Status: "closed", Depot: &line, ClosedAt: closedAt(25 * time.Hour)},
		{ID: "line-d1", Title: "done elsewhere", Stage: "done", Status: "closed", Depot: &line, ClosedAt: closedAt(time.Hour)},
		{ID: "line-c1", Title: "dropped", Stage: "new", Status: "closed", Depot: &line, ClosedAt: closedAt(30 * time.Minute)},
		{ID: "line-b0", Title: "duplicate", Stage: "backlog", Status: "closed", Depot: &line, ClosedAt: closedAt(5 * time.Hour)},
		{ID: "line-l1", Title: "loose end", Stage: "merged", Status: "closed", Depot: &loose, ClosedAt: closedAt(time.Hour)},
		{ID: "line-n0", Title: "still open", Stage: "new", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
	}
	for i := range 6 {
		all = append(all, store.Bead{ID: fmt.Sprintf("line-lx%d", i), Title: fmt.Sprintf("landing %d", i),
			Stage: "merged", Status: "closed", Depot: &line, ClosedAt: closedAt(time.Duration(i+3) * time.Hour)})
	}
	for i := range 4 {
		all = append(all, store.Bead{ID: fmt.Sprintf("line-c%d", i+2), Title: fmt.Sprintf("closed %d", i+2),
			Stage: "review", Status: "closed", Depot: &line, ClosedAt: closedAt(time.Duration(i+6) * time.Hour)})
	}

	got := web.FlowList(d, 0, open, all, now)
	if got.Depot != "line" || got.Flow != "code" || got.Tag != "repo@52e22d5" {
		t.Fatalf("page %+v, want line/code at repo@52e22d5", got)
	}
	if len(got.Groups) != 2 || got.Groups[0].Stage != "backlog" || got.Groups[1].Stage != "new" {
		t.Fatalf("groups %+v, want backlog and new in flow order", got.Groups)
	}
	backlog, new := got.Groups[0], got.Groups[1]
	if backlog.Count != 2 || backlog.Next != "new" || backlog.Note != "waits on shaping" {
		t.Errorf("backlog %+v, want 2 beads pointing at new, waiting on shaping", backlog)
	}
	var ids []string
	for _, r := range backlog.Rows {
		ids = append(ids, r.Bead)
	}
	if !reflect.DeepEqual(ids, []string{"line-b2", "line-b1"}) {
		t.Errorf("backlog rows %v, want the newest first", ids)
	}
	if new.Count != 7 || new.Next != "review" || new.Note != "" {
		t.Errorf("new %+v, want 7 beads pointing at review, no note", new)
	}
	if len(new.Rows) != 6 || new.More != 1 || new.MoreLink != "/beads?depot=line&stage=new" {
		t.Errorf("new shows %d rows with %d more to %q, want 6, 1, the filtered bead list",
			len(new.Rows), new.More, new.MoreLink)
	}
	if len(got.Empty) != 1 || got.Empty[0].Stage != "review" || got.Empty[0].Next != "merged" {
		t.Fatalf("empty %+v, want review folded, pointing at merged", got.Empty)
	}
	if got.EmptyLabel() != "1 empty stage" || got.EmptyNames() != "review" {
		t.Errorf("fold %q with %q, want 1 empty stage with review", got.EmptyLabel(), got.EmptyNames())
	}
	if got.Landing == nil {
		t.Fatal("no landing log for a flow that ends in merged")
	}
	if got.Landing.Stage != "merged" || got.Landing.Count != 7 || len(got.Landing.Rows) != 6 ||
		got.Landing.More != 1 || got.Landing.MoreLink != "/beads?all=1&depot=line&stage=merged" {
		t.Fatalf("landing %+v, want merged with 6 of 7 and its all-beads link", got.Landing)
	}
	if r := got.Landing.Rows[0]; r.ID != "line-m1" || r.Verb != "landed" || r.Age != "2h" {
		t.Errorf("landing row %+v, want m1 landed 2h", r)
	}
	if got.Closed.Stage != "closed" || got.Closed.Count != 8 || len(got.Closed.Rows) != 6 ||
		got.Closed.More != 2 || got.Closed.MoreLink != "/beads?all=1&depot=line&stage=closed" {
		t.Fatalf("closed %+v, want 6 of 8 and its all-closed link", got.Closed)
	}
	var words []string
	for _, r := range got.Closed.Rows {
		words = append(words, r.ID+" "+r.Words())
	}
	want := []string{
		"line-c1 closed 30m",
		"line-d1 done 1h",
		"line-b0 closed 5h",
		"line-c2 closed 6h",
		"line-c3 closed 7h",
		"line-c4 closed 8h",
	}
	if !reflect.DeepEqual(words, want) {
		t.Errorf("closed rows %v, want %v", words, want)
	}
}

// The last open stage points at the flow's actual terminal, and the
// landing log stands under its name: done for a flow that ends in done,
// with an older done reading done under closed.
func testFlowListDone(t *testing.T) {
	t.Parallel()
	now := time.Now()
	line := "line"
	d := &web.HomeDepot{Name: "line", Kind: store.DepotDir, Flows: []web.HomeFlow{
		{Name: "docs", Src: "repo (directory)",
			Stages:   []web.StageCount{{Stage: "draft"}},
			Terminal: []string{"done"}},
	}}
	closedAt := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	open := []store.Bead{
		{ID: "line-w1", Title: "words", Stage: "draft", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
	}
	all := []store.Bead{
		{ID: "line-f1", Title: "filed", Stage: "done", Status: "closed", Depot: &line, ClosedAt: closedAt(2 * time.Hour)},
		{ID: "line-o1", Title: "older", Stage: "done", Status: "closed", Depot: &line, ClosedAt: closedAt(30 * time.Hour)},
	}

	got := web.FlowList(d, 0, open, all, now)
	if got.Tag != "" {
		t.Errorf("tag %q, want none for a dir depot's flow", got.Tag)
	}
	if len(got.Groups) != 1 || got.Groups[0].Stage != "draft" || got.Groups[0].Next != "done" {
		t.Fatalf("groups %+v, want draft pointing at done", got.Groups)
	}
	if len(got.Empty) != 0 {
		t.Errorf("empty %+v, want no folded stage", got.Empty)
	}
	if got.Landing == nil || got.Landing.Stage != "done" || got.Landing.Count != 1 {
		t.Fatalf("landing %+v, want done with one landing", got.Landing)
	}
	if r := got.Landing.Rows[0]; r.ID != "line-f1" || r.Words() != "landed 2h" {
		t.Errorf("landing row %+v, want f1 landed 2h", r)
	}
	if got.Closed.Count != 1 || len(got.Closed.Rows) != 1 {
		t.Fatalf("closed %+v, want the older done alone", got.Closed)
	}
	if r := got.Closed.Rows[0]; r.ID != "line-o1" || r.Words() != "done 1d 6h" {
		t.Errorf("closed row %+v, want o1 done 1d 6h", r)
	}
}

// A flow whose merged stage is still open lands in live: the open merged
// stage is an open group pointing at live, the log stands under live, and
// a bead closed at merged reads landed under closed.
func testFlowListLive(t *testing.T) {
	t.Parallel()
	now := time.Now()
	line := "line"
	d := &web.HomeDepot{Name: "line", Kind: store.DepotGit, Flows: []web.HomeFlow{
		{Name: "site",
			Stages:   []web.StageCount{{Stage: "backlog"}, {Stage: "merged"}},
			Terminal: []string{"live"}},
	}}
	closedAt := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	open := []store.Bead{
		{ID: "line-a1", Title: "asked", Stage: "backlog", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
		{ID: "line-m1", Title: "merging", Stage: "merged", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
	}
	all := []store.Bead{
		{ID: "line-l1", Title: "out", Stage: "live", Status: "closed", Depot: &line, ClosedAt: closedAt(time.Hour)},
		{ID: "line-x1", Title: "stuck", Stage: "merged", Status: "closed", Depot: &line, ClosedAt: closedAt(2 * time.Hour)},
	}

	got := web.FlowList(d, 0, open, all, now)
	if len(got.Groups) != 2 || got.Groups[0].Next != "merged" || got.Groups[1].Stage != "merged" || got.Groups[1].Next != "live" {
		t.Fatalf("groups %+v, want backlog to merged to live", got.Groups)
	}
	if got.Landing == nil || got.Landing.Stage != "live" || got.Landing.Count != 1 {
		t.Fatalf("landing %+v, want live with one landing", got.Landing)
	}
	if r := got.Landing.Rows[0]; r.ID != "line-l1" || r.Words() != "landed 1h" {
		t.Errorf("landing row %+v, want l1 landed 1h", r)
	}
	if got.Closed.Count != 1 || len(got.Closed.Rows) != 1 {
		t.Fatalf("closed %+v, want the bead closed at merged alone", got.Closed)
	}
	if r := got.Closed.Rows[0]; r.ID != "line-x1" || r.Words() != "landed 2h" {
		t.Errorf("closed row %+v, want x1 landed 2h", r)
	}
}

// A flow that ends in no landing terminal has no landing log: its last
// open stage points at its actual terminal, and what closed there reads
// closed under closed. A bead no flow governs stands under none of it.
func testFlowListNoLanding(t *testing.T) {
	t.Parallel()
	now := time.Now()
	line := "line"
	d := &web.HomeDepot{Name: "line", Kind: store.DepotGit, Flows: []web.HomeFlow{
		{Name: "bugs", Type: "bug",
			Stages:   []web.StageCount{{Stage: "triage"}},
			Terminal: []string{"fixed"}},
	}}
	closedAt := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	open := []store.Bead{
		{ID: "line-f1", Title: "found", Type: "bug", Stage: "triage", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
		{ID: "line-u1", Title: "untyped", Stage: "triage", Status: "open", Depot: &line, UpdatedAt: now.Add(-time.Hour)},
	}
	all := []store.Bead{
		{ID: "line-c1", Title: "caught", Type: "bug", Stage: "fixed", Status: "closed", Depot: &line, ClosedAt: closedAt(time.Hour)},
	}

	got := web.FlowList(d, 0, open, all, now)
	if len(got.Groups) != 1 || got.Groups[0].Stage != "triage" || got.Groups[0].Count != 1 || got.Groups[0].Next != "fixed" {
		t.Fatalf("groups %+v, want triage alone pointing at fixed", got.Groups)
	}
	if got.Landing != nil {
		t.Errorf("landing %+v, want none: fixed is no landing terminal", got.Landing)
	}
	if got.Closed.Count != 1 || len(got.Closed.Rows) != 1 {
		t.Fatalf("closed %+v, want the bead closed at fixed alone", got.Closed)
	}
	if r := got.Closed.Rows[0]; r.ID != "line-c1" || r.Words() != "closed 1h" {
		t.Errorf("closed row %+v, want c1 closed 1h", r)
	}
}

// The flow's list as JSON: the groups in flow order with their next
// stages, the cap with its link, the folded stage, the landing and the
// closed beads.
func TestFlowJSON(t *testing.T) {
	t.Parallel()
	f, bs := stockFlow(t)

	var got web.FlowPage
	f.getJSON("/depots/line/code", &got)
	if got.Depot != "line" || got.Flow != "code" || got.Tag != "" {
		t.Fatalf("page %+v, want line/code with no tag", got)
	}
	if len(got.Groups) != 2 || got.Groups[0].Stage != "backlog" || got.Groups[1].Stage != "new" {
		t.Fatalf("groups %+v, want backlog and new in flow order", got.Groups)
	}
	backlog, new := got.Groups[0], got.Groups[1]
	if backlog.Count != 2 || backlog.Next != "new" || backlog.Note != "waits on shaping" || len(backlog.Rows) != 2 {
		t.Errorf("backlog %+v, want one's and two's rows pointing at new", backlog)
	}
	for _, r := range backlog.Rows {
		if r.Bead != bs["one"].ID && r.Bead != bs["two"].ID {
			t.Errorf("backlog row %+v, want one's or two's bead", r)
		}
	}
	if new.Count != 7 || new.Next != "review" || len(new.Rows) != 6 || new.More != 1 {
		t.Errorf("new %+v, want 7 beads with 6 rows and 1 more", new)
	}
	if new.MoreLink != "/beads?depot=line&stage=new" {
		t.Errorf("new more link %q, want the filtered bead list", new.MoreLink)
	}
	if len(got.Empty) != 1 || got.Empty[0].Stage != "review" || got.Empty[0].Next != "merged" {
		t.Errorf("empty %+v, want review folded, pointing at merged", got.Empty)
	}
	if got.Landing == nil || got.Landing.Stage != "merged" || got.Landing.Count != 1 {
		t.Fatalf("landing %+v, want merged with seven alone", got.Landing)
	}
	if r := got.Landing.Rows[0]; r.ID != bs["seven"].ID || r.Title != "seven" || r.Verb != "landed" || r.Age == "" {
		t.Errorf("landing row %+v, want seven landed with its age", r)
	}
	if got.Closed.Count != 2 || len(got.Closed.Rows) != 2 {
		t.Fatalf("closed %+v, want eight and four", got.Closed)
	}
	if first, second := got.Closed.Rows[0], got.Closed.Rows[1]; first.ID != bs["eight"].ID || second.ID != bs["four"].ID {
		t.Errorf("closed rows %q and %q, want eight then four, the newest first", first.ID, second.ID)
	}
	for _, r := range got.Closed.Rows {
		if r.Verb != "closed" || r.Age == "" {
			t.Errorf("closed row %+v, want closed with its age", r)
		}
	}
}

// chevron and checkMark are the glyphs of the stage's back link and folds, and
// of a row that has closed.
const (
	chevron   = `<svg class="wv-glyph wv-chev" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6"/></svg>`
	checkMark = `<svg class="wv-glyph wv-check" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M8 12.5l2.5 2.5L16 9.5"/></svg>`
)

// The same request without the header is the page, the fifth stage kind: the
// back link to the depot, the groups with their rows to their beads, six rows
// to a group and Show N more behind them, the empty stages in one fold, the
// plain terminal rows. No Board/Map toggle, no filter bar, no New bead.
func TestFlowHTML(t *testing.T) {
	t.Parallel()
	f, bs := stockFlow(t)

	r := f.get("/depots/line/code")
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q", r.status, r.header.Get("Content-Type"))
	}
	s := squash(r.body)
	for _, want := range []string{
		"<title>code · depot</title>",
		`<article class="wv-page" data-kind="flow">`,
		`<a class="wv-back" href="/depots/line">` + chevron + `line</a><h1 class="wv-title">code</h1>` +
			`<p class="wv-meta">flow of line<span class="wv-sep">·</span>9 open</p>`,
		`<h2 class="wv-group__head"><span>backlog</span><span class="wv-group__count">2</span><span class="wv-group__note">waits on shaping</span></h2>`,
		`<a class="wv-srow" href="/beads/` + bs["bulk-0"].ID + `"><span class="wv-dot wv-dot--idle"></span>` +
			`<span class="wv-srow__name">bulk-0</span><span class="wv-srow__end"><span>→ review</span></span></a>`,
		`<a class="wv-more" href="/beads?depot=line&amp;stage=new">Show 1 more</a>`,
		`<span class="wv-fold__label">1 empty stage</span><span class="wv-fold__value">review</span>`,
		`<h2 class="wv-group__head"><span>review</span><span class="wv-group__count">0</span><span class="wv-group__note">→ merged</span></h2>`,
		`<h2 class="wv-group__head"><span>merged</span><span class="wv-group__count">1</span><span class="wv-group__note">last 24h · terminal</span></h2>`,
		`<li class="wv-srow wv-srow--quiet">` + checkMark + `<span><span class="wv-srow__name">seven</span><br>` +
			`<span class="wv-srow__meta"><span class="wv-id">` + bs["seven"].ID + `</span></span></span>` +
			`<span class="wv-srow__end"><span>landed `,
		`<h2 class="wv-group__head"><span>closed</span><span class="wv-group__count">2</span><span class="wv-group__note">terminal</span></h2>`,
		`<span class="wv-srow__name">eight</span><br><span class="wv-srow__meta"><span class="wv-id">` + bs["eight"].ID + `</span></span></span>` +
			`<span class="wv-srow__end"><span>closed `,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	// A group names six rows and the rest stand behind Show N more: new holds
	// seven, so the seventh, three, is not among its rows.
	newGroup := s[strings.Index(s, `<span>new</span><span class="wv-group__count">7`):]
	newGroup = newGroup[:strings.Index(newGroup, `</section>`)]
	if n := strings.Count(newGroup, `<a class="wv-srow" href="/beads/`); n != 6 {
		t.Errorf("new names %d rows, want 6: %s", n, newGroup)
	}
	if strings.Contains(s, "/beads/"+bs["three"].ID) {
		t.Errorf("three's row stands past the cap")
	}
	// The empty stages stand in one fold, whatever their number; the block of
	// rows that open a bead's page swaps the whole list, as no other block does.
	if n := strings.Count(s, `<details class="wv-fold">`); n != 1 {
		t.Errorf("the page has %d folds, want the empty stages' one", n)
	}
	if n := strings.Count(s, `<ol hx-select-oob="#rail:outerHTML,#drawer:outerHTML,#list:outerHTML">`); n != 2 {
		t.Errorf("the page has %d blocks of bead rows that swap the whole list, want one for each group of open beads", n)
	}
	// The terminal rows are plain: no link carries seven or eight to its bead.
	if strings.Contains(s, "/beads/"+bs["seven"].ID) || strings.Contains(s, "/beads/"+bs["eight"].ID) {
		t.Errorf("a terminal row links its bead")
	}
	for _, not := range []string{"New bead", ">Board<", ">Map<", "Filter", `class="filters"`, "flow-head"} {
		if strings.Contains(s, not) {
			t.Errorf("the page draws %q: nothing dead is drawn", not)
		}
	}
	// A group of the flow that holds nothing says so in a line of its own: the
	// bugs flow has no landing terminal and has closed nothing.
	if bugs := squash(f.get("/depots/line/bugs").body); !strings.Contains(bugs, `<p class="wv-none">Nothing closed.</p>`) ||
		strings.Contains(bugs, "Nothing landed") {
		t.Errorf("the bugs flow's closed group is not drawn empty, or it draws a landing log: %s", bugs)
	}
}

// A depot or flow nobody knows is a 404, as JSON and as HTML.
func TestFlowNotFound(t *testing.T) {
	t.Parallel()
	f := flowYard(t)
	for _, tc := range []struct{ target, want string }{
		{"/depots/nosuch/code", "depot nosuch"},
		{"/depots/line/nosuch", "flow nosuch"},
	} {
		r := f.get(tc.target)
		if r.status != http.StatusNotFound || !strings.Contains(r.body, tc.want) {
			t.Errorf("HTML %s: status %d, want 404 naming %q: %s", tc.target, r.status, tc.want, r.body)
		}
		r = f.get(tc.target, "Accept", "application/json")
		if r.status != http.StatusNotFound || !strings.Contains(r.body, tc.want) {
			t.Errorf("JSON %s: status %d, want 404 naming %q: %s", tc.target, r.status, tc.want, r.body)
		}
	}
}
