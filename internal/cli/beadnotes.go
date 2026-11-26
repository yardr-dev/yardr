package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"yardr.dev/yardr/internal/store"
)

// printNotes adds a bead's notes to bead show's text, after the body, the
// oldest first: each with its author, time and seq, the text as written,
// and how it stands with the session it is for, when it is for one. None
// prints nothing, as before.
//
// An ask's text is JSON, so an ask is drawn as the question it is, with its
// numbered options, its state and the answers it took in, as prime and
// review draw it, and those answers print no note of their own. An ask that
// does not parse, and an answer that names no ask, print their text as
// stored: bead show is the view that keeps the raw form, where prime and
// review say "not a question any more".
//
// A note's text may come from another yard, or from a device on the
// listener, and says nothing of who wrote it: the line of author and time
// does. So the text stands further in than that line, where no line of it
// can be taken for the start of another note, and it reaches the terminal
// as letters only (store.NoteLetters).
//
// An author from another yard says whose word the note is, as mail does:
// (as <user>) where a link trusts the name (as, from trustMarks), the
// foreign line otherwise. A nil as marks with no grants: when the trusts
// cannot be read, every name of another yard carries the foreign line
// and nobody reads (as <user>).
func printNotes(w io.Writer, notes []store.Note, home string, as map[string]string) {
	if len(notes) == 0 {
		return
	}
	if as == nil {
		as = map[string]string{}
	}
	asks := map[uint64]store.ResolvedAsk{}
	claimed := map[uint64]bool{}
	for _, r := range store.ResolveAsks(notes, time.Now()) {
		asks[r.Note.Seq] = r
		for _, a := range r.Answers {
			claimed[a.Note.Seq] = true
		}
	}
	fmt.Fprintln(w, "\nNotes:")
	for _, n := range notes {
		if n.Kind == store.NoteAnswer && claimed[n.Seq] {
			continue
		}
		author := store.OneLine(store.MarkedAuthor(n.Author, as))
		fmt.Fprintf(w, "  %s %s (note %d)%s\n", author, n.At.Local().Format("2006-01-02 15:04:05"), n.Seq, noteTell(n))
		text := n.Text
		if r, ok := asks[n.Seq]; ok && r.AskErr == nil {
			text = strings.Join(r.LinesMarked(as), "\n")
		}
		fmt.Fprintf(w, "    %s\n", strings.ReplaceAll(store.NoteLetters(text), "\n", "\n    "))
		if line := store.NoteFilesLine(home, n.Files); line != "" {
			fmt.Fprintf(w, "    %s\n", line)
		}
		fmt.Fprintln(w)
	}
}

// noteTell is how the note stands with the session it is for: told and
// read, told and not read, still to be told, or not told for good, with
// whether that session is still live. "" for a note that is for no
// session.
func noteTell(n store.Note) string {
	switch {
	case n.Notify == "":
		return ""
	case n.Acked:
		return ", read by session " + n.Notify
	case n.Told && n.NotifyLive:
		return ", told session " + n.Notify + ", not read yet"
	case n.Told:
		return ", told session " + n.Notify + ", not read (session ended)"
	case n.Untold != "":
		return ", session " + n.Notify + " could not be told: " + store.OneLine(n.Untold)
	case n.NotifyLive:
		return ", session " + n.Notify + " will be told"
	default:
		return ", session " + n.Notify + " ended before it was told"
	}
}
