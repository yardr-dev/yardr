package web

import (
	"strings"
	"time"

	"yardr.dev/yardr/internal/store"
)

// askNotes are the notes of one bead that hold an ask, or an answer to one,
// read as words. A note of either kind is JSON in its text, which no person
// reads: the page and the history word it as `yardr prime` does, from the
// store's own reading of the notes (store.ResolveAsks), so the two never say
// a question differently and the web holds no parser of its own.
type askNotes struct {
	// words is each ask note's words by its seq: the question, whom it is
	// to, the context, the options and how the ask stands (open, answered
	// with the pick, put aside, declined or taken back; a run ask's command
	// as text). A note that holds no ask is not among them.
	words map[uint64]string
	// asks is the seq of the ask each answer note belongs to, by the answer's
	// seq. An answer is read inside its ask, where `yardr prime` reads it, so
	// the pick is not drawn twice. An answer that names no ask of the bead is
	// not among them.
	asks map[uint64]uint64
}

// readAsks reads the asks among a bead's notes, oldest first, as of now: a
// put-aside ask is read as put aside until it comes back. marks is whom the
// links trust (store.TrustedAs), nil for none, which marks every answer from
// another yard as foreign.
//
// A note of kind ask that no longer parses is left out, so it is drawn as it
// stands: the page does not drop a note.
func readAsks(notes []store.Note, now time.Time, marks map[string]string) askNotes {
	out := askNotes{words: map[uint64]string{}, asks: map[uint64]uint64{}}
	for _, r := range store.ResolveAsks(notes, now) {
		if r.AskErr != nil {
			continue
		}
		// Letters only, as every note's words are drawn: the asker's words
		// carry no mark of direction or control character to the page.
		out.words[r.Note.Seq] = store.NoteLetters(strings.Join(r.LinesMarked(marks), "\n"))
		for _, a := range r.Answers {
			out.asks[a.Note.Seq] = r.Note.Seq
		}
	}
	return out
}

// Words is the words of the ask note seq, "" for a note that is no ask, or
// holds one that does not parse.
func (a askNotes) Words(seq uint64) string { return a.words[seq] }

// Folded reports whether the note seq is an answer drawn inside its ask.
func (a askNotes) Folded(seq uint64) bool {
	_, ok := a.asks[seq]
	return ok
}

// of is what the note n says where a line has room for its words alone: an
// ask's own words, an answer's the first line of its ask's, which names the
// ask and says how it stands (the pick itself is read under the ask, where the
// answer is folded), and any other note's text as it was written.
func (a askNotes) of(n store.Note) string {
	if w, ok := a.words[n.Seq]; ok {
		return w
	}
	if ask, ok := a.asks[n.Seq]; ok {
		head, _, _ := strings.Cut(a.words[ask], "\n")
		return head
	}
	return n.Text
}
