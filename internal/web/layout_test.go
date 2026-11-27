package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// The window is three columns and the document does not scroll: the rail is
// 64 wide, the list 340 and the stage takes the rest, with its content in one
// column of 760 at most, centred, with a gutter. The markup carries the rail,
// the list and the stage on every page (TestEveryPageIsTheWholeShell), the
// stylesheet the measures and the readable text; what the pixels do is the
// bead's screenshots.
func TestWindowFillsTheWindow(t *testing.T) {
	t.Parallel()
	css := string(readAsset(t, "yard.css"))
	for _, want := range []string{
		"--wv-rail: 64px;", "--wv-list: 340px;", "--wv-measure: 760px;", "--wv-gutter: 24px;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks the measure %s", want)
		}
	}
	shell := cssRule(t, ".wv-shell")
	for _, want := range []string{
		"display: grid;",
		"grid-template-columns: var(--wv-rail) var(--wv-list) minmax(0, 1fr);",
		// The grid is the window's height, so no column makes the document
		// taller than it.
		"height: 100vh;", "height: 100dvh;",
	} {
		if !strings.Contains(shell, want) {
			t.Errorf(".wv-shell lacks %s: %s", want, shell)
		}
	}
	if body := cssRule(t, "body"); !strings.Contains(body, "overflow: hidden;") {
		t.Errorf("the document scrolls: %s", body)
	}
	// The stage's content is one column of the measure, centred: a rule on
	// what stands directly in the stage, so a page needs no wrapper of its
	// own for it, and the phone's top bar and a leaf stand apart from it.
	content := cssRule(t, ".wv-stage > :not(.wv-top):not(.wv-live)")
	for _, want := range []string{"width: min(var(--wv-measure), 100% - 2 * var(--wv-gutter));", "margin-inline: auto;"} {
		if !strings.Contains(content, want) {
			t.Errorf("the stage's content lacks %s: %s", want, content)
		}
	}
	for _, want := range []string{
		// Running text keeps a measure of about 80 characters, in the stage kind's
		// words, and so do a note's lines.
		".wv-text { max-width: 80ch; font: 400 14px/20px var(--sans); white-space: pre-wrap; overflow-wrap: anywhere; }",
		".wv-note p { max-width: 80ch; font: 400 14px/20px var(--sans); white-space: pre-wrap; overflow-wrap: anywhere; }",
		// A feed line keeps its one line.
		".wv-entry__body p { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks %s", want)
		}
	}
	// What the window took the place of is gone: the three fixed columns,
	// the sidebar, the right column, the header's count, the phone's More.
	for _, gone := range []string{"--sidebar-width", "--centre-width", "--right-width", ".sidebar", ".side-", ".right ", ".centre", "needs-count", ".rail .more", "aside"} {
		if strings.Contains(css, gone) {
			t.Errorf("the stylesheet still names %s", gone)
		}
	}
}

// The list and the stage scroll apart, each inside itself: the list's rows
// scroll under its head, thin and in the hairlines' colours, and the stage
// scrolls its own content; neither makes the page taller. The open row stands
// in the middle of the list on load, as far as its ends allow, in a browser that
// has scroll-initial-target: a row of the open page is the target, never a
// section's link. What the pixels do is the bead's screenshots.
func TestListAndStageScrollApart(t *testing.T) {
	t.Parallel()
	// A declaration stands at the start of its rule or after a space or a
	// semicolon, so that height does not match min-height.
	declares := func(rule, decl string) bool {
		return regexp.MustCompile(`(?:^|[\s;])` + regexp.QuoteMeta(decl)).MatchString(rule)
	}
	for selector, wants := range map[string][]string{
		".wv-list": {"display: flex;", "flex-direction: column;", "min-height: 0;", "overflow: hidden;"},
		".wv-list__rows": {"flex: 1;", "min-height: 0;", "overflow-y: auto;", "scrollbar-width: thin;",
			"scrollbar-color: var(--hairline-strong) transparent;"},
		".wv-list__head": {"flex: none;"},
		".wv-stage":      {"min-height: 0;", "min-width: 0;", "overflow: auto;", "padding: 44px 0 40px;"},
	} {
		rule := cssRule(t, selector)
		for _, want := range wants {
			if !declares(rule, want) {
				t.Errorf("%s lacks %s: %s", selector, want, rule)
			}
		}
	}
	open := cssRule(t, `.wv-list .wv-row[aria-current="page"]`)
	for _, want := range []string{"scroll-initial-target: nearest;", "scroll-snap-align: center;"} {
		if !declares(open, want) {
			t.Errorf("the list's open row lacks %s: %s", want, open)
		}
	}
	// The one rule is the only use of the property beyond it.
	if n := strings.Count(string(readAsset(t, "yard.css")), "scroll-initial-target:"); n != 1 {
		t.Errorf("scroll-initial-target stands %d times, want the open row's rule alone", n)
	}
}

// The overview's weight: the answer is the only large line, in the kit's role
// for it; the page's words are the proportional face and its machine words
// (ids, times, commands) stay monospace; a feed line runs one line with an
// ellipsis on a desktop. What the pixels do is the bead's screenshot.
func TestHomeWeight(t *testing.T) {
	t.Parallel()
	css := string(readAsset(t, "yard.css"))
	for _, want := range []string{"--wv-answer: 600 32px/38px var(--sans);", "--wv-answer: 600 26px/32px var(--sans);"} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks the answer's role %s", want)
		}
	}
	if answer := cssRule(t, ".wv-answer"); !strings.Contains(answer, "font: var(--wv-answer);") {
		t.Errorf("the answer is not in its role: %s", answer)
	}
	// A dot stands before the answer where something waits, and none where
	// nothing does.
	if dot := cssRule(t, ".wv-answer::before"); !strings.Contains(dot, "background: var(--accent);") {
		t.Errorf("the answer's dot is not the accent's: %s", dot)
	}
	if clear := cssRule(t, ".wv-answer--clear::before"); !strings.Contains(clear, "display: none;") {
		t.Errorf("a clear answer keeps its dot: %s", clear)
	}
	if page := cssRule(t, ".wv-page"); !strings.Contains(page, "font: 400 14px/20px var(--sans);") {
		t.Errorf("the overview's words are not sans: %s", page)
	}
	for _, selector := range []string{".wv-id", ".wv-entry__at", ".wv-hour"} {
		if rule := cssRule(t, selector); !strings.Contains(rule, "var(--mono)") {
			t.Errorf("%s is not monospace: %s", selector, rule)
		}
	}
	line := cssRule(t, ".wv-entry__body p")
	for _, want := range []string{"overflow: hidden;", "text-overflow: ellipsis;", "white-space: nowrap;"} {
		if !strings.Contains(line, want) {
			t.Errorf(".wv-entry__body p lacks %s: %s", want, line)
		}
	}
	// No new face: every font-family names a token.
	for _, m := range regexp.MustCompile(`(?m)font-family:\s*([^;]+);`).FindAllStringSubmatch(css, -1) {
		if m[1] != "var(--sans)" && m[1] != "var(--mono)" {
			t.Errorf("a face outside the tokens: font-family: %s", m[1])
		}
	}
}

// mediaCSS is the bodies of the stylesheet's @media blocks for query,
// joined: what the window gets at that width.
func mediaCSS(t *testing.T, css, query string) string {
	t.Helper()
	var out strings.Builder
	rest := css
	for {
		_, after, ok := strings.Cut(rest, "@media ("+query+") {")
		if !ok {
			break
		}
		depth := 1
		i := 0
		for ; i < len(after); i++ {
			switch after[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if depth != 0 {
			t.Fatalf("the %s block never closes", query)
		}
		out.WriteString(after[:i])
		rest = after[i+1:]
	}
	if out.Len() == 0 {
		t.Fatalf("the stylesheet has no %s block", query)
	}
	return out.String()
}

// The tables of Settings are one layout at every width: a row to each route,
// peer or group with a hairline under it, its header in quiet words and no
// capitals, and what does not fit scrolls sideways inside its own block, not
// the page. A group's config folds under its row, the row's rule moving to the
// fold's foot. The sheet's setting row is a label, its state and its button.
// What the pixels do is the bead's screenshot.
func TestSettingsTables(t *testing.T) {
	t.Parallel()
	css := string(readAsset(t, "yard.css"))
	for selector, wants := range map[string][]string{
		".wv-scrollx":                            {"overflow-x: auto;"},
		".wv-table th":                           {"color: var(--dim);", "white-space: nowrap;"},
		".wv-table td":                           {"border-bottom: 1px solid var(--hairline);"},
		".wv-table tr:has(+ .wv-table__fold) td": {"border-bottom: 0;"},
		".wv-setting":                            {"display: flex;", "justify-content: space-between;", "border-bottom: 1px solid var(--hairline);"},
		".wv-state--on":                          {"color: var(--ink);"},
		".wv-lede":                               {"max-width: 68ch;", "color: var(--dim);"},
		".wv-meter":                              {"width: 120px;", "height: 6px;"},
	} {
		rule := cssRule(t, selector)
		for _, want := range wants {
			if !strings.Contains(rule, want) {
				t.Errorf("%s lacks %s: %s", selector, want, rule)
			}
		}
	}
	if strings.Contains(cssRule(t, ".wv-table th"), "uppercase") {
		t.Error("a table's header is set in capitals")
	}
	// On a phone the words of a setting take the row's width and its state and
	// button stand under them, where beside them the words would be squeezed to
	// a column and the button's label broken over two lines.
	phone := mediaCSS(t, css, "max-width: 899px")
	for _, want := range []string{".wv-setting { flex-wrap: wrap;", ".wv-setting__text { flex: 1 1 100%;"} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
	// The stacked cards a narrow window had in place of the routes table are
	// gone with the rest of the old pages' rules.
	for _, gone := range []string{".routes", ".route-id", ".item-head", ".facts", ".figures", ".peers", ".talk-head", ".letter ", ".compose "} {
		if strings.Contains(css, gone) {
			t.Errorf("the stylesheet still names %s", gone)
		}
	}
}

// Under 900 px the stage is the page and the list is a drawer: the rail is a
// bar of four tiles at the foot, 48 high, the yard's chip standing in the
// stage's top bar; the drawer comes from the left over a scrim by :target and
// :has(), with no script, and the targets are 48 high. There is no More. What
// the pixels do is the bead's screenshots.
func TestPhoneWindow(t *testing.T) {
	t.Parallel()
	css := string(readAsset(t, "yard.css"))
	phone := mediaCSS(t, css, "max-width: 899px")
	for _, want := range []string{
		// The grid is one column over the bar.
		".wv-shell { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr) auto; }",
		".wv-stage { grid-row: 1; padding-top: 0; }",
		// The rail is the bar: a row of the tiles, without the chip and the
		// foot's spacer; Settings has its own tile.
		".wv-rail { grid-row: 2; flex-direction: row; justify-content: space-around; align-items: stretch; height: var(--wv-bar);",
		".wv-rail .wv-chip, .wv-rail__foot { display: none; }",
		".wv-tile, .wv-rail > .wv-chip + .wv-tile { flex: 1; width: auto; max-width: 96px; min-height: 48px; margin: 0; justify-content: center; }",
		// The top bar holds the chip, the mode's name and its count, and
		// the link that opens the drawer, 48 high.
		".wv-top { display: flex; align-items: center;",
		".wv-top__list { display: flex; align-items: center; gap: 2px; min-height: 48px;",
		// Closed, the drawer is hidden as well as moved, so a keyboard
		// does not tab into rows it cannot see.
		"transform: translateX(-102%); visibility: hidden;",
		// Open by the marker, with the scrim behind it.
		".wv-shell:has(#drawer:target) .wv-list { transform: none; visibility: visible; transition-delay: 0s; box-shadow: var(--shadow-popover); }",
		".wv-shell:has(#drawer:target) .wv-scrim { display: block; position: fixed; z-index: 10; inset: 0 0 var(--wv-bar) 0; background: var(--shade); }",
		".wv-list__close { display: flex; align-items: center; min-height: 48px;",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
	// The drawer is 340 wide, or 88% of a window that is narrower.
	if !strings.Contains(phone, "width: min(var(--wv-list), 88vw);") {
		t.Error("the drawer is not 340 wide at most and 88% of the window")
	}
	// A row, a section's head and the type step up to the phone's.
	for _, want := range []string{".wv-row { min-height: 48px; padding: 10px; }", ".wv-sect > summary { min-height: 48px; }",
		".wv-row__name { font-size: 16px; line-height: 22px; }", "--wv-title: 600 22px/28px var(--sans);"} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
	// On a wider window the top bar, the scrim and the close link are not
	// drawn, and the marker is no box.
	if gone := cssRule(t, ".wv-live, .wv-flag, .wv-top, .wv-scrim"); !strings.Contains(gone, "display: none;") {
		t.Errorf("the phone's parts stand on a wide window: %s", gone)
	}
	if closer := cssRule(t, ".wv-list__close"); !strings.Contains(closer, "display: none;") {
		t.Errorf("the close link stands on a wide window: %s", closer)
	}
	if strings.Contains(css, ".more") || strings.Contains(css, "More") {
		t.Errorf("the stylesheet still has the phone's More")
	}
}

// On a phone a feed line wraps, its title in full, where on a desktop it is one
// line ending in an ellipsis; a stage's hour heads and entries keep their
// measures. What the pixels do is the bead's screenshots.
func TestFeedRowOnPhone(t *testing.T) {
	t.Parallel()
	text := string(readAsset(t, "yard.css"))
	phone := mediaCSS(t, text, "max-width: 899px")
	for _, want := range []string{
		".wv-entry { gap: 10px; }",
		".wv-entry__body p { white-space: normal; }",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
	// The phone's rule comes after the desktop's: at the same specificity the
	// later one wins, so the line would stay cut on a phone otherwise.
	if wide, narrow := strings.Index(text, ".wv-entry__body p { white-space: nowrap;"), strings.Index(text, ".wv-entry__body p { white-space: normal; }"); wide < 0 || narrow < wide {
		t.Error("the phone's feed line must come after the wide one")
	}
	// The empty sentence shows only where it is first, so an entry the stream
	// brings hides it.
	none := cssRule(t, ".wv-feed__none:not(:first-child)")
	if !strings.Contains(none, "display: none;") {
		t.Errorf("an entry does not hide the empty sentence: %s", none)
	}
}

// On a phone the ask card's targets are 48 px, its field fills its row and the
// buttons share the next, and its age drops under its head's words. On a
// desktop the answer row is one line of a field and buttons. What the pixels do
// is the bead's screenshot.
func TestAskRowOnPhone(t *testing.T) {
	t.Parallel()
	text := string(readAsset(t, "yard.css"))
	phone := mediaCSS(t, text, "max-width: 899px")
	for _, want := range []string{
		".wv-field, .wv-btn { height: 48px; line-height: 46px; }",
		".wv-ask__row .wv-field { flex-basis: 100%; }",
		".wv-ask__row .wv-btn { flex: 1; padding: 0 8px; }",
		".wv-ask__row button.wv-btn { height: auto; min-height: 48px; line-height: 1.25; padding: 8px; }",
		".wv-ask__row a.wv-btn { white-space: nowrap; }",
		".wv-ask__age { flex-basis: 100%; margin: 0; padding-left: 18px; }",
		".wv-opt { padding: 10px 12px; }",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
	row := cssRule(t, ".wv-ask__row")
	for _, want := range []string{"display: flex;", "flex-wrap: wrap;"} {
		if !strings.Contains(row, want) {
			t.Errorf(".wv-ask__row lacks %s: %s", want, row)
		}
	}
	// A field is as big as a phone's, which would enlarge the page on focus
	// otherwise, and the card's controls name the stylesheet's colours: a
	// button takes none of the browser's own.
	if field := cssRule(t, ".wv-field"); !strings.Contains(field, "font: 400 14px/20px var(--sans);") || !strings.Contains(field, "color: var(--ink);") {
		t.Errorf(".wv-field is not in the page's face and ink: %s", field)
	}
	for _, selector := range []string{".wv-btn", ".wv-opt"} {
		if rule := cssRule(t, selector); !strings.Contains(rule, "color: var(--ink);") {
			t.Errorf("%s takes the browser's colour: %s", selector, rule)
		}
	}
	// An option that cannot be picked is a row to read, with no pointer.
	if rule := cssRule(t, "div.wv-opt"); !strings.Contains(rule, "cursor: default;") {
		t.Errorf("an option row that is no button has a pointer: %s", rule)
	}
}

// The conversation reads as a messaging app: the stage fills the window, the
// head stays above the letters in a narrow window and the composer below, and
// the letters scroll between them, bottom-up, so the newest is in view on open
// with the form under it. A wide window leaves the head to a screen reader,
// since Crew's list beside the stage names the party. A delegated run's
// record, which has no composer, is not a pane: it scrolls as any page does.
// What the pixels do is the bead's screenshot.
func TestTalkLayout(t *testing.T) {
	t.Parallel()
	stage := cssRule(t, ".wv-stage:has(.wv-compose)")
	for _, want := range []string{"display: flex;", "flex-direction: column;", "overflow: hidden;"} {
		if !strings.Contains(stage, want) {
			t.Errorf(".wv-stage:has(.wv-compose) lacks %s: %s", want, stage)
		}
	}
	talk := cssRule(t, ".wv-talk:has(.wv-compose)")
	for _, want := range []string{"display: flex;", "flex-direction: column;", "flex: 1;", "min-height: 0;"} {
		if !strings.Contains(talk, want) {
			t.Errorf(".wv-talk:has(.wv-compose) lacks %s: %s", want, talk)
		}
	}
	letters := cssRule(t, ".wv-talk:has(.wv-compose) .wv-letters")
	for _, want := range []string{"flex-direction: column-reverse;", "flex: 1;", "min-height: 0;", "overflow-y: auto;"} {
		if !strings.Contains(letters, want) {
			t.Errorf("the conversation's pane lacks %s: %s", want, letters)
		}
	}
	blocks := cssRule(t, ".wv-talk:has(.wv-compose) .wv-letters > *")
	if !strings.Contains(blocks, "flex: none;") {
		t.Errorf("the pane's blocks lack flex: none;: %s", blocks)
	}
	// The composer is a framed box on the raised ground, with a hairline and
	// the card radius; its line has no border of its own but a rule, and the
	// frame shows the focus. The Send button is the one filled button.
	for selector, wants := range map[string][]string{
		".wv-compose":                     {"background: var(--panel);", "border: 1px solid var(--hairline);", "border-radius: var(--radius-lg);"},
		".wv-compose textarea":            {"border: 0;", "border-bottom: 1px solid var(--hairline);"},
		".wv-compose:focus-within":        {"border-color: var(--accent);"},
		".wv-btn--primary":                {"background: var(--pill);", "color: var(--on-accent);"},
		".wv-record":                      {"background: var(--panel);", "border-radius: var(--radius-lg);"},
		".wv-record code":                 {"font: 400 12.5px/18px var(--mono);", "color: var(--ink);"},
		".wv-letter":                      {"display: grid;", "grid-template-columns: 56px minmax(0, 1fr);"},
		".wv-day::before, .wv-day::after": {"flex: 1;", "height: 1px;"},
	} {
		rule := cssRule(t, selector)
		for _, want := range wants {
			if !strings.Contains(rule, want) {
				t.Errorf("%s lacks %s: %s", selector, want, rule)
			}
		}
	}
	// On a phone the stage takes what the bar leaves, with no top padding: the
	// top bar stands there instead, and the composer's line keeps the 16 pixels
	// a phone does not enlarge on focus.
	phone := mediaCSS(t, string(readAsset(t, "yard.css")), "max-width: 899px")
	for _, want := range []string{".wv-stage:has(.wv-compose) { padding-top: 0; }", ".wv-compose textarea { font-size: 16px;"} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
	// From 900 px the head is clipped as a letter's sender is, to a screen
	// reader alone, and never display: none, which would take the h1 that names
	// the page out of the outline. Only a wide window clips it: on a phone the
	// list is a drawer, closed, and the head is what names the party.
	wide := mediaCSS(t, string(readAsset(t, "yard.css")), "min-width: 900px")
	head := regexp.MustCompile(`\.wv-talk > \.wv-head \{([^}]*)\}`).FindStringSubmatch(wide)
	if head == nil {
		t.Fatal("the wide window's rules have no rule for the conversation's head")
	}
	for _, want := range []string{"position: absolute;", "width: 1px;", "height: 1px;", "overflow: hidden;", "clip-path: inset(50%);", "white-space: nowrap;"} {
		if !strings.Contains(head[1], want) {
			t.Errorf("the wide window's head lacks %s: %s", want, head[1])
		}
	}
	if strings.Contains(head[1], "display: none") {
		t.Errorf("the wide window's head is out of the page's outline: %s", head[1])
	}
}

// The files behind a summary card read on a phone: the pager is a row of
// controls that wraps instead of widening the page, each as high as a phone's
// targets; a text keeps its lines and pans inside its block; a file's name,
// a path with no space to break at, breaks where it would run past the
// window; and a card's sentence that opens a file reads as a link. A
// list page's chips keep one scrolling line each on a phone, each pill as high
// as a phone's targets, and a preview's buttons wrap, the line that stands in
// Open's place taking a line of its own. What the pixels do is the bead's
// screenshot.
func TestFilePagesOnPhone(t *testing.T) {
	t.Parallel()
	for selector, wants := range map[string][]string{
		".wv-pager":                {"display: flex;", "flex-wrap: wrap;"},
		".wv-file":                 {"white-space: pre;", "overflow-x: auto;"},
		".wv-title":                {"overflow-wrap: anywhere;"},
		".opens":                   {"text-decoration: underline;"},
		".wv-ask__row > .wv-faint": {"flex: 1 0 100%;", "overflow-wrap: anywhere;"},
		// A chip is a pill with its number, the current one filled.
		".wv-chips p":            {"display: flex;", "flex-wrap: wrap;"},
		".wv-pill":               {"min-height: 30px;", "border-radius: var(--radius-full);"},
		".wv-pill[aria-current]": {"background: var(--surface-pressed);"},
	} {
		rule := cssRule(t, selector)
		for _, want := range wants {
			if !strings.Contains(rule, want) {
				t.Errorf("%s lacks %s: %s", selector, want, rule)
			}
		}
	}
	phone := mediaCSS(t, string(readAsset(t, "yard.css")), "max-width: 899px")
	for _, want := range []string{
		".wv-pager a, .wv-pager span { display: flex; align-items: center; min-height: 48px;",
		".wv-back { min-height: 48px;",
		".wv-fold > summary { min-height: 48px; }",
		".wv-pill { min-height: 48px; flex: none; }",
		".wv-chips p { flex-wrap: nowrap; overflow-x: auto;",
		".wv-chips .wv-chips__label { flex: none; width: auto; padding-right: 4px; }",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone rules lack %s", want)
		}
	}
}

// The light look: the same tokens with the design system's light values for a
// visitor whose system asks for light, with dark staying the default. The
// window's surfaces and its rust-as-text follow, and the list stands on the
// ground in light where it stands on the raised surface in dark.
func TestLightTheme(t *testing.T) {
	t.Parallel()
	text := string(readAsset(t, "yard.css"))
	if !strings.Contains(text, "@media (prefers-color-scheme: light) {") {
		t.Fatal("the stylesheet has no light theme")
	}
	_, light, _ := strings.Cut(text, "@media (prefers-color-scheme: light) {")
	for _, want := range []string{
		"color-scheme: light;",
		"--ground: #F7F7F6;",          // --surface-app
		"--panel: #FFFFFF;",           // --surface-raised
		"--hairline: #E2E2DF;",        // --border-subtle
		"--hairline-strong: #CFCFCB;", // --border-strong
		"--ink: #161718;",             // --text-primary
		"--dim: #5B5E64;",             // --text-secondary
		"--faint: #8E9196;",           // --text-disabled
		"--running: #1F9A60;",         // --status-running
		"--held: #B3382C;",            // --status-error
		"--link: #1A56C8;",            // --text-link
		"--surface-sunken: #EDEDEB;",
		"--surface-hover: #EDEDEB;",
		"--surface-pressed: #E2E2DF;",
		"--border-hover: #B3B3AE;",
		"--text-accent: #8F3C19;", // --yard-700
		"--chip: #B54E22;",        // --yard-600
		"--chip-ink: #FFFFFF;",
		".wv-list { background: var(--ground); }",
	} {
		if !strings.Contains(light, want) {
			t.Errorf("the light theme lacks %s", want)
		}
	}
	// Dark stays: the base tokens keep their values.
	dark, _, _ := strings.Cut(text, "@media (prefers-color-scheme: light) {")
	for _, want := range []string{
		"color-scheme: dark light;",
		"--ground: #161718;",
		"--panel: #1c1d1f;",
		"--hairline: #27282b;",
		"--hairline-strong: #333538;",
		"--ink: #e9e9ea;",
		"--dim: #abadb0;",
		"--faint: #808286;",
		"--running: #4cc38a;",
		"--held: #e5675a;",
		"--link: #7aa2f7;",
	} {
		if !strings.Contains(dark, want) {
			t.Errorf("the dark tokens lack %s", want)
		}
	}
}
