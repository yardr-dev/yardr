package web

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"yardr.dev/yardr/internal/store"
)

// The list is the second column of the window: what stands in the open mode,
// as rows, with the open row marked. It is in every page's HTML and never
// fetched by script. It is built from what the shell has read for the page
// already, the overview, the line and the acting user's inbox, so a page
// costs no read for it, but Crew's, which reads the crew and the users where
// the page is not the crew's own (docs/web.md, The window).

// place is where a page stands in the window: its mode, and which row of the
// mode's list it opens.
type place struct {
	// mode is decide, lines, crew or settings; "" on an error page, which
	// opens none.
	mode string
	// path is the request's path.
	path string
	// bead is the bead a Decide page is about, "" on any other.
	bead string
	// depot and flow are the depot and the flow of it a Lines page stands on.
	depot, flow string
	// with is the party of the conversation a Crew page opens, "" where the
	// page does not name one.
	with string
}

// listPart is one piece of a row's line: words, an id in code, or words in a
// colour (wait for rust, fault for red, faint for the quiet tier).
type listPart struct {
	Text  string
	Class string
	Code  bool
}

// listLine is one of a row's quiet lines, its parts set apart by a middle dot.
type listLine []listPart

// listRow is one row of the list: a place, a bead, a depot's flow, a member
// of the crew. The dot column is always there, empty where the row has no dot.
type listRow struct {
	Href string
	// Dot is the row's dot: wait (rust), run (green), idle (a ring), fault
	// (red), or "".
	Dot  string
	Name string
	// Lines are the row's quiet lines under its name.
	Lines []listLine
	// End is the count or the age at the row's end, in rust where EndWait.
	End     string
	EndWait bool
	// Unread sets the name in the heavier weight: mail waits from this party.
	Unread bool
	// Current marks the row the page opens.
	Current bool
}

// listSection is a group of rows: a fold with its title and its count, or
// bare rows (a mode's places) with no head.
type listSection struct {
	Title string
	Count string
	// Href is where a title that is a link goes: a depot's section opens the
	// depot's page.
	Href string
	// Current marks the page the title links to, as a row is.
	Current bool
	// Bare is a list of rows with no head and no fold.
	Bare bool
	// Fold is a section that stands closed until opened, unless it holds the
	// open row.
	Fold bool
	// Empty is the sentence a section with no rows says, "" where it says none.
	Empty string
	// Leaves is whether every row leaves the mode: the swap then takes the
	// whole list, not its rows, so the new mode's list starts at its top.
	Leaves bool
	Rows   []listRow
}

// listView is the open mode's list: the head and the sections under it.
type listView struct {
	Title string
	// Count is the head's words at the right (3 wait for you, 11 depots, 34
	// open, 2 running), "" where it says none; Wait sets it in rust.
	Count string
	Wait  bool
	// Sub is the quiet line under the head: Lines' open count per stage.
	Sub      string
	Sections []listSection
}

// markBead marks the first row of the sections that opens the bead's page and
// opens the fold it stands in. A bead that stands in none of them marks none.
func markBead(secs []listSection, bead string) {
	if bead == "" {
		return
	}
	href := beadHref(bead)
	for i := range secs {
		for j := range secs[i].Rows {
			if secs[i].Rows[j].Href == href {
				secs[i].Rows[j].Current = true
				secs[i].Fold = false
				return
			}
		}
	}
}

func beadHref(id string) string { return "/beads/" + url.PathEscape(id) }

// countWords is n and a noun in words: 1 depot, 11 depots.
func countWords(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// askedWhere says where an ask waits for the viewer: the depots, and the
// flows ("depot/flow"), of the beads that carry one. A bead of no depot or of
// no flow lights none.
func askedWhere(ov *Home, l *Line) (depots, flows map[string]bool) {
	depots, flows = map[string]bool{}, map[string]bool{}
	byID := map[string]*store.Bead{}
	for i := range ov.yard.beads {
		byID[ov.yard.beads[i].ID] = &ov.yard.beads[i]
	}
	for _, ws := range [][]Waiting{l.Waits, l.Parked} {
		for _, w := range ws {
			b := byID[w.Bead]
			if !w.HasAsk() || b == nil || b.Depot == nil {
				continue
			}
			depots[*b.Depot] = true
			for i := range ov.Depots {
				if d := &ov.Depots[i]; d.Name == *b.Depot {
					if j := d.flowIndex(b.Type); j >= 0 {
						flows[d.Name+"/"+d.Flows[j].Name] = true
					}
					break
				}
			}
		}
	}
	return depots, flows
}

// open is the flow's open beads: the stages it holds one in, each with its
// count in the flow's order ("backlog 3"), and the total.
func (f *HomeFlow) open() (line listLine, total int) {
	for _, sc := range f.Stages {
		if sc.Open > 0 {
			line = append(line, listPart{Text: fmt.Sprintf("%s %d", sc.Stage, sc.Open)})
			total += sc.Open
		}
	}
	return line, total
}

// waitRow is the bead's wait, as a Decide row says it.
//
// An ask: the question, then the bead's id and title, then who asked. A
// fault: the bead's title, the fault in red, the note that came with it. A
// bead with both stands once, as a fault does, the question where the note
// would stand. A parked bead: the title, its id and stage, the reason.
func waitRow(w Waiting, parked bool) listRow {
	row := listRow{Href: beadHref(w.Bead), End: w.Age()}
	if !parked {
		row.Dot = "wait"
	}
	var fault *Reason
	for i := range w.Why {
		if r := &w.Why[i]; r.Kind != "ask" && r.Kind != "manual" {
			fault = r
			break
		}
	}
	ask := w.FirstAsk()
	ident := listLine{{Text: w.Bead, Code: true}, {Text: w.Title}}
	switch {
	case ask != nil && fault != nil:
		row.Name = w.Title
		row.Lines = []listLine{{{Text: fault.Text, Class: "fault"}}, {{Text: ask.Text}}}
	case ask != nil:
		row.Name = ask.Text
		row.Lines = []listLine{ident}
		if ask.Asker != "" {
			row.Lines = append(row.Lines, listLine{{Text: "asked by " + ask.Asker}})
		}
	case fault != nil:
		row.Name = w.Title
		row.Lines = []listLine{{{Text: fault.Text, Class: "fault"}}}
		if fault.Note != "" {
			row.Lines = append(row.Lines, listLine{{Text: fault.Note}})
		}
	default:
		row.Name = w.Title
		row.Lines = []listLine{{{Text: w.Bead, Code: true}, {Text: w.Stage}}}
		if why := w.Reasons(); why != "" {
			row.Lines = append(row.Lines, listLine{{Text: why}})
		}
	}
	row.EndWait = ask != nil
	return row
}

// decideList is Decide's list: what waits for the person, what is parked,
// what runs, the depots, and on /line what runs late. waits are the line's
// waits as attention has them, in its order. The beads running past their
// usual time are the line's Late, which only /line reads: they stand in a
// section of their own there, and a running bead's row carries its late line
// where they are read.
func decideList(pl place, ov *Home, l *Line, waits []Waiting) listView {
	out := listView{Title: "Decide"}
	if n := len(waits); n > 0 {
		out.Count, out.Wait = waitWords(n), true
	}
	askedDepots, _ := askedWhere(ov, l)
	inWaits := map[string]bool{}
	wait := listSection{Title: "Waits for you", Count: nonZero(len(waits)), Empty: "Nothing waits for you."}
	asking := map[string]bool{}
	blocked := map[string]bool{}
	for _, w := range waits {
		inWaits[w.Bead] = true
		wait.Rows = append(wait.Rows, waitRow(w, false))
	}
	for _, w := range append(slices.Clone(l.Waits), l.Parked...) {
		if w.HasAsk() {
			asking[w.Bead] = true
		}
		if slices.ContainsFunc(w.Why, func(r Reason) bool { return r.Kind == "blocked" }) {
			blocked[w.Bead] = true
		}
	}
	out.Sections = append(out.Sections, wait)
	// The parked beads that stand outside the waits: where they start, and
	// those the reader answered.
	var parked listSection
	for _, w := range l.Parked {
		if !inWaits[w.Bead] {
			parked.Rows = append(parked.Rows, waitRow(w, true))
		}
	}
	if len(parked.Rows) > 0 {
		parked.Title, parked.Count, parked.Fold = "Parked", strconv.Itoa(len(parked.Rows)), true
		out.Sections = append(out.Sections, parked)
	}
	// What runs now, the oldest first: a manual group's claim runs nothing.
	byBead := map[string]*store.Bead{}
	for i := range ov.yard.beads {
		byBead[ov.yard.beads[i].ID] = &ov.yard.beads[i]
	}
	lateBy := map[string]LateRow{}
	for _, r := range l.Late {
		lateBy[r.Bead] = r
	}
	running := listSection{Title: "Running"}
	sessions := slices.Clone(ov.yard.sessions)
	slices.SortStableFunc(sessions, func(a, b store.Session) int { return a.StartedAt.Compare(b.StartedAt) })
	for _, sess := range sessions {
		if isManual(sess) {
			continue
		}
		row := listRow{Href: beadHref(sess.Bead), Dot: "run", Name: sess.Bead, End: age(time.Since(sess.StartedAt))}
		if b := byBead[sess.Bead]; b != nil {
			row.Name = b.Title
		}
		if blocked[sess.Bead] {
			row.Dot = "fault"
		}
		row.Lines = []listLine{{{Text: sess.Bead, Code: true}, {Text: sess.Stage}, {Text: sess.Group}}}
		switch late, isLate := lateBy[sess.Bead]; {
		case asking[sess.Bead]:
			row.Lines = append(row.Lines, listLine{{Text: "waits on your answer", Class: "wait"}})
		case isLate && late.Late != nil:
			row.Lines = append(row.Lines, listLine{{Text: late.Late.Line}})
		}
		running.Rows = append(running.Rows, row)
	}
	if len(running.Rows) > 0 {
		running.Count = fmt.Sprintf("%d working", len(running.Rows))
		out.Sections = append(out.Sections, running)
	}
	if pl.path == "/line" {
		late := listSection{Title: "Late", Count: nonZero(len(l.Late)), Empty: "Nothing is late."}
		for _, r := range l.Late {
			row := listRow{Href: beadHref(r.Bead), Name: r.Title, End: age(time.Since(r.Since)),
				Lines: []listLine{{{Text: r.Bead, Code: true}, {Text: r.Stage}}}}
			if r.Late != nil {
				row.Lines = append(row.Lines, listLine{{Text: r.Late.Line}})
			}
			late.Rows = append(late.Rows, row)
		}
		out.Sections = append(out.Sections, late)
	}
	depots := listSection{Title: "Depots", Leaves: true, Empty: "This yard has no depot yet."}
	open := 0
	for _, d := range busiest(ov) {
		open += d.Open
		row := listRow{Href: "/depots/" + url.PathEscape(d.Name), Name: d.Name, End: nonZero(d.Open)}
		if askedDepots[d.Name] {
			row.Dot = "wait"
		}
		var meta listLine
		for j := range d.Flows {
			f := &d.Flows[j]
			if _, n := f.open(); n > 0 {
				meta = append(meta, listPart{Text: fmt.Sprintf("%s %d", f.Name, n)})
			} else {
				meta = append(meta, listPart{Text: f.Name, Class: "faint"})
			}
		}
		if len(meta) > 0 {
			row.Lines = []listLine{meta}
		}
		depots.Rows = append(depots.Rows, row)
	}
	if len(depots.Rows) > 0 {
		depots.Count = fmt.Sprintf("%d open", open)
	}
	out.Sections = append(out.Sections, depots)
	markBead(out.Sections, pl.bead)
	return out
}

// busiest are the overview's depots, the one with the most open beads first
// and the yard's own order between depots with as many: the order the lists
// draw them in, where the work is first.
func busiest(ov *Home) []*HomeDepot {
	out := make([]*HomeDepot, len(ov.Depots))
	for i := range ov.Depots {
		out[i] = &ov.Depots[i]
	}
	slices.SortStableFunc(out, func(a, b *HomeDepot) int { return b.Open - a.Open })
	return out
}

// nonZero is n as a count's words, "" at zero: a row's end and a section's
// count are left out where they would say none.
func nonZero(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// sortedStages are the stages with the most open beads first, the line's order
// between those with as many.
func sortedStages(in []StageCount) []StageCount {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b StageCount) int { return b.Open - a.Open })
	return out
}

// linesList is Lines' list: the six places no depot or flow opens, then every
// depot as a section that opens its page, and its flows as rows.
func linesList(pl place, ov *Home, l *Line) listView {
	out := listView{Title: "Lines", Count: countWords(len(ov.Depots), "depot", "depots") + ", " + strconv.Itoa(l.Open) + " open"}
	var stages []string
	for _, sc := range sortedStages(l.Stages) {
		if sc.Open > 0 {
			stages = append(stages, fmt.Sprintf("%s %d", sc.Stage, sc.Open))
		}
	}
	out.Sub = strings.Join(stages, " · ")
	flowsN := 0
	for i := range ov.Depots {
		flowsN += len(ov.Depots[i].Flows)
	}
	run := ""
	if l.Sessions > 0 {
		run = "run"
	}
	places := listSection{Bare: true, Rows: []listRow{
		{Href: "/flows", Name: "Flows", Lines: []listLine{{{Text: "each flow's stages, and who works them"}}}, End: nonZero(flowsN)},
		{Href: "/depots", Name: "Depots", Lines: []listLine{{{Text: "the repositories and directories beads are worked in"}}}, End: nonZero(len(ov.Depots))},
		{Href: "/beads", Name: "Beads", Lines: []listLine{{{Text: "open, the newest change first"}}}, End: nonZero(l.Open)},
		{Href: "/sessions", Dot: run, Name: "Sessions", Lines: []listLine{{{Text: "running now"}}}, End: nonZero(l.Sessions)},
		{Href: "/previews", Name: "Previews", Lines: []listLine{{{Text: "proposed, approved and running"}}}},
		{Href: "/deps", Name: "Deps", Lines: []listLine{{{Text: "the edges with an open end"}}}},
	}}
	for i := range places.Rows {
		places.Rows[i].Current = places.Rows[i].Href == pl.path
	}
	out.Sections = append(out.Sections, places)
	_, askedFlows := askedWhere(ov, l)
	for _, d := range busiest(ov) {
		sec := listSection{Title: d.Name, Href: "/depots/" + url.PathEscape(d.Name),
			Count: "none open", Current: pl.depot == d.Name && pl.flow == ""}
		if d.Open > 0 {
			sec.Count = fmt.Sprintf("%d open", d.Open)
		}
		for j := range d.Flows {
			f := &d.Flows[j]
			line, total := f.open()
			if total == 0 {
				line = listLine{{Text: "none open"}}
			}
			row := listRow{Href: sec.Href + "/" + url.PathEscape(f.Name), Name: f.Name, Lines: []listLine{line}, End: nonZero(total),
				Current: pl.depot == d.Name && pl.flow == f.Name}
			if askedFlows[d.Name+"/"+f.Name] {
				row.Dot = "wait"
			}
			sec.Rows = append(sec.Rows, row)
		}
		out.Sections = append(out.Sections, sec)
	}
	return out
}

// settingsPage is one page of Settings' list: where it is and what it holds.
type settingsPage struct{ path, name, blurb string }

// settingsList is Settings' list: the pages by name, each with its blurb.
// Peers and Telemetry stand for the operator alone, as on the hub; Pair
// device for a view behind an auth gateway alone. Depots and Deps are Lines'
// rows. /pairing, a page in no row of the map, opens none.
func (s *Server) settingsList(ctx context.Context, pl place) listView {
	pages := []settingsPage{
		{"/config", "Config", "the pages below, and the yardmaster's switch"},
		{"/groups", "Groups", "who works a stage"},
		{"/routes", "Routes", "which group a stage sends its beads to, first match wins"},
	}
	operator := s.isOperator(ctx)
	if operator {
		pages = append(pages, settingsPage{"/peers", "Peers", peersLink.Blurb})
	}
	pages = append(pages, settingsPage{"/limits", "Limits", "what the subscriptions the yard burns still carry"})
	if operator {
		pages = append(pages, settingsPage{"/telemetry", "Telemetry", telemetryLink.Blurb})
	}
	if s.jwt != nil {
		pages = append(pages, settingsPage{"/pairing", "Pair device", "how a device is paired with this yard"})
	}
	sec := listSection{Bare: true}
	for _, p := range pages {
		current := pl.path == p.path || p.path == "/config" && strings.HasPrefix(pl.path, "/config/")
		sec.Rows = append(sec.Rows, listRow{Href: p.path, Name: p.name, Lines: []listLine{{{Text: p.blurb}}},
			Current: current && pl.path != "/pairing"})
	}
	return listView{Title: "Settings", Sections: []listSection{sec}}
}

// crewList is Crew's list: the crew, each with its agent and its state, and
// the people to talk with. unread are the parties the acting user has unread
// mail from, nil where the view acts as no user, which has no people to show
// and no mail to mark. with is the party whose row stands open.
func crewList(rows []CrewRow, people []store.User, as string, unread []string, with string) listView {
	running := 0
	crew := listSection{Title: "Crew", Empty: "This yard has no crew."}
	for _, r := range rows {
		if r.Running {
			running++
		}
		row := listRow{Href: talkHref(r.Name), Name: r.Name, Dot: r.listDot(), Lines: []listLine{{{Text: r.Agent}}, r.stateLine()},
			Current: r.Name == with}
		if slices.Contains(unread, r.Name) {
			row.End, row.EndWait, row.Unread = "unread mail", true, true
		}
		crew.Rows = append(crew.Rows, row)
	}
	out := listView{Title: "Crew"}
	if len(rows) > 0 {
		out.Count = fmt.Sprintf("%d running", running)
		crew.Count = out.Count
	}
	out.Sections = append(out.Sections, crew)
	if as != "" {
		sec := listSection{Title: "People"}
		for _, u := range people {
			if u.Name == as {
				continue
			}
			row := listRow{Href: talkHref(u.Name), Name: u.Name, Current: u.Name == with}
			if slices.Contains(unread, u.Name) {
				row.Dot, row.End, row.EndWait, row.Unread = "wait", "unread mail", true, true
			}
			sec.Rows = append(sec.Rows, row)
		}
		if len(sec.Rows) > 0 {
			sec.Count = strconv.Itoa(len(sec.Rows))
			out.Sections = append(out.Sections, sec)
		}
	}
	return out
}

func talkHref(with string) string { return "/talk?with=" + url.QueryEscape(with) }

// listDot is the member's dot: green for one that works, a ring for one that
// idles, red for one that is blocked or failed, none for one that is
// stopped.
func (r CrewRow) listDot() string {
	switch {
	case r.State == "failed", r.Running && r.Status == "blocked":
		return "fault"
	case r.Running && r.Status == "idle":
		return "idle"
	case r.Running:
		return "run"
	}
	return ""
}

// stateLine is the member's state as its row says it: working, idle or
// blocked with how long it has been up, or stopped or failed; then disabled,
// a restart asked for with its reason, and the member's own unread count.
func (r CrewRow) stateLine() listLine {
	var line listLine
	switch {
	case r.Running:
		word := r.Status
		if word == "" || word == "unknown" {
			word = "running"
		}
		part := listPart{Text: word}
		if r.Status == "blocked" {
			part.Class = "fault"
		}
		line = append(line, part)
		if r.Up != "" {
			line = append(line, listPart{Text: "up " + r.Up})
		}
	case r.State == "failed":
		line = append(line, listPart{Text: r.State, Class: "fault"})
	default:
		line = append(line, listPart{Text: r.State})
	}
	if !r.Enabled {
		line = append(line, listPart{Text: "disabled"})
	}
	if r.Restart != "" {
		line = append(line, listPart{Text: "restart asked: " + r.Restart})
	}
	if r.Unread > 0 {
		line = append(line, listPart{Text: fmt.Sprintf("%d unread", r.Unread)})
	}
	return line
}

// crewListFor is Crew's list for the page: the crew's rows (a handler's own
// where it has read them), the users where the view acts, and the party whose
// row stands open. The conversation a page opens without naming one is the
// one /talk opens, which is the yardmaster's, else the first member's.
func (s *Server) crewListFor(ctx context.Context, pl place, rows []CrewRow, inbox *store.Mailbox) (listView, error) {
	var err error
	if rows == nil {
		if rows, err = s.crewRows(ctx); err != nil {
			return listView{}, err
		}
	}
	as := s.acting(ctx)
	var users []store.User
	if as != "" {
		if users, err = s.st.Users(ctx); err != nil {
			return listView{}, err
		}
	}
	with := pl.with
	if with == "" && as != "" && (pl.path == "/talk" || pl.path == "/crew") {
		settings, err := s.st.Settings(ctx)
		if err != nil {
			return listView{}, err
		}
		members := make([]store.CrewMember, len(rows))
		for i := range rows {
			members[i] = rows[i].CrewMember
		}
		with = defaultTalkPeer(members, users, as, yardmasterName(settings))
	}
	var unread []string
	if inbox != nil {
		unread = inbox.From
	}
	return crewList(rows, users, as, unread, with), nil
}
