package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/web"
)

// feedBlock is the feed's block of a page, squashed: its head, with All
// activity where the page has it, and the entries under it.
func feedBlock(t *testing.T, text string) string {
	t.Helper()
	_, s, _ := strings.Cut(squash(stageOf(t, text)), `<section class="wv-block" aria-label="What happened">`)
	s, _, _ = strings.Cut(s, `</section>`)
	if s == "" {
		t.Fatal("the page has no feed")
	}
	return s
}

// feedSection is the feed part of a home page: its entries and the heads of
// their hours, without the overview above them.
func feedSection(t *testing.T, text string) string {
	t.Helper()
	_, s, _ := strings.Cut(feedBlock(t, text), `<ol class="wv-feed" id="feed"`)
	s, _, _ = strings.Cut(s, `</ol>`)
	if s == "" {
		t.Fatal("the page has no feed")
	}
	return s
}

// feedEntry is one entry of the feed as the page says it: its tone, what
// happened, the bead it is of, with the bead's title as the link says it, and
// the note under it.
type feedEntry struct{ seq, tone, what, bead, title, note string }

var (
	entryToneRE  = regexp.MustCompile(`^(?: wv-entry--([a-z]+))?" id="e(\d+)">`)
	entryWhatRE  = regexp.MustCompile(`<span class="wv-entry__what">(.*?)</span>`)
	entryTitleRE = regexp.MustCompile(`<a class="wv-entry__title" href="/beads/([^"]*)">(.*?)</a>`)
	entryNoteRE  = regexp.MustCompile(`<p class="wv-entry__note">(.*?)</p>`)
)

// feedEntries are the entries of the feed of a page, newest first.
func feedEntries(t *testing.T, text string) []feedEntry {
	t.Helper()
	var out []feedEntry
	for _, chunk := range strings.Split(feedSection(t, text), `<li class="wv-entry`)[1:] {
		chunk, _, _ = strings.Cut(chunk, `</li>`)
		var e feedEntry
		if m := entryToneRE.FindStringSubmatch(chunk); m != nil {
			e.tone, e.seq = m[1], m[2]
		} else {
			t.Fatalf("an entry that is none: %s", chunk)
		}
		if m := entryWhatRE.FindStringSubmatch(chunk); m != nil {
			e.what = html.UnescapeString(m[1])
		}
		if m := entryTitleRE.FindStringSubmatch(chunk); m != nil {
			e.bead, e.title = m[1], html.UnescapeString(m[2])
		}
		if m := entryNoteRE.FindStringSubmatch(chunk); m != nil {
			e.note = html.UnescapeString(m[1])
		}
		out = append(out, e)
	}
	return out
}

// said is what the entries say of what happened, each as its tone and its
// words: "held|held", "|noted".
func said(entries []feedEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.tone+"|"+e.what)
	}
	return out
}

// ownedFixture serves a yard the test can log any event kind on, as
// TestFeedOtherWords does.
func ownedFixture(t *testing.T, as string) (*fixture, *store.Store) {
	t.Helper()
	api, own := storetest.NewOwned(t)
	srv, err := web.NewServer(api, filepath.Join(t.TempDir(), ".depot"), nil)
	must(t, err)
	var token string
	if as != "" {
		token = srv.ActAs(as)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { shut(ts) })
	f := &fixture{t: t, client: testClient(t), st: api, ts: ts}
	if as != "" {
		f.signIn(srv, token)
	}
	return f, own
}

// Home's feed keeps what a person wants on a phone, each one line, with
// "All activity" carrying the rest to the full feed. A landing stands
// once: its move to the terminal stage is hidden beside it.
func TestHomeFeedKeeps(t *testing.T) {
	t.Parallel()
	// The mail below is to me, and mail goes to its parties alone.
	f, own := ownedFixture(t, "me")
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	fl, err := store.ParseFlow("new -> review on done\nreview -> merged on done\nmerged terminal\n")
	must(t, err)
	fl.Name = "code"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "line", "", "code", "me"))
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "builder")
	must(t, err)
	_, err = f.st.SetHold(ctx, b.ID, false, store.Expect{}, "me")
	must(t, err)
	must(t, f.st.LogDispatch(ctx, b.ID, "session_blocked", "yard", map[string]any{"session": "s1"}))
	must(t, f.st.LogDispatch(ctx, b.ID, "session_unblocked", "yard", map[string]any{"session": "s1"}))
	must(t, f.st.LogDispatch(ctx, b.ID, "gave_up", "yard", map[string]any{"stage": "new", "failures": 3}))
	must(t, own.LogEvent(ctx, b.ID, "crew_gave_up", "yard", map[string]any{"crew": "brakeman"}))
	must(t, f.st.LogDispatch(ctx, b.ID, "session_harness_error", "yard",
		map[string]any{"session": "s1", "reason": "at_capacity", "line": "the model is at capacity"}))
	must(t, own.LogEvent(ctx, b.ID, "session_failed", "yard", map[string]any{"session": "s1", "reason": "the pane died"}))
	// The yard breathing around them: claims, starts, turns, prompts,
	// plumbing and bookkeeping, none of it for the first screen.
	must(t, own.LogEvent(ctx, b.ID, "started", "yard", map[string]any{"session": "s1"}))
	must(t, own.LogEvent(ctx, b.ID, "claimed", "yard", map[string]any{"group": "crew", "stage": "new"}))
	title := "a door, hung"
	_, err = f.st.UpdateBead(ctx, b.ID, store.BeadPatch{Title: &title}, store.Expect{}, "me")
	must(t, err)
	other, err := f.st.CreateBead(ctx, store.NewBead{Title: "a window", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	must(t, f.st.AddDep(ctx, other.ID, b.ID, "blocks", "", "", "me"))
	for _, kind := range []string{"subscribed", "server_replaced", "crew_started", "crew_stopped", "crew_rotation_asked"} {
		must(t, own.LogEvent(ctx, b.ID, kind, "yard", map[string]any{"crew": "brakeman"}))
	}
	must(t, own.LogEvent(ctx, b.ID, "crew_status", "yard",
		map[string]any{"crew": "brakeman", "status": "idle", "was": "blocked"}))
	must(t, own.LogEvent(ctx, b.ID, "mail", "yard", map[string]any{"to": "me", "text": "hello"}))
	for _, kind := range []string{"workspace_removed", "daemon_replaced", "session_prompted", "session_idle",
		"session_stalled", "move_refused"} {
		must(t, f.st.LogDispatch(ctx, b.ID, kind, "yard", map[string]any{"session": "s1"}))
	}
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "builder")
	must(t, err)
	_, err = f.st.AdvanceNote(ctx, b.ID, "merged", "assembly: merged 70dabcd", store.Expect{}, "assembly")
	must(t, err)

	entries := feedEntries(t, f.get("/").body)
	got := said(entries)
	for _, want := range []string{
		"held|held",
		"|no longer held",
		"|session blocked",
		"|session unblocked",
		"|gave up",
		"|crew gave up",
		"held|harness stopped on an error: at capacity",
		"held|session ended: failed",
		"|noted",
		"landed|landed in merged at 70dabcd",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("the feed lacks %q: %q", want, got)
		}
	}
	if block := feedBlock(t, f.get("/").body); !strings.Contains(block, `<a class="wv-sh__count" href="/?all=1">All activity</a>`) {
		t.Errorf("the feed's head lacks All activity: %s", block)
	}
	for _, not := range []string{
		"created",
		"claimed by",
		"session started",
		"updated",
		"dep added",
		"subscribed",
		"workspace removed",
		"daemon replaced",
		"server replaced",
		"crew started",
		"crew stopped",
		"crew rotation asked",
		"crew status",
		"session prompted",
		"session idle",
		"session stalled",
		"move refused",
		"mail",
		"moved from new to review (done)",
		// The landing's move, hidden beside it.
		"moved from review to merged",
	} {
		for _, e := range entries {
			if e.what == not || (len(not) > 6 && strings.Contains(e.what, not)) {
				t.Errorf("the feed has %q", not)
			}
		}
	}
	if strings.Contains(feedSection(t, f.get("/").body), ">Older<") {
		t.Error("the feed names an older page, and All activity is there instead")
	}
	if n := len(entries); n != 10 {
		t.Errorf("the feed shows %d entries, want the 10 kept", n)
	}
	// The stream the page hangs its feed on carries the kept entries.
	if body := f.get("/").body; !strings.Contains(squash(body), `sse-connect="/events?after=`) ||
		!strings.Contains(squash(body), `&amp;format=home`) {
		t.Error("the page's stream is not the home one")
	}

	// The JSON keeps every event, kept or not.
	h := homeJSON(t, f, "/")
	kinds := map[string]bool{}
	for _, e := range h.Events {
		kinds[e.Kind] = true
	}
	for _, k := range []string{"created", "started", "claimed", "updated", "dep_added", "subscribed",
		"workspace_removed", "daemon_replaced", "server_replaced", "crew_started", "crew_stopped",
		"crew_rotation_asked", "crew_status", "session_prompted", "session_idle", "session_stalled",
		"move_refused", "mail", "advanced", "closed"} {
		if !kinds[k] {
			t.Errorf("the JSON lacks a %s event: nothing left it out of the page", k)
		}
	}
	if n := len(h.Events); n <= 10 {
		t.Errorf("the JSON holds %d events, want the full page behind the 10 shown", n)
	}
}

// A move stands on home when it goes into or out of a manual stage, or
// back on failed or changes; every other move is agents doing their jobs.
// A close by hand, a release, a reopen and a firing timer stand.
func TestHomeFeedMoves(t *testing.T) {
	t.Parallel()
	f, own := ownedFixture(t, "")
	ctx := context.Background()
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "line", Base: "origin/main"}, "me"))
	fl, err := store.ParseFlow("new -> review on done | decide\ndecide -> new\n" +
		"review -> approved on done | new on changes\napproved -> new on failed | merged on done\nmerged terminal\n")
	must(t, err)
	fl.Name = "code"
	_, err = f.st.PutFlow(ctx, *fl, "me")
	must(t, err)
	must(t, f.st.SetFlowPointer(ctx, "line", "", "code", "me"))
	must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual"}, "me"))
	_, err = f.st.AddRoute(ctx, store.Route{Stage: "decide", Group: "decisions"}, "me")
	must(t, err)
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "a door", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	_, err = f.st.AdvanceNote(ctx, b.ID, "decide", "", store.Expect{}, "builder")
	must(t, err)
	_, err = f.st.AdvanceNote(ctx, b.ID, "new", "", store.Expect{}, "me")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "builder")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "changes", "", store.Expect{}, "reviewer")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "builder")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "reviewer")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "failed", "", store.Expect{}, "assembly")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "builder")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "reviewer")
	must(t, err)
	_, err = f.st.Done(ctx, b.ID, "done", "", store.Expect{}, "assembly")
	must(t, err)
	d, err := f.st.CreateBead(ctx, store.NewBead{Title: "a window", Stage: "new", Depot: "line"}, "me")
	must(t, err)
	// A release changes nothing on an unclaimed bead and logs nothing, so
	// the event is written as the store would write it.
	must(t, own.LogEvent(ctx, d.ID, "released", "me", map[string]any{"reason": "try again"}))
	_, err = f.st.CloseBead(ctx, d.ID, "dropped", store.Expect{}, "me")
	must(t, err)
	_, err = f.st.Reopen(ctx, d.ID, store.Expect{}, "me")
	must(t, err)
	must(t, own.LogEvent(ctx, b.ID, "timer", "yard",
		map[string]any{"by": "yard", "to": "me", "text": "timer: stand up"}))

	entries := feedEntries(t, f.get("/").body)
	got := said(entries)
	for _, want := range []string{
		"|moved from new to decide",
		"|moved from decide to new",
		"|moved from review to new (changes)",
		"|moved from approved to new (failed)",
		"landed|landed in merged",
		"|released, to be routed again",
		"|closed: dropped",
		"|reopened",
		"|timer of yard for me",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("the feed lacks %q: %q", want, got)
		}
	}
	for _, not := range []string{
		"moved from new to review (done)",
		"moved from review to approved (done)",
		"moved from approved to merged",
		"created",
	} {
		for _, e := range entries {
			if e.what == not {
				t.Errorf("the feed has %q", not)
			}
		}
	}
	if n := len(entries); n != 9 {
		t.Errorf("the feed shows %d entries, want the 9 kept", n)
	}
}

// Past ten kept events the feed names ten and "All activity" carries the
// rest to the full feed; with nothing kept and nothing at all the feed
// says so.
func TestHomeFeedTen(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	b := bead(t, f.st, "a door")
	for i := range 12 {
		must(t, f.st.AddNote(ctx, b.ID, fmt.Sprint("remark ", i), "me"))
	}
	s := feedSection(t, f.get("/").body)
	if n := len(feedEntries(t, f.get("/").body)); n != 10 {
		t.Errorf("the feed shows %d entries, want 10", n)
	}
	if !strings.Contains(feedBlock(t, f.get("/").body), `href="/?all=1">All activity</a>`) {
		t.Error("the feed lacks All activity behind its 10")
	}
	// The 10 shown are the 10 newest, each row carrying its seq as id.
	h := homeJSON(t, f, "/")
	var want []string
	for _, e := range h.Events[:10] {
		want = append(want, fmt.Sprintf(`id="e%d"`, e.Seq))
	}
	for _, id := range want {
		if !strings.Contains(s, id) {
			t.Errorf("the feed lacks the row %s of the 10 newest", id)
		}
	}
	for _, e := range h.Events[10:] {
		if strings.Contains(s, fmt.Sprintf(`id="e%d"`, e.Seq)) {
			t.Errorf("the feed shows e%d past its 10", e.Seq)
		}
	}
	if n := len(feedEntries(t, f.get("/?all=1").body)); n != 13 {
		t.Errorf("the full feed shows %d entries, want the 12 notes and the creation", n)
	}
	if strings.Contains(feedBlock(t, f.get("/?all=1").body), "All activity") {
		t.Error("the full feed names All activity, and it is there")
	}

	empty := newFixture(t)
	s = feedSection(t, empty.get("/").body)
	if !strings.Contains(s, "Nothing has happened yet.") || strings.Contains(feedBlock(t, empty.get("/").body), "All activity") {
		t.Errorf("the empty feed says %q", s)
	}
}

// ?all=1 changes the page, never the JSON: / and /?all=1 carry the same
// full events (TestFeedHTML tries the pages).
func TestHomeFeedAllJSON(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	landing(t, f.st)
	ctx := context.Background()
	for i := range 60 {
		_, err := f.st.CreateBead(ctx, store.NewBead{Title: fmt.Sprint("bead ", i), Stage: "new"}, "me")
		must(t, err)
	}
	plain, full := homeJSON(t, f, "/"), homeJSON(t, f, "/?all=1")
	if fmt.Sprint(plain.Events) != fmt.Sprint(full.Events) {
		t.Error("the JSON events differ between / and /?all=1")
	}
	if len(plain.Events) != 50 {
		t.Errorf("the JSON holds %d events, want the full 50", len(plain.Events))
	}
}

// The home stream puts only a kept event's fragment on top and passes the
// rest as their ids alone, so the client's cursor still passes them. A
// kept fragment is a message: the column still re-reads on it.
func TestHomeStream(t *testing.T) {
	t.Parallel()
	f, own := ownedFixture(t, "")
	ctx := context.Background()
	b := bead(t, f.st, "a door")
	head, err := f.st.Head(ctx)
	must(t, err)
	s := f.stream(t, fmt.Sprintf("/events?after=%d&format=home", head))
	must(t, f.st.AddNote(ctx, b.ID, "two\nlines", "me"))
	c := bead(t, f.st, "a window")
	_, err = f.st.SetHold(ctx, c.ID, true, store.Expect{}, "me")
	must(t, err)
	must(t, own.LogEvent(ctx, c.ID, "advanced", "builder",
		map[string]any{"from": "new", "to": "review"}))
	must(t, own.LogEvent(ctx, c.ID, "advanced", "assembly",
		map[string]any{"from": "review", "to": "merged"}))
	must(t, own.LogEvent(ctx, c.ID, "closed", "assembly", map[string]any{"reason": "merged"}))

	// A fragment is the head of its hour and the entry under it: the stream
	// knows nothing of what the page shows, and the page drops a head that
	// repeats the one above it.
	hour := `<li class="wv-hour" data-hour="`
	if fr := s.next(t); !strings.HasPrefix(fr.data, hour) || !strings.Contains(fr.data, `<span class="wv-entry__what">noted</span>`) {
		t.Errorf("the note came as %q, want its hour's head and its fragment", fr.data)
	}
	if fr := s.next(t); fr.data != "" {
		t.Errorf("the creation came as %q, want its id alone", fr.data)
	}
	if fr := s.next(t); !strings.HasPrefix(fr.data, hour) || !strings.Contains(fr.data, `<li class="wv-entry wv-entry--held"`) ||
		!strings.Contains(fr.data, `<span class="wv-entry__what">held</span>`) {
		t.Errorf("the hold came as %q, want its fragment", fr.data)
	}
	if fr := s.next(t); fr.data != "" {
		t.Errorf("the routine move came as %q, want its id alone", fr.data)
	}
	if fr := s.next(t); fr.data != "" {
		t.Errorf("the landing's move came as %q, want its id alone", fr.data)
	}
	if fr := s.next(t); !strings.Contains(fr.data, `<li class="wv-entry wv-entry--landed"`) ||
		!strings.Contains(fr.data, `<span class="wv-entry__what">landed in merged</span>`) {
		t.Errorf("the landing came as %q, want its fragment", fr.data)
	}
	s.quiet(t)
}

// Home's feed leaves out a note by the bead's own agent or the landing and
// keeps one by a user or a crew member. ?all=1, the JSON and the bead's own
// page keep every note, as today.
func TestHomeFeedNotes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	must(t, f.st.PutGroup(ctx, store.Group{Name: "yardr-refinery", Runner: "exec", Config: map[string]any{"cmd": "true"}}, "me"))
	_, err := f.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr"}, "me")
	must(t, err)
	agent := bead(t, f.st, "the agents bead")
	must(t, f.st.AddNote(ctx, agent.ID, "an agent line", agent.ID+"-new"))
	landed := bead(t, f.st, "the landed bead")
	must(t, f.st.AddNote(ctx, landed.ID, "a landing line", "yardr-refinery"))
	user := bead(t, f.st, "the users bead")
	must(t, f.st.AddNote(ctx, user.ID, "a user line", "me"))
	crew := bead(t, f.st, "the crews bead")
	must(t, f.st.AddNote(ctx, crew.ID, "a crew line", "yardmaster"))

	s := feedSection(t, f.get("/").body)
	if n := len(feedEntries(t, f.get("/").body)); n != 2 {
		t.Errorf("the feed shows %d entries, want the user and crew notes", n)
	}
	for _, id := range []string{user.ID, crew.ID} {
		if !strings.Contains(s, `<span class="wv-id">`+id+`</span>`) {
			t.Errorf("the feed lacks the note on %s", id)
		}
	}
	for _, id := range []string{agent.ID, landed.ID} {
		if strings.Contains(s, `/beads/`+id) {
			t.Errorf("the feed shows the note on %s", id)
		}
	}
	if !strings.Contains(feedBlock(t, f.get("/").body), `href="/?all=1">All activity</a>`) {
		t.Error("the feed names no All activity behind its two")
	}

	noted := 0
	for _, e := range feedEntries(t, f.get("/?all=1").body) {
		if e.what == "noted" {
			noted++
		}
	}
	if noted != 4 {
		t.Errorf("the full feed shows %d notes, want every one", noted)
	}
	h := homeJSON(t, f, "/")
	var kinds int
	for _, e := range h.Events {
		if e.Kind == "noted" {
			kinds++
		}
	}
	if kinds != 4 {
		t.Errorf("the JSON holds %d notes, want every one", kinds)
	}
	for _, c := range []struct{ id, note string }{
		{agent.ID, "an agent line"},
		{landed.ID, "a landing line"},
	} {
		if body := f.get("/beads/" + c.id).body; !strings.Contains(body, c.note) {
			t.Errorf("the bead's page lacks its %q", c.note)
		}
	}
}

// The feed is grouped by the hour: an entry opens an hour where the one above
// it is of another, and the head says the hour, with the day before it where
// the hour is not today's. The groups are the reader's machine's, as the pages
// show times.
func TestFeedByTheHour(t *testing.T) {
	t.Parallel()
	now := time.Now()
	today := func(h, m int) time.Time { return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, time.Local) }
	past := func(h, m int) time.Time { return time.Date(2020, 3, 4, h, m, 0, 0, time.Local) }
	entry := func(seq uint64, at time.Time) web.Entry { return web.Entry{Seq: seq, At: at, Text: "noted"} }
	h := web.Home{All: true, Feed: web.Feed{Events: []web.Entry{
		entry(6, today(14, 58)), entry(5, today(14, 30)), entry(4, today(3, 7)), entry(3, past(14, 50)), entry(2, past(14, 2)), entry(1, past(9, 59)),
	}}}
	var heads []bool
	for _, r := range h.FeedRows() {
		heads = append(heads, r.Head)
	}
	if want := []bool{true, false, true, true, false, true}; !slices.Equal(heads, want) {
		t.Errorf("the entries open hours %v, want %v", heads, want)
	}
	for _, c := range []struct {
		at         time.Time
		minute     string
		label, key string
	}{
		{today(14, 58), "14:58", "14:00", today(14, 0).Format("2006-01-02T15")},
		{today(3, 7), "03:07", "03:00", today(3, 0).Format("2006-01-02T15")},
		{past(14, 50), "14:50", "Mar 4 14:00", "2020-03-04T14"},
		{past(9, 59), "09:59", "Mar 4 09:00", "2020-03-04T09"},
	} {
		e := entry(1, c.at)
		if e.Minute() != c.minute || e.HourLabel() != c.label || e.HourKey() != c.key {
			t.Errorf("%v: minute %q, hour %q (key %q), want %q, %q (%q)", c.at, e.Minute(), e.HourLabel(), e.HourKey(), c.minute, c.label, c.key)
		}
	}
	// The kept lines are the first ten of the kept entries; the full page names
	// every entry of the page.
	kept := web.Home{Kept: slices.Repeat([]web.Entry{entry(1, today(1, 0))}, 12)}
	if n := len(kept.FeedRows()); n != 10 {
		t.Errorf("the kept feed names %d entries, want 10", n)
	}
}
