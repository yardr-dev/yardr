package web

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"yardr.dev/yardr/internal/store"
)

// What the bead's page says about itself, in the words its template sets out:
// the route strip's stops, the state beside the ids, the body's first
// paragraph and the one line each fold keeps of what it holds. They are
// methods and not template logic so that each rule is held by a test.

// Stop is one stop of the route strip: a stage of the bead's flow. The bead's
// own stop is Now, the ones before it Past.
type Stop struct {
	Name      string
	Past, Now bool
}

// stageOrder is a flow's stages as the flow's page draws its strip: the
// stages a bead can sit in, in the order the flow was written, then its
// terminals, which close the bead that enters them.
func stageOrder(f *store.Flow) []string {
	var open, terminal []string
	for _, st := range f.Stages {
		if f.IsTerminal(st) {
			terminal = append(terminal, st)
		} else {
			open = append(open, st)
		}
	}
	return append(open, terminal...)
}

// Stops are the route strip's stops, one per stage of the flow in its order.
// A bead no flow governs, or one whose stage its flow lacks, has its own
// stage as a last stop, so that the stage is always the strip's current one.
func (p BeadPage) Stops() []Stop {
	stages := p.Route
	i := slices.Index(stages, p.Bead.Stage)
	if i < 0 {
		stages = append(slices.Clone(stages), p.Bead.Stage)
		i = len(stages) - 1
	}
	out := make([]Stop, len(stages))
	for j, name := range stages {
		out[j] = Stop{Name: name, Past: j < i, Now: j == i}
	}
	return out
}

// State is one word of the meta line, with the class that sets its colour:
// wait in rust, fault in red, run in green.
type State struct {
	Text, Class string
}

// States are the words the meta line says after the bead's places. A closed
// bead says so and why. An open one says held first, then what stands with
// the person (it waits for them where it has an ask for them or a move they
// may make) or else that an agent's or a command's session works it.
func (p BeadPage) States() []State {
	if p.Bead.Status != "open" {
		text := p.Bead.Status
		if r := p.Bead.CloseReason; r != nil && *r != "" {
			text += ": " + *r
		}
		return []State{{Text: text}}
	}
	var out []State
	if p.Bead.Hold {
		out = append(out, State{Text: "held", Class: "fault"})
	}
	switch {
	case len(p.Asks) > 0 || len(p.Moves) > 0:
		out = append(out, State{Text: "waits for you", Class: "wait"})
	case p.Working:
		out = append(out, State{Text: "working", Class: "run"})
	}
	return out
}

// Opening is the bead's body as the page lays it out: its first paragraph,
// whether more follows it, and the line count of the whole.
type Opening struct {
	First string
	More  bool
	Lines int
}

// Opening reads the body, which the page draws as the words it was written
// in, with its lines kept: its first paragraph is the lines up to the first
// blank one, those before it skipped. More says that anything follows, which
// is what the fold below it, Read it whole, has to show.
func (p BeadPage) Opening() Opening {
	body := p.Bead.Body
	lines := strings.Split(strings.TrimRight(body, "\r\n"), "\n")
	out := Opening{Lines: len(lines)}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := start
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		end++
	}
	out.First = strings.Join(lines[start:end], "\n")
	for _, l := range lines[end:] {
		if strings.TrimSpace(l) != "" {
			out.More = true
			break
		}
	}
	return out
}

// Count says how many lines the body has, as the section's head and the fold
// that opens it whole say it.
func (o Opening) Count() string { return countWords(o.Lines, "line", "lines") }

// elementID is an id made of parts that are names from the yard, for an element
// that htmx finds by it: hx-preserve looks it up as the selector #id, which a
// slash or a dot would break (and a swap that throws is worse than one that
// keeps nothing), so each part keeps its letters, digits, hyphens and
// underscores and anything else is written as an underscore.
func elementID(kind string, parts ...string) string {
	safe := func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}
	id := kind
	for _, p := range parts {
		id += "-" + strings.Map(safe, p)
	}
	return id
}

// FoldID is the id of the page's fold name. A page that reads itself again at
// every event of the yard keeps the folds a person opened open by it, so it
// names the bead: another bead's folds are another page's.
func (p BeadPage) FoldID(name string) string { return elementID("fold", name, p.Bead.ID) }

// NoteID is the id of the send-on form's note field. A page that reads itself
// again keeps what is typed in the field (hx-preserve) by it; it names the
// stage as well as the bead, so a bead that has moved on is given a new field.
func (p BeadPage) NoteID() string { return elementID("note", p.Bead.ID, p.Bead.Stage) }

// NoteCount is how many notes the page draws in the Notes section: an answer
// an ask has taken in is drawn inside it.
func (p BeadPage) NoteCount() int {
	n := 0
	for _, note := range p.Notes {
		if !p.AskNotes.Folded(note.Seq) {
			n++
		}
	}
	return n
}

// Person is whether name is a user of the yard, whose notes the page sets in
// the heavier face: the people of the thread among the agents.
func (p BeadPage) Person(name string) bool { return p.People[name] }

// ShowsOutcomes says whether the Stage fold draws how work in the stage can
// end: where the bead is open and the viewer has no move to send it on with,
// the Send on form being the way to do it. Without outcomes it draws the next
// stages instead.
func (p BeadPage) ShowsOutcomes() bool {
	return p.Bead.Status == "open" && len(p.Moves) == 0 && len(p.Outcomes) > 0
}

// ShowsNext is whether the Stage fold draws the stages the bead may be moved
// to, as ShowsOutcomes says of the outcomes.
func (p BeadPage) ShowsNext() bool {
	return p.Bead.Status == "open" && len(p.Moves) == 0 && len(p.Outcomes) == 0 && len(p.Next) > 0
}

// HasStage is whether the page has a Stage fold: the stage's file, or how
// work in it can end, or where the bead may go next.
func (p BeadPage) HasStage() bool {
	return p.StageFile != nil || p.ShowsOutcomes() || p.ShowsNext()
}

// HasLinks is whether the page has a Linked beads fold.
func (p BeadPage) HasLinks() bool {
	return len(p.Children) > 0 || len(p.LedTo) > 0 || len(p.Deps) > 0
}

// FactsLine is what the Facts fold keeps in one line: the places and the
// times, the longest ids and paths left to the fold.
func (p BeadPage) FactsLine() string {
	var parts []string
	if p.Bead.Depot != nil {
		parts = append(parts, "depot "+*p.Bead.Depot)
	}
	if p.Flow != nil {
		parts = append(parts, fmt.Sprintf("flow %s (%s)", p.Flow.Name, p.Flow.Source))
	} else {
		parts = append(parts, "no flow")
	}
	if g := p.Bead.Group; g != nil && *g != "" {
		parts = append(parts, "group "+*g)
	}
	if len(p.Bead.Labels) > 0 {
		parts = append(parts, "labels "+strings.Join(p.Bead.Labels, ", "))
	}
	created := "created " + clock(p.Bead.CreatedAt)
	if by := p.Bead.CreatedBy; by != nil && *by != "" {
		created += " by " + *by
	}
	parts = append(parts, created, "changed "+clock(p.Bead.UpdatedAt))
	if p.Bead.ClosedAt != nil {
		parts = append(parts, "closed "+clock(*p.Bead.ClosedAt))
	}
	return strings.Join(parts, " · ")
}

// LinksLine is what the Linked beads fold keeps in one line: how many of each
// it holds.
func (p BeadPage) LinksLine() string {
	var parts []string
	if n := len(p.Children); n > 0 {
		parts = append(parts, countWords(n, "child", "children"))
	}
	if n := len(p.LedTo); n > 0 {
		parts = append(parts, fmt.Sprintf("led to %d", n))
	}
	if n := len(p.Deps); n > 0 {
		parts = append(parts, countWords(n, "edge", "edges"))
	}
	return strings.Join(parts, " · ")
}

// StageLine is what the Stage fold keeps in one line: the stage's file and
// what the fold says of how the stage ends or where the bead goes from it.
func (p BeadPage) StageLine() string {
	var parts []string
	if p.StageFile != nil {
		parts = append(parts, p.StageFile.Path)
	}
	switch {
	case p.ShowsOutcomes():
		ends := make([]string, len(p.Outcomes))
		for i, o := range p.Outcomes {
			ends[i] = o.Name + " to " + o.To
		}
		parts = append(parts, countWords(len(ends), "outcome", "outcomes")+": "+strings.Join(ends, ", "))
	case p.ShowsNext():
		parts = append(parts, "next: "+strings.Join(p.Next, ", "))
	}
	return strings.Join(parts, ", and ")
}

// sessionState is how a session reads in the Sessions fold's line: a manual
// group's claim is claimed, a live session says how long it has run, and one
// that ended says how.
func sessionState(s store.Session) string {
	live := s.State == "starting" || s.State == "running"
	switch {
	case live && s.Manual():
		return "claimed"
	case live:
		return s.State + " " + age(time.Since(s.StartedAt))
	}
	return s.State
}

// SessionsLine is what the Sessions fold keeps in one line: how many sessions
// worked the bead, each with its stage and how it stands.
func (p BeadPage) SessionsLine() string {
	if len(p.Sessions) == 0 {
		return "none"
	}
	parts := make([]string, len(p.Sessions))
	for i, s := range p.Sessions {
		parts[i] = s.Stage + " " + sessionState(s)
	}
	return fmt.Sprintf("%d: %s", len(parts), strings.Join(parts, ", "))
}

// HistoryLine is what the History fold keeps in one line: how many events of
// the bead the log still has and when the newest was.
func (p BeadPage) HistoryLine() string {
	if len(p.History) == 0 {
		return "none"
	}
	return fmt.Sprintf("%s, the newest %s", countWords(len(p.History), "event", "events"), p.History[len(p.History)-1].Clock())
}
