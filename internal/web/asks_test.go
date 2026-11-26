package web_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
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

// What the ask card draws of its controls and its options (templates/ask.html),
// as a page says them: the buttons of its answer row (declineButton is
// approve_test.go's), the pick button of an option (the recommended one marked,
// as the card marks it), and the row of an option that cannot be picked, where
// the view does not act or a pick would be refused.
const (
	sendButton  = `<button class="wv-btn" type="submit" name="action" value="send">Send</button>`
	laterButton = `<button class="wv-btn" type="submit" name="action" value="later">Later</button>`
)

func optionSpans(n int, label, consequence string) string {
	var c string
	if consequence != "" {
		c = `<span class="wv-opt__c">` + consequence + `</span>`
	}
	return fmt.Sprintf(`<span class="wv-opt__n">%d</span><span class="wv-opt__b"><span class="wv-opt__t">%s</span>%s</span>`, n, label, c)
}

func pickButton(n int, label, consequence string, rec bool) string {
	class, title := "wv-opt", ""
	if rec {
		class += " wv-opt--rec"
		title = fmt.Sprintf(` title="Answer: %d · %s"`, n, label)
	}
	return fmt.Sprintf(`<button class="%s" type="submit" name="option" value="%d"%s>%s</button>`, class, n, title, optionSpans(n, label, consequence))
}

func optionRow(n int, label, consequence string, rec bool) string {
	class := "wv-opt"
	if rec {
		class += " wv-opt--rec"
	}
	return fmt.Sprintf(`<div class="%s">%s</div>`, class, optionSpans(n, label, consequence))
}

// pickOptions are the options pickAsk puts, as the card draws them as pick
// buttons: the first recommended, the yard's own You decide last.
var pickOptions = []string{
	pickButton(1, "Move them now", "all links point at the new source tonight", true),
	pickButton(2, "After Friday", "keep the old source until the release is out", false),
	pickButton(3, "You decide", "leave it to yardmaster", false),
}

// cardsOf are the ask cards on the page's stage, each from its opening tag to
// its closing one, squashed.
func cardsOf(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	rest := squash(stageOf(t, body))
	for {
		_, after, ok := strings.Cut(rest, `<article class="wv-ask" `)
		if !ok {
			return out
		}
		card, next, _ := strings.Cut(after, `</article>`)
		out = append(out, `<article class="wv-ask" `+card+`</article>`)
		rest = next
	}
}

// oneCard is the only ask card on the page's stage.
func oneCard(t *testing.T, body string) string {
	t.Helper()
	cards := cardsOf(t, body)
	if len(cards) != 1 {
		t.Fatalf("the stage has %d ask cards, want one:\n%s", len(cards), stageOf(t, body))
	}
	return cards[0]
}

// formsOf are the forms of a card, each from its opening tag to its closing
// one.
func formsOf(card string) []string {
	var out []string
	rest := card
	for {
		_, after, ok := strings.Cut(rest, "<form")
		if !ok {
			return out
		}
		form, next, _ := strings.Cut(after, "</form>")
		out = append(out, "<form"+form+"</form>")
		rest = next
	}
}

// submitter matches the start tag of a button or an input, which is where a
// form's submit buttons are found.
var submitter = regexp.MustCompile(`<(button|input)\b(?:[^>"']|"[^"]*"|'[^']*')*>`)

// defaultButton is the attributes of the form's default button, the first
// submit button in tree order, which is the one a browser presses when a
// person hits Enter in a text field. It is nil where the form has none.
func defaultButton(form string) map[string]string {
	for _, m := range submitter.FindAllStringSubmatch(form, -1) {
		a := startTags(m[0], m[1])[0]
		ty, typed := a["type"]
		// A button with no type submits, as an input of type submit or image
		// does; any other type submits nothing.
		if m[1] == "button" && typed && !strings.EqualFold(ty, "submit") ||
			m[1] == "input" && !strings.EqualFold(ty, "submit") && !strings.EqualFold(ty, "image") {
			continue
		}
		return a
	}
	return nil
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

	body := f.get("/").body
	s := answerSection(t, body)
	if !strings.Contains(s, `<h1 class="wv-answer">1 waits for you</h1>`) || !strings.Contains(s, "1 to decide.") {
		t.Errorf("the answer is %q, want one to decide and no fault", s)
	}
	// The list's row says the question first, the bead and its title under
	// it, and who asked; the age ends it in rust.
	rows := section2(t, listSections(t, body), "Waits for you").rows
	if len(rows) != 1 || rows[0].href != "/beads/"+b.ID || rows[0].name != "Move the links now?" || rows[0].dot != "wait" ||
		len(rows[0].lines) != 2 || rows[0].lines[0] != b.ID+" · a door" || rows[0].lines[1] != "asked by yardmaster" {
		t.Errorf("the ask's row is %+v", rows)
	}
	// The stage stands the ask whole, once, with its age in the head.
	card := oneCard(t, body)
	if !strings.Contains(card, `<span>yardmaster asks you</span><span class="wv-ask__age">ask `) || !strings.Contains(card, ` · `) ||
		!strings.Contains(card, ` ago</span>`) {
		t.Errorf("the card's head says no asker, no ask number or no age: %s", card)
	}
	for _, not := range []string{"held", "All waiting"} {
		if strings.Contains(s, not) {
			t.Errorf("the ask's stage has %q", not)
		}
	}
	_ = ctx
}

// The card on the stage stands whole, as on its bead's page: the head, the
// question, the about line that names the bead, the context lines, each option
// with its consequence as the pick button, and the recommendation labelled
// under its own row.
func TestHomeNeedsAskDetail(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	if body := f.get("/").body; strings.Contains(body, "wv-ask") {
		t.Errorf("a bead with no ask has a card: %s", body)
	}
	seq := askOn(t, f, b.ID, pickAsk())

	// The view does not act, so the options are rows to read and nothing
	// posts.
	card := oneCard(t, f.get("/").body)
	for _, want := range []string{
		`id="ask-` + b.ID + `-` + strconv.FormatUint(seq, 10) + `">`,
		`<p class="wv-ask__head"><span class="wv-dot wv-dot--wait"></span><span>yardmaster asks you</span><span class="wv-ask__age">ask ` + strconv.FormatUint(seq, 10) + ` · `,
		`<h2 class="wv-ask__q">Move the links now?</h2>`,
		`<p class="wv-ask__about"><a class="wv-id" href="/beads/` + b.ID + `">` + b.ID + `</a><span>a door</span><span>in new</span></p>`,
		`<ul class="wv-ctx"><li>The links file declares the new source.</li><li>The old source goes away Friday.</li></ul>`,
		optionRow(1, "Move them now", "all links point at the new source tonight", true),
		`<p class="wv-rec">recommended: the test passed</p>`,
		optionRow(2, "After Friday", "keep the old source until the release is out", false),
		// The yard's own last pick, the shape a card stored before
		// yardr-17bj keeps: it stands as a plain row, recommended by
		// nobody.
		optionRow(3, "You decide", "leave it to yardmaster", false),
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the home ask lacks %q:\n%s", want, card)
		}
	}
	for _, foreign := range []string{"<form", "<button", "Recommended:", "Recommends", "to approve:", "need-ask", `class="options"`} {
		if strings.Contains(card, foreign) {
			t.Errorf("the read-only ask shows %q", foreign)
		}
	}
	// A view that acts answers with the options as its pick buttons.
	g, _ := talkFixture(t, "jo")
	actYard(t, g, pickAsk())
	acting := oneCard(t, g.get("/").body)
	for _, want := range pickOptions {
		if !strings.Contains(acting, want) {
			t.Errorf("the acting card lacks the pick %q:\n%s", want, acting)
		}
	}
	if !strings.Contains(acting, `<form class="wv-ask__form" method="post" action="/beads/`) {
		t.Errorf("the acting card posts to no bead:\n%s", acting)
	}
}

// One card stands on the stage, whole: the oldest ask of the first asked wait
// of the list, whatever else waits before it or asks after it. Every other
// asked wait has its row in the list, which opens its bead's page.
func TestHomeStandsOneCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := askFixture(t)
	older := askBead(t, f, "the older ask", "new")
	askOn(t, f, older.ID, store.Ask{Question: "Older bead's question?", To: "mckean", Context: []string{"older."}})
	newer := askBead(t, f, "the newer ask", "new")
	askOn(t, f, newer.ID, store.Ask{Question: "Newer bead's first?", To: "mckean", Context: []string{"first."}})
	askOn(t, f, newer.ID, store.Ask{Question: "Newer bead's second?", To: "mckean", Context: []string{"second."}})
	// The bead that began to wait last stands first in the list, and it has no
	// ask: the card is of the first row that has one.
	held := askBead(t, f, "held, not asked", "new")
	_, err := f.st.SetHold(ctx, held.ID, true, store.Expect{}, "me")
	must(t, err)

	for _, path := range []string{"/", "/line"} {
		body := f.get(path).body
		rows := section2(t, listSections(t, body), "Waits for you").rows
		if len(rows) != 3 || rows[0].href != "/beads/"+held.ID || rows[1].href != "/beads/"+newer.ID || rows[2].href != "/beads/"+older.ID {
			t.Fatalf("%s: Waits for you holds %+v, want the held bead, the newer asked bead, then the older", path, rows)
		}
		card := oneCard(t, body)
		if !strings.Contains(card, `<h2 class="wv-ask__q">Newer bead&#39;s first?</h2>`) || !strings.Contains(card, `<li>first.</li>`) ||
			!strings.Contains(card, `href="/beads/`+newer.ID+`"`) {
			t.Errorf("%s: the card is not the oldest ask of the first asked row:\n%s", path, card)
		}
		for _, not := range []string{"Newer bead&#39;s second?", "Older bead&#39;s question?", "second.", "older."} {
			if strings.Contains(stageOf(t, body), not) {
				t.Errorf("%s: the stage has %q, which is no card's of the one standing", path, not)
			}
		}
		// The other asked wait is a row, a tap from its bead's page.
		if row := rows[2]; row.name != "Older bead's question?" || row.dot != "wait" {
			t.Errorf("%s: the older asked wait's row is %+v", path, row)
		}
	}
}

// The card's typed words are kept through a read of the page again, which the
// live pages make at every event: the words field has an id that the next read
// gives it again, and the read lets that one stand. The form around it is not
// kept: its revision is the bead's as of the read, and a form kept past the
// next event would post a stale one, refused again at every retry.
func TestAskCardKeepsItsWords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _ := talkFixture(t, "jo")
	b, seq := actYard(t, f, pickAsk())
	field := regexp.MustCompile(`<input class="wv-field" type="text" id="(words-[^"]*)" hx-preserve="true" name="words"`)
	rev := regexp.MustCompile(`<input type="hidden" name="rev" value="([^"]*)">`)
	want := fmt.Sprintf("words-%s-%d", b.ID, seq)
	first := map[string]string{}
	for _, path := range []string{"/", "/line", "/beads/" + b.ID} {
		card := oneCard(t, f.get(path).body)
		m := field.FindStringSubmatch(card)
		if m == nil || m[1] != want {
			t.Errorf("%s: the card's words field is not %q with hx-preserve: %s", path, want, card)
			continue
		}
		if n := strings.Count(card, "hx-preserve"); n != 1 {
			t.Errorf("%s: %d elements of the card are kept, want the words field alone: %s", path, n, card)
		}
		first[path] = rev.FindStringSubmatch(card)[1]
	}
	// The bead changes between two reads: the field keeps its id, so the read
	// leaves what is typed in it, and the form's revision is the fresh one.
	must(t, f.st.AddNote(ctx, b.ID, "something happened", "builder"))
	for _, path := range []string{"/", "/line", "/beads/" + b.ID} {
		card := oneCard(t, f.get(path).body)
		if m := field.FindStringSubmatch(card); m == nil || m[1] != want {
			t.Errorf("%s: the next read gives the words field another id: %s", path, card)
		}
		if now := rev.FindStringSubmatch(card)[1]; now == first[path] || now != revOf(t, f, b.ID) {
			t.Errorf("%s: the form's revision is %q after the bead changed, want the fresh %q (was %q)", path, now, revOf(t, f, b.ID), first[path])
		}
	}
}

// Enter in a text field of an ask card presses its form's default button, the
// first submit button in tree order. So each form of a card opens with a
// button of its own, and it is never an option's pick or an approval, which a
// person who only typed words did not choose and cannot take back. It does
// what the card's words button does, Send where there are options and Answer
// where there are none, and on a secret card it gives. Where the card has none
// of those it is disabled, which makes Enter submit nothing. Home and the line
// draw the same forms, and on a card with neither pick nor approval their
// first button used to be Later.
func TestAskCardEnterPressesNoPickAndNoApproval(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// build is a yard with the card in it, as a way to read a page of it,
		// and the bead the card is on.
		build func(t *testing.T) (read func(path string) string, bead string)
		// stands is a control the card draws on its bead's page that Enter
		// must not press, one the default button used to be or could have
		// been.
		stands string
		// onBead and away list the default button of each of the card's
		// forms in order, on its bead's page and away from it, on home and on
		// the line. Each is the action the button carries, or "" for the
		// disabled one.
		onBead, away []string
	}{
		{"options and words", func(t *testing.T) (func(string) string, string) {
			f, _ := talkFixture(t, "jo")
			b, _ := actYard(t, f, pickAsk())
			return func(path string) string { return f.get(path).body }, b.ID
		}, pickOptions[0], []string{"send"}, []string{"send"}},
		{"words alone", func(t *testing.T) (func(string) string, string) {
			f, _ := talkFixture(t, "jo")
			b, _ := actYard(t, f, store.Ask{Question: "What should it say?", Context: []string{"the sign."}})
			return func(path string) string { return f.get(path).body }, b.ID
		}, `value="answer"`, []string{"answer"}, []string{"answer"}},
		{"a secret given on its page", func(t *testing.T) (func(string) string, string) {
			y := newApproveYard(t, true)
			y.putCRM(false)
			b := y.bead("the crm key")
			y.askSecret(b.ID, "jo")
			tok := y.token("jo@example.net")
			return func(path string) string { return y.surfaceBody(path, tok) }, b.ID
		}, `value="give"`, []string{"give"}, []string{""}},
		{"a secret given elsewhere", func(t *testing.T) (func(string) string, string) {
			f, b, _ := secretActYard(t)
			return func(path string) string { return f.get(path).body }, b.ID
		}, `value="later"`, []string{""}, []string{""}},
		{"a run card", func(t *testing.T) (func(string) string, string) {
			y := newApproveYard(t, true)
			b := y.bead("raise the session limit")
			y.askRun(b.ID, "jo", "settings", "set", "max_sessions", "20")
			tok := y.token("jo@example.net")
			return func(path string) string { return y.surfaceBody(path, tok) }, b.ID
		}, `value="approve"`, []string{""}, []string{""}},
		{"a setup card", func(t *testing.T) (func(string) string, string) {
			y := newApproveYard(t, true)
			servesSetupmail(y.own)
			b := y.bead("set up mail")
			y.askSetup(b.ID)
			tok := y.token("jo@example.net")
			return func(path string) string { return y.surfaceBody(path, tok) }, b.ID
		}, `value="approve"`, []string{"", ""}, []string{"", ""}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			read, bead := c.build(t)
			for i, s := range approveSurfaces {
				want := c.away
				if i == 0 {
					want = c.onBead
				}
				card := oneCard(t, read(s.path(bead)))
				if i == 0 && !strings.Contains(card, c.stands) {
					t.Errorf("%s: the card lacks %q, so the test shows nothing:\n%s", s.name, c.stands, card)
				}
				forms := formsOf(card)
				if len(forms) != len(want) {
					t.Errorf("%s: the card has %d forms, want %d:\n%s", s.name, len(forms), len(want), card)
					continue
				}
				for n, form := range forms {
					b := defaultButton(form)
					if b == nil || b["class"] != "wv-enter" {
						t.Errorf("%s, form %d: the first submit button is %v, want the wv-enter button:\n%s", s.name, n+1, b, form)
						continue
					}
					// It is out of the tab order and out of a screen reader's
					// reach, and nothing hides it: a browser may pass over a
					// default button that is hidden.
					if b["tabindex"] != "-1" || b["aria-hidden"] != "true" {
						t.Errorf("%s, form %d: the default button is in the tab order or in a screen reader's reach: %v", s.name, n+1, b)
					}
					if _, hidden := b["hidden"]; hidden || b["style"] != "" {
						t.Errorf("%s, form %d: the default button is hidden: %v", s.name, n+1, b)
					}
					_, disabled := b["disabled"]
					switch {
					case want[n] == "" && (!disabled || b["name"] != "" || b["value"] != ""):
						t.Errorf("%s, form %d: the default button is %v, want it disabled, with no name", s.name, n+1, b)
					case want[n] != "" && (disabled || b["name"] != "action" || b["value"] != want[n]):
						t.Errorf("%s, form %d: the default button is %v, want action %q", s.name, n+1, b, want[n])
					}
				}
			}
		})
	}
	// The button is out of sight and takes no room. No rule gives it a display
	// or hides it, since some browsers then skip implicit submission.
	rule := cssRule(t, ".wv-enter")
	for _, want := range []string{"position: absolute;", "width: 1px;", "height: 1px;", "overflow: hidden;", "clip-path: inset(50%);"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".wv-enter lacks %s: %s", want, rule)
		}
	}
	if strings.Contains(rule, "display") {
		t.Errorf(".wv-enter sets display, which some browsers then skip as a default button: %s", rule)
	}
}

// A bead that both waits and carries an open ask stands once, fault first:
// the held lead, then the question. It counts once, as a fault, and its ask is
// the card the stage stands.
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
	body := f.get("/").body
	s := answerSection(t, body)
	if !strings.Contains(s, "1 waits for you") || !strings.Contains(s, "1 held.") || strings.Contains(s, "to decide") {
		t.Errorf("the answer is %q: the bead counts once, as a fault", s)
	}
	// One row in the list: the fault first, then the question where the
	// note would stand.
	rows := section2(t, listSections(t, body), "Waits for you").rows
	if len(rows) != 1 || rows[0].name != "a door" || len(rows[0].lines) != 2 ||
		rows[0].lines[0] != "held by me" || rows[0].lines[1] != "Move the links now?" {
		t.Errorf("the row is %+v, want the hold first and the question after", rows)
	}
	if !strings.Contains(oneCard(t, body), `<h2 class="wv-ask__q">Move the links now?</h2>`) {
		t.Error("the stage does not stand the held bead's ask")
	}
}

// /line stands the same card as the home page, whole, never folded: its stage
// is the overview.
func TestLineAskContext(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new")
	askOn(t, f, b.ID, pickAsk())

	got := waiting(lineJSON(t, f))
	if len(got) != 1 || len(got[0].Why) != 1 || got[0].Why[0].Kind != "ask" {
		t.Fatalf("/line waits %+v, want the ask", got)
	}
	home, line := oneCard(t, f.get("/").body), oneCard(t, f.get("/line").body)
	// The ages are of the moment each page was drawn.
	if sameWhole(home) != sameWhole(line) {
		t.Errorf("/line's card is not home's:\n%s\n%s", line, home)
	}
	for _, want := range []string{
		`<h2 class="wv-ask__q">Move the links now?</h2>`,
		`<li>The links file declares the new source.</li>`,
		`<li>The old source goes away Friday.</li>`,
		optionRow(1, "Move them now", "all links point at the new source tonight", true),
		`<p class="wv-rec">recommended: the test passed</p>`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("/line lacks %q", want)
		}
	}
	// The card is not folded under its question any more, and the question
	// stands in the card and Decide's list alone.
	text := squash(f.get("/line").body)
	for _, not := range []string{"<details class=\"wait-ask\"", "fold-closed", "Recommended:", "Recommends", "to approve:"} {
		if strings.Contains(text, not) {
			t.Errorf("/line shows %q", not)
		}
	}
	if n := strings.Count(text, "Move the links now?"); n != 2 {
		t.Errorf("/line says the question %d times, want twice: the card and its row of Decide's list", n)
	}
}

// The card does not claim a recommendation it has none of. An ask with no
// options wants words, and one with options but no recommendation leaves every
// option an unmarked row.
func TestHomeNeedsAskRecommend(t *testing.T) {
	t.Parallel()
	t.Run("words", func(t *testing.T) {
		f := askFixture(t)
		words := askBead(t, f, "words", "new")
		askOn(t, f, words.ID, store.Ask{
			Question: "What should the greeting say?", To: "mckean",
			Context: []string{"the greeting."},
		})
		card := oneCard(t, f.get("/").body)
		for _, not := range []string{"wv-opts", "wv-rec", "recommended", "words wanted"} {
			if strings.Contains(card, not) {
				t.Errorf("the words card has %q: %s", not, card)
			}
		}
	})
	t.Run("plain", func(t *testing.T) {
		f := askFixture(t)
		plain := askBead(t, f, "plain", "new")
		askOn(t, f, plain.ID, store.Ask{
			Question: "Which colour?", To: "mckean", Context: []string{"the door."},
			Options: []store.AskOption{
				{Label: "Red", Consequence: "paint it red"},
				{Label: "Blue", Consequence: "paint it blue"},
			},
		})
		card := oneCard(t, f.get("/").body)
		if strings.Contains(card, "Recommended:") || strings.Contains(card, "wv-rec") || strings.Contains(card, "wv-opt--rec") {
			t.Errorf("the unrecommended card names a recommendation: %s", card)
		}
		for _, want := range []string{optionRow(1, "Red", "paint it red", false), optionRow(2, "Blue", "paint it blue", false)} {
			if !strings.Contains(card, want) {
				t.Errorf("the unrecommended card lacks %q", want)
			}
		}
	})
}

// The web stays flat and names the bead's first topic label on the card's
// about line where there is one, without its prefix.
func TestHomeNeedsAskTopic(t *testing.T) {
	t.Parallel()
	f := askFixture(t)
	b := askBead(t, f, "a door", "new", "topic:linear-out", "area:web")
	askOn(t, f, b.ID, pickAsk())
	card := oneCard(t, f.get("/").body)
	if want := `<span>in new</span><span>linear-out</span></p>`; !strings.Contains(card, want) {
		t.Errorf("the card's about line lacks %q: %s", want, card)
	}
	if strings.Contains(card, "topic:") || strings.Contains(card, "area:") {
		t.Error("the card shows the raw label")
	}
	// A bead with no topic label says none: the line ends with its stage.
	g := askFixture(t)
	nolabel := askBead(t, g, "no label", "new")
	askOn(t, g, nolabel.ID, pickAsk())
	card = oneCard(t, g.get("/").body)
	if want := `<span>in new</span></p>`; !strings.Contains(card, want) || strings.Contains(card, "Everything else") {
		t.Errorf("a bead with no topic has an about line that is not its stage's: %s", card)
	}
}

// A stage card put now reads whole on Home and its bead's page: the frozen
// bead header, the moves, and no You decide. Its about line names the bead's
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
	s := oneCard(t, f.get("/").body)
	for _, want := range []string{
		`<span>in review</span><span>web-view</span></p>`,
		`<h2 class="wv-ask__q">Where should this bead go next? — ` + fresh.ID + `: a door</h2>`,
		`<li>Bead: ` + fresh.ID + ` — a door (branch yard/` + fresh.ID + `)</li>`,
		`<span>yard asks you</span>`,
		`<p class="wv-rec">recommended: The question is small and the branch is green.</p>`,
		pickButton(1, "done", "The bead moves to approved with your words as the note.", true),
		pickButton(2, "changes", "The bead goes back for changes.", false),
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
	if !strings.Contains(page, `<span>yard asks you</span><span class="wv-ask__age">ask `) ||
		!strings.Contains(page, `— `+fresh.ID+`: a door</h2>`) {
		t.Errorf("the bead page lacks the card's head and frozen header:\n%s", page)
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
	// Two wait, and the one card the stage stands is of the journal's or the
	// open bead's: whichever of them waited last.
	body := f.get("/").body
	if page := answerSection(t, body); !strings.Contains(page, "2 wait for you") ||
		(!strings.Contains(page, `href="/beads/`+crew.Journal+`">`) && !strings.Contains(page, `href="/beads/`+open.ID+`">`)) {
		t.Errorf("the overview is %q, want two waits and a card of the open ask or the journal's", page)
	}
	if rows := section2(t, listSections(t, body), "Waits for you").rows; len(rows) != 2 {
		t.Errorf("Waits for you has %d rows, want the open ask's and the journal's", len(rows))
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
	s := oneCard(t, f.get("/").body)
	if rows := section2(t, listSections(t, f.get("/").body), "Waits for you").rows; len(rows) != 1 {
		t.Fatalf("the bead stands in %d rows, want one", len(rows))
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
	if tile := tileOf(t, body, "/"); !strings.Contains(tile, `title="1 waits for you" aria-label="Decide, 1 waits for you"`) ||
		!strings.Contains(tile, `<span class="wv-badge" aria-hidden="true">1</span>`) {
		t.Errorf("two asks on one bead do not count once in Decide: %s", tile)
	}
	if strings.Contains(body, `title="Crew —`) {
		t.Errorf("Crew carries a badge where the view acts as no user: %s", railOf(t, body))
	}
	if tile := tileOf(t, body, "/flows"); strings.Contains(tile, "wv-badge") {
		t.Errorf("Lines carries a badge: %s", tile)
	}
	// A held asked bead stands once, and a held bead with no ask waits too:
	// the badge counts what waits, the asks among it.
	ctx := context.Background()
	must(t, func() error { _, err := f.st.SetHold(ctx, b.ID, true, store.Expect{}, "me"); return err }())
	plain := askBead(t, f, "a plain door", "new")
	must(t, func() error { _, err := f.st.SetHold(ctx, plain.ID, true, store.Expect{}, "me"); return err }())
	body = f.get("/").body
	if tile := tileOf(t, body, "/"); !strings.Contains(tile, `title="2 wait for you"`) {
		t.Errorf("a held asked bead and a held bead with no ask do not count as two waits: %s", tile)
	}
	// The numeral caps above nine; the accessible name retains the count.
	for i := 0; i < 9; i++ {
		extra := askBead(t, f, "another bead", "new")
		askOn(t, f, extra.ID, store.Ask{Question: "Move it?", To: "mckean", Context: []string{"The shed is full."}})
	}
	body = f.get("/").body
	if tile := tileOf(t, body, "/"); !strings.Contains(tile, `title="11 wait for you" aria-label="Decide, 11 wait for you"`) ||
		!strings.Contains(tile, `<span class="wv-badge" aria-hidden="true">9&#43;</span>`) {
		t.Errorf("eleven waiting beads do not use the capped badge: %s", tile)
	}
}

// The ask helpers: whether the bead carries one, its oldest ask, and whether
// something stops it, which an ask alone does not.
func TestWaitingAsk(t *testing.T) {
	t.Parallel()
	ask := web.Reason{Kind: "ask", Text: "Move them now?", Recommend: 1, Options: []store.AskOption{{Label: "Move them now"}}}
	held := web.Reason{Kind: "held", Text: "held by me"}
	manual := web.Reason{Kind: "manual", Text: "waits for decisions in decide"}
	if w := (web.Waiting{Bead: "b", Stage: "new", Why: []web.Reason{ask}}); !w.HasAsk() ||
		w.FirstAsk() == nil || w.FirstAsk().Text != "Move them now?" || w.Fault() {
		t.Errorf("ask-only: %+v", w)
	}
	if w := (web.Waiting{Bead: "b", Stage: "new", Why: []web.Reason{held, ask}}); !w.HasAsk() ||
		w.FirstAsk().Text != "Move them now?" || !w.Fault() {
		t.Errorf("held and asked: %+v", w)
	}
	if w := (web.Waiting{Bead: "b", Stage: "decide", Why: []web.Reason{manual, ask}}); !w.HasAsk() || w.Fault() {
		t.Errorf("parked and asked: %+v", w)
	}
	if w := (web.Waiting{Bead: "b", Stage: "new", Why: []web.Reason{held}}); w.HasAsk() || w.FirstAsk() != nil {
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
	if w.FirstAsk() == nil || w.FirstAsk().Text != want {
		t.Errorf("the row's question %+v, want the folded %q", w.FirstAsk(), want)
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
	body := f.get("/").body
	if rows := section2(t, listSections(t, body), "Waits for you").rows; len(rows) != 1 || rows[0].name != want {
		t.Errorf("the list's row lacks the folded question: %+v", rows)
	}
	s := oneCard(t, body)
	if !strings.Contains(s, `<h2 class="wv-ask__q">`+want+`</h2>`) {
		t.Errorf("the card lacks the folded question")
	}
	// The run card reads as on its bead's page: the sentence naming its
	// yard, the command whole, and the take-back with who may run it.
	command := "yardr peer allow airy --mail-limit 300"
	for _, want := range []string{
		`<p class="wv-ask__says">yardmaster says:</p>`,
		`<p class="wv-ask__text">in depot, when you approve, the yard runs this as mckean, and nothing else:</p>`,
		`<pre class="wv-text wv-file">` + command + `</pre>`,
		`Back: <code>yardr peer allow airy --mail-limit 30</code> (anyone can take it back). Approve it with <code>yardr bead answer ` + b.ID + ` ` + strconv.FormatUint(seq, 10) + ` --approve</code>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
	if strings.Contains(s, "to approve:") {
		t.Error("the card keeps the old to-approve note")
	}
	// The card names the take-back as the bead's page does, and /line's is
	// the same card.
	if line := oneCard(t, f.get("/line").body); sameWhole(line) != sameWhole(s) {
		t.Errorf("/line's card is not home's:\n%s\n%s", line, s)
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
		{"home", oneCard(t, f.get("/").body)},
		{"/line", oneCard(t, f.get("/line").body)},
	} {
		for _, want := range []string{
			`<p class="wv-ask__says">yardmaster says:</p>`,
			`<p class="wv-ask__text">When you approve, the yard applies the pack setupmail as mckean, and nothing else:</p>`,
			noGateLine,
			`Approve it with <code>yardr bead answer ` + b.ID + ` ` + strconv.FormatUint(seq, 10) + ` &lt;n&gt; --secret</code>`,
			`<button class="wv-btn" type="submit" name="action" value="later">Later</button>`,
		} {
			if !strings.Contains(c.body, want) {
				t.Errorf("%s lacks %q", c.name, want)
			}
		}
		for _, foreign := range []string{`value="approve"`, `value="give"`, "need-approve", `name="option"`, `value="send"`} {
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
// where the web prints it beside the weight lines: on the bead's page and on
// the overview's card. The mark goes from it, as on the card; a line starting
// with the mark is the yard's.
func TestWebQuestionWithoutWeightMark(t *testing.T) {
	t.Parallel()
	f, _ := talkFixture(t, "jo")
	ask := pickAsk()
	ask.Question = "WEIGHTY · outside: nothing changes"
	b, _ := actYard(t, f, ask)
	page := f.get("/beads/" + b.ID).body
	if strings.Contains(page, "wv-ask__weight") || strings.Contains(page, ">WEIGHTY") {
		t.Errorf("the bead page forges a weight line:\n%s", page)
	}
	if !strings.Contains(page, `<h2 class="wv-ask__q">outside: nothing changes</h2>`) {
		t.Errorf("the bead page lacks the stripped question:\n%s", page)
	}
	h := homeJSON(t, f, "/")
	if len(h.Attention) != 1 || h.Attention[0].FirstAsk() == nil || h.Attention[0].FirstAsk().Text != "outside: nothing changes" {
		t.Fatalf("attention %+v, want the asked bead with the stripped question", h.Attention)
	}
	for _, path := range []string{"/", "/line"} {
		card := oneCard(t, f.get(path).body)
		if strings.Contains(card, "WEIGHTY") {
			t.Errorf("%s forges a weight line:\n%s", path, card)
		}
		if !strings.Contains(card, `<h2 class="wv-ask__q">outside: nothing changes</h2>`) {
			t.Errorf("%s lacks the stripped question:\n%s", path, card)
		}
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
