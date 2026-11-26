package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/web"
)

// answerSection is the overview's head and card: the stage from its answer to
// its feed, without the feed after it.
func answerSection(t *testing.T, text string) string {
	t.Helper()
	_, s, _ := strings.Cut(squash(stageOf(t, text)), `<article class="wv-page" data-kind="overview">`)
	s, _, _ = strings.Cut(s, `<section class="wv-block"`)
	if s == "" {
		t.Fatal("the page has no answer line")
	}
	return s
}

// shortID is a bead id as the depot's rows show it.
func shortID(id string) string { return web.DepotWait{Bead: id}.ShortID() }

// sameReason reports whether two reasons say the same in the JSON: kind,
// text and note. The struct itself is not comparable, since an ask carries
// its options.
func sameReason(a, b web.Reason) bool {
	return a.Kind == b.Kind && a.Text == b.Text && a.Note == b.Note
}

// The top of / answers whether anything waits for the person, and its subline
// splits the count into what they decide and what is held. One waiting bead
// counts once, and a bead with a fault and an ask stands as a fault.
func TestHomeAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setup := func(t *testing.T) *fixture {
		t.Helper()
		f := newFixture(t)
		must(t, f.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
		must(t, f.st.PutGroup(ctx, store.Group{Name: "decisions", Runner: "manual"}, "me"))
		_, err := f.st.AddRoute(ctx, store.Route{Stage: "decide", Group: "decisions"}, "me")
		must(t, err)
		return f
	}
	held := func(t *testing.T, f *fixture, title string) *store.Bead {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Stage: "new", Depot: "home"}, "me")
		must(t, err)
		_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me")
		must(t, err)
		return b
	}
	parked := func(t *testing.T, f *fixture, title string) *store.Bead {
		t.Helper()
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: title, Stage: "decide", Depot: "home"}, "me")
		must(t, err)
		return b
	}
	// answer and subline are the stage's one large line and the sentence
	// under it, as the page says them.
	answer := func(t *testing.T, f *fixture) (head, sub string) {
		t.Helper()
		s := answerSection(t, f.get("/").body)
		m := regexp.MustCompile(`<h1 class="([^"]*)">([^<]*)</h1><p class="wv-answer__sub"[^>]*>(.*?)</p>`).FindStringSubmatch(s)
		if m == nil {
			t.Fatalf("the stage has no answer and subline: %s", s)
		}
		return m[1] + "|" + m[2], tagsRe.ReplaceAllString(m[3], "")
	}

	t.Run("nothing waits", func(t *testing.T) {
		f := setup(t)
		_, err := f.st.CreateBead(ctx, store.NewBead{Title: "at work", Stage: "new", Depot: "home"}, "me")
		must(t, err)
		head, sub := answer(t, f)
		if head != "wv-answer wv-answer--clear|Nothing needs you." || !strings.HasPrefix(sub, "All clear. The rest of depot runs: ") {
			t.Errorf("the answer is %q under %q, want nothing-needs-you, clear, with the yard's numbers", head, sub)
		}
		if s := answerSection(t, f.get("/").body); strings.Contains(s, "wv-ask") || strings.Contains(s, "to decide") {
			t.Errorf("with nothing waiting the stage still shows a card or a count:\n%s", s)
		}
		if h := homeJSON(t, f, "/"); len(h.Attention) != 0 {
			t.Errorf("attention %+v, want none", h.Attention)
		}
	})

	t.Run("one parked", func(t *testing.T) {
		f := setup(t)
		parked(t, f, "which colour")
		head, sub := answer(t, f)
		if head != "wv-answer|1 waits for you" || !strings.HasPrefix(sub, "1 to decide. The rest of depot runs: ") {
			t.Errorf("the answer is %q under %q, want one to decide", head, sub)
		}
	})

	t.Run("one fault", func(t *testing.T) {
		f := setup(t)
		held(t, f, "a door")
		head, sub := answer(t, f)
		if head != "wv-answer|1 waits for you" || !strings.HasPrefix(sub, "1 held. The rest of depot runs: ") {
			t.Errorf("the answer is %q under %q, want one held", head, sub)
		}
	})

	t.Run("two wait, one fault", func(t *testing.T) {
		f := setup(t)
		pb := parked(t, f, "which colour")
		hb := held(t, f, "a door")
		head, sub := answer(t, f)
		if head != "wv-answer|2 wait for you" || !strings.HasPrefix(sub, "1 to decide, 1 held, both in the list. The rest of depot runs: ") {
			t.Errorf("the answer is %q under %q, want one to decide and one held", head, sub)
		}
		// Faults and the rest stand in the list, newest first: both rows.
		waits := section2(t, listSections(t, f.get("/").body), "Waits for you")
		var got []string
		for _, r := range waits.rows {
			got = append(got, r.href)
		}
		if len(got) != 2 || !slices.Contains(got, "/beads/"+hb.ID) || !slices.Contains(got, "/beads/"+pb.ID) {
			t.Errorf("Waits for you holds %v, want the held and the parked bead", got)
		}
	})

	t.Run("faults plural", func(t *testing.T) {
		f := setup(t)
		held(t, f, "a door")
		held(t, f, "a window")
		parked(t, f, "which colour")
		head, sub := answer(t, f)
		if head != "wv-answer|3 wait for you" || !strings.HasPrefix(sub, "1 to decide, 2 held, both in the list. ") {
			t.Errorf("the answer is %q under %q, want one to decide and two held", head, sub)
		}
	})
}

// The answer line in every count, without a yard: how many wait, in the words
// the rail's badge says them in.
func TestAnswer(t *testing.T) {
	t.Parallel()
	wait := web.Waiting{Bead: "a", Why: []web.Reason{{Kind: "held", Text: "held by me"}}}
	for _, c := range []struct {
		name string
		attn []web.Waiting
		want string
	}{
		{"none", nil, "Nothing needs you."},
		{"one", []web.Waiting{wait}, "1 waits for you"},
		{"two", []web.Waiting{wait, wait}, "2 wait for you"},
		{"eleven", slices.Repeat([]web.Waiting{wait}, 11), "11 wait for you"},
	} {
		if got := (web.Home{Attention: c.attn}).Answer(); got != c.want {
			t.Errorf("%s: answer %q, want %q", c.name, got, c.want)
		}
	}
}

// The subline's parts, each left out at zero: the first sentence splits what
// waits into what is decided and what is held and says both stand in the list;
// the second gives the rest of the yard, its numbers linked where a page lists
// them, and is left out with every number of it.
func TestSubline(t *testing.T) {
	t.Parallel()
	ask := web.Waiting{Bead: "a", Why: []web.Reason{{Kind: "ask", Text: "which?"}}}
	parked := web.Waiting{Bead: "b", Why: []web.Reason{{Kind: "manual", Text: "waits"}}}
	fault := web.Waiting{Bead: "c", Why: []web.Reason{{Kind: "held", Text: "held by me"}}}
	faultAsked := web.Waiting{Bead: "d", Why: []web.Reason{{Kind: "held", Text: "held by me"}, {Kind: "ask", Text: "which?"}}}
	depots := []web.HomeDepot{{Open: 30, Working: 3, Transit: 12}, {Open: 4, Working: 1, Transit: 8}}
	for _, c := range []struct {
		name  string
		home  web.Home
		first string
		rest  []web.SublinePart
	}{
		{"nothing at all", web.Home{Yard: "kestrel"}, "All clear.", nil},
		{"only to decide", web.Home{Yard: "kestrel", Attention: []web.Waiting{ask, parked}}, "2 to decide.", nil},
		{"only held", web.Home{Yard: "kestrel", Attention: []web.Waiting{fault}}, "1 held.", nil},
		{"both", web.Home{Yard: "kestrel", Attention: []web.Waiting{ask, fault, fault}}, "1 to decide, 2 held, both in the list.", nil},
		{"a fault that is asked stands once, as a fault", web.Home{Yard: "kestrel", Attention: []web.Waiting{faultAsked}}, "1 held.", nil},
		{"the yard's numbers", web.Home{Yard: "kestrel", Depots: depots, ClosedCount: 5, Late: 1}, "All clear.", []web.SublinePart{
			{Text: "4 moving", Href: "/sessions"}, {Text: "34 open", Href: "/flows"}, {Text: "5 closed today", Href: "/beads?all=1"},
			{Text: "20 arriving"}, {Text: "1 late", Href: "/line"}}},
		{"late only when something is", web.Home{Yard: "kestrel", Depots: depots}, "All clear.", []web.SublinePart{
			{Text: "4 moving", Href: "/sessions"}, {Text: "34 open", Href: "/flows"}, {Text: "20 arriving"}}},
		{"nothing moving, nothing arriving", web.Home{Yard: "kestrel", Depots: []web.HomeDepot{{Open: 2}}, ClosedCount: 1}, "All clear.", []web.SublinePart{
			{Text: "2 open", Href: "/flows"}, {Text: "1 closed today", Href: "/beads?all=1"}}},
	} {
		got := c.home.Subline()
		if got.First != c.first || !reflect.DeepEqual(got.Rest, c.rest) {
			t.Errorf("%s: subline %+v, want %q and %+v", c.name, got, c.first, c.rest)
		}
		if wantIntro := map[bool]string{true: "The rest of kestrel runs:"}[len(c.rest) > 0]; got.Intro != wantIntro {
			t.Errorf("%s: intro %q, want %q", c.name, got.Intro, wantIntro)
		}
	}
}

// The list holds every wait, so the page has no cut to five and no row that
// carries the rest elsewhere: seven waits are seven rows of the list, said once
// in the answer, and the stage lists none of them. The JSON names them all.
func TestHomeNamesEveryWait(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
	var beads []*store.Bead
	for range 7 {
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: "wait", Stage: "new", Depot: "home"}, "me")
		must(t, err)
		beads = append(beads, b)
	}
	for _, b := range beads {
		_, err := f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me")
		must(t, err)
	}
	body := f.get("/").body
	s := answerSection(t, body)
	if !strings.Contains(s, `<h1 class="wv-answer">7 wait for you</h1>`) || !strings.Contains(s, "7 held.") {
		t.Errorf("the answer is not seven held:\n%s", s)
	}
	for _, gone := range []string{"All waiting", `<ul class="lines needs">`, `<li><a href="/beads/`} {
		if strings.Contains(squash(stageOf(t, body)), gone) {
			t.Errorf("the stage keeps %q, which the list's rows replaced", gone)
		}
	}
	waits := section2(t, listSections(t, body), "Waits for you")
	if waits.count != "7" || len(waits.rows) != 7 {
		t.Errorf("Waits for you: %q with %d rows, want all seven", waits.count, len(waits.rows))
	}
	if h := homeJSON(t, f, "/"); len(h.Attention) != 7 {
		t.Errorf("attention names %d waits, want all seven", len(h.Attention))
	}
	if l := lineJSON(t, f); len(l.Waits) != 7 {
		t.Errorf("/line waits %d, want all seven", len(l.Waits))
	}
}

// A held row whose bead's events hold a gave_up for that hold names the
// give-up, on / and on /line alike, and still counts once, as a fault.
func TestHomeNeedsGaveUp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
	b, err := f.st.CreateBead(ctx, store.NewBead{Title: "the github pack", Stage: "review", Depot: "home"}, "me")
	must(t, err)
	_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "yard")
	must(t, err)
	must(t, f.st.LogDispatch(ctx, b.ID, "gave_up", "yard", map[string]any{"stage": "review", "failures": 3}))

	want := web.Reason{Kind: "held", Text: "gave up after 3 failures in review"}
	if got := waiting(lineJSON(t, f)); len(got) != 1 || len(got[0].Why) != 1 || !sameReason(got[0].Why[0], want) {
		t.Errorf("/line waits %+v, want the give-up named", got)
	}
	if h := homeJSON(t, f, "/"); len(h.Attention) != 1 || len(h.Attention[0].Why) != 1 || !sameReason(h.Attention[0].Why[0], want) {
		t.Errorf("attention %+v, want the give-up named", h.Attention)
	}
	// No new field: the reason's JSON is kind and text, as before.
	var l struct {
		Waits []struct {
			Why []map[string]any `json:"why"`
		} `json:"waits"`
	}
	r := f.get("/line", "Accept", "application/json")
	if err := json.Unmarshal([]byte(r.body), &l); err != nil {
		t.Fatal(err)
	}
	for k := range l.Waits[0].Why[0] {
		if k != "kind" && k != "text" {
			t.Errorf("the reason's JSON has %q", k)
		}
	}

	// The list's row says it in red, on / and on /line, and the bead counts
	// once, as a fault.
	for _, path := range []string{"/", "/line"} {
		body := f.get(path).body
		if sub := answerSection(t, body); !strings.Contains(sub, "1 waits for you") || !strings.Contains(sub, "1 held.") {
			t.Errorf("%s: the answer is %q: the bead counts once, as a fault", path, sub)
		}
		row := section2(t, listSections(t, body), "Waits for you").rows
		if len(row) != 1 || len(row[0].lines) == 0 || row[0].lines[0] != "gave up after 3 failures in review" {
			t.Errorf("%s: the wait's row is %+v, want the give-up as its first line", path, row)
		}
		if !strings.Contains(listOf(t, body), `<span class="wv-fault">gave up after 3 failures in review</span>`) {
			t.Errorf("%s: the give-up is not in red", path)
		}
	}

	// A hold lifted and given again, with no new gave_up, is plain again.
	_, err = f.st.SetHold(ctx, b.ID, false, store.Expect{}, "me")
	must(t, err)
	_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me")
	must(t, err)
	if got := waiting(lineJSON(t, f)); len(got) != 1 || len(got[0].Why) != 1 ||
		!sameReason(got[0].Why[0], web.Reason{Kind: "held", Text: "held by me"}) {
		t.Errorf("held again %+v, want a plain hold", got)
	}
	if row := section2(t, listSections(t, f.get("/").body), "Waits for you").rows; len(row) != 1 || row[0].lines[0] != "held by me" {
		t.Errorf("the row no longer says held by me: %+v", row)
	}
}

// A give-up said with what its event has: the dispatcher's has the count
// and the stage, a hand-logged one may have less.
func TestHomeNeedsGaveUpText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	must(t, f.st.PutDepot(ctx, store.Depot{Name: "home", Base: "origin/main"}, "me"))
	cases := []struct {
		title string
		data  map[string]any
		want  string
	}{
		{"full", map[string]any{"stage": "new", "failures": 3}, "gave up after 3 failures in new"},
		{"one", map[string]any{"stage": "new", "failures": 1}, "gave up after 1 failure in new"},
		{"no count", map[string]any{"stage": "new"}, "gave up in new"},
		{"no stage", map[string]any{"failures": 2}, "gave up after 2 failures"},
		{"bare", nil, "gave up"},
	}
	for _, c := range cases {
		b, err := f.st.CreateBead(ctx, store.NewBead{Title: c.title, Stage: "new", Depot: "home"}, "me")
		must(t, err)
		_, err = f.st.SetHold(ctx, b.ID, true, store.Expect{}, "yard")
		must(t, err)
		must(t, f.st.LogDispatch(ctx, b.ID, "gave_up", "yard", c.data))
	}
	got := map[string]string{}
	for _, w := range waiting(lineJSON(t, f)) {
		got[w.Title] = w.Why[0].Text
	}
	for _, c := range cases {
		if got[c.title] != c.want {
			t.Errorf("%s says %q, want %q", c.title, got[c.title], c.want)
		}
	}
}

// Whether a wait is a fault: something stops the bead (a hold, a prompt nobody
// answers, a stage its flow lacks), and not a stage that is a person's by
// design or a question put to them.
func TestWaitingFault(t *testing.T) {
	t.Parallel()
	held := web.Reason{Kind: "held", Text: "held by me"}
	blocked := web.Reason{Kind: "blocked", Text: "its session waits on a prompt: x"}
	stranded := web.Reason{Kind: "stranded", Text: "stranded in limbo"}
	manual := web.Reason{Kind: "manual", Text: "waits for decisions in decide"}
	ask := web.Reason{Kind: "ask", Text: "which?"}
	for _, c := range []struct {
		name  string
		why   []web.Reason
		fault bool
	}{
		{"held", []web.Reason{held}, true},
		{"blocked", []web.Reason{blocked}, true},
		{"stranded", []web.Reason{stranded}, true},
		{"manual", []web.Reason{manual}, false},
		{"ask", []web.Reason{ask}, false},
		{"held and manual", []web.Reason{held, manual}, true},
		{"manual and held", []web.Reason{manual, held}, true},
		{"held and asked", []web.Reason{held, ask}, true},
		{"none", nil, false},
	} {
		if got := (web.Waiting{Bead: "home-x", Stage: "decide", Why: c.why}).Fault(); got != c.fault {
			t.Errorf("%s: fault %v, want %v", c.name, got, c.fault)
		}
	}
}

// A weighty mapping card, on the overview, shows the weight first and the
// decline path with the terminal command instead of the pick buttons, as its
// bead's page does. The options still read as rows, and the words stay with
// Later and Decline, riding them as the reason.
func TestHomeNeedsWeightyMapping(t *testing.T) {
	t.Parallel()
	f, b, seq := mappingActYard(t)
	ctx := context.Background()
	ask := strconv.FormatUint(seq, 10)
	s := answerSection(t, f.get("/").body)
	for _, want := range []string{
		`<p class="wv-ask__weight">WEIGHTY · as-user: lets calls or words count as a person&#39;s: a pick says which user U777 in talk is, and a delivery from them is then heard as that user&#39;s</p>`,
		`This card is weighty and this window has no gate. Approve it in footplate, or in a terminal on the yard's machine:`,
		`Approve it with <code>yardr bead answer ` + b.ID + ` ` + ask + ` &lt;n&gt;</code>`,
		`<span class="wv-opt__n">1</span><span class="wv-opt__b"><span class="wv-opt__t">ada</span>`,
		`<span class="wv-opt__n">2</span><span class="wv-opt__b"><span class="wv-opt__t">mckean</span>`,
		`hx-preserve="true" name="words"`,
		`<button class="wv-btn" type="submit" name="action" value="later">Later</button>`,
		`<button class="wv-btn" type="submit" name="action" value="decline" title="Say no to all options. The asker finds another way.">Decline</button>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the card lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `name="option"`) {
		t.Errorf("the weighty card offers its picks:\n%s", s)
	}
	// A writing pick posted straight at the handler is still the yard's
	// to refuse, and the ask stays open; decline with words still posts.
	rev := revOf(t, f, b.ID)
	r := f.postForm("/beads/"+b.ID, url.Values{"rev": {rev}, "ask": {ask}, "option": {"1"}}, "Origin", f.ts.URL)
	if r.status != 200 || !strings.Contains(r.body, "is weighty (as-user)") || !strings.Contains(r.body, "approve it where its weight is shown") {
		t.Fatalf("status %d, want the redrawn page with the yard's refusal:\n%s", r.status, r.body)
	}
	r = f.postForm("/beads/"+b.ID, url.Values{"rev": {rev}, "ask": {ask}, "words": {"not now"}, "action": {"decline"}}, "Origin", f.ts.URL)
	if r.status != http.StatusSeeOther {
		t.Fatalf("decline: status %d", r.status)
	}
	v, err := f.st.Bead(ctx, b.ID)
	must(t, err)
	if v.Asks[0].State != "declined" || v.Asks[0].Words != "not now" {
		t.Errorf("the ask is %s with %q, want it declined with the words", v.Asks[0].State, v.Asks[0].Words)
	}
}

// A weighty secret card, on the overview, shows the weight with what the ask
// is for, the decline path with the terminal command as its bead's page does,
// and Later and Decline: a secret card offers no giving.
func TestHomeNeedsWeightySecret(t *testing.T) {
	t.Parallel()
	f, b, seq := secretActYard(t)
	s := answerSection(t, f.get("/").body)
	for _, want := range []string{
		`<p class="wv-ask__weight">WEIGHTY · credential: gives a credential or an address to the yard: CRM_TOKEN, for the source crm (https://crm.example.com)</p>`,
		`a secret: CRM_TOKEN, for the source crm (https://crm.example.com)`,
		`This card is weighty and this window has no gate. Approve it in footplate, or in a terminal on the yard's machine:`,
		`Give it with <code>yardr bead answer ` + b.ID + ` ` + strconv.FormatUint(seq, 10) + ` --secret</code>`,
		`<button class="wv-btn" type="submit" name="action" value="later">Later</button>`,
		`<button class="wv-btn" type="submit" name="action" value="decline" title="Say no to all options. The asker finds another way.">Decline</button>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the card lacks %q:\n%s", want, s)
		}
	}
	for _, foreign := range []string{`name="option"`, `type="password"`, `value="give"`} {
		if strings.Contains(s, foreign) {
			t.Errorf("the secret card shows %q:\n%s", foreign, s)
		}
	}
}
