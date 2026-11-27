package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"yardr.dev/yardr/internal/chat"
	"yardr.dev/yardr/internal/store"
)

// Talk is the talk page: the conversation of the user the view acts as
// (--as) with one crew member or user, live, and the form that sends it
// mail. It is the web's `yardr chat` for one party at a time.
type Talk struct {
	// As is the user the view acts as, "" when it does not act: the page
	// then says so instead of any conversation.
	As string `json:"as"`
	// With is the crew member or other user in conversation, "" when the
	// view does not act or has no conversation partner.
	With string `json:"with"`
	// WithStatus is its state, in the words footplate lists it with; ""
	// when with names no crew member: a conversation with a person shows
	// no status.
	WithStatus string `json:"with_status,omitempty"`
	// Member is the crew member in conversation as /crew has it, whose agent
	// and uptime stand beside its state in the head; absent for a person.
	Member *CrewRow `json:"member,omitempty"`
	// Crew are the members to talk to, in the yard's order.
	Crew []TalkPeer `json:"crew"`
	// People are the users to talk to, in the yard's order, without the
	// user the view acts as.
	People []TalkPeer `json:"people"`
	// Letters are the conversation's newest page, oldest first, or the
	// page under Before.
	Letters []TalkLetter `json:"letters"`
	// More is whether mail older than the oldest letter shown was left
	// out; Before reads it.
	More bool `json:"more,omitempty"`
	// Before is the ?before= the page was read with, 0 for the newest
	// page: an older page is a snapshot, only the newest is live.
	Before uint64 `json:"before,omitempty"`
	// Head is the yard's newest seq when the page was read, before its
	// letters: the stream after it brings what happened since.
	Head uint64 `json:"head"`
	// MaxMail is how many bytes the form takes: a text it sends as one mail,
	// or as numbered mails when one does not hold it.
	MaxMail int `json:"max_mail"`
	// NeedsToken is whether the view acts as a user and this browser has
	// not shown its cookie (viewtoken.go), which is why As is empty: the
	// page says how to get it, not that the view was started without --as.
	NeedsToken bool `json:"needs_token,omitempty"`
	// Error is why a POST did not send, "" otherwise; Draft is the text
	// it did not send, back in the form.
	Error string `json:"error,omitempty"`
	Draft string `json:"draft,omitempty"`
}

// Composer is the id of the composer's form: the conversation, and the
// newest letter of the user's own on the page. A read of the conversation
// again carries the same id, so the browser leaves the form, and the words
// typed in it, where they are; the form after a send has a newer letter,
// and another conversation's has another party, so each is a new form and
// starts empty.
func (t Talk) Composer() string {
	var own uint64
	for _, l := range t.Letters {
		if l.Own {
			own = max(own, l.Seq)
		}
	}
	return fmt.Sprintf("send-%s-%d", t.With, own)
}

// TalkHead is what a conversation's head says: the party's name and, for a
// crew member, its state in footplate's words, with the member behind it,
// whose agent and uptime stand beside the state. A person has no state, and
// neither has a member that has left the crew.
type TalkHead struct {
	Name, State string
	Member      *CrewRow
}

// Party is the conversation's head: the stage of /talk and of /crew.
func (t Talk) Party() TalkHead { return TalkHead{Name: t.With, State: t.WithStatus, Member: t.Member} }

// TalkPeer is one party to talk to: a crew member or a user.
type TalkPeer struct {
	Name string `json:"name"`
	// Unread is whether the user has unread mail from it.
	Unread bool `json:"unread"`
}

// TalkLetter is one letter of the conversation.
type TalkLetter struct {
	store.Letter
	// Own is whether the user the view acts as wrote it, under their own
	// name or one a link trusted as them.
	Own bool `json:"own"`
	// Sender is who the page names for it: the sender, with the device
	// and, on mail to another name, the addressee.
	Sender string `json:"sender"`
	// Grouped is whether the letter reads under the head of the letter above
	// it, the sender's name and time: the same sender, within five minutes of
	// it, on the same day (grouped). Every other letter stands under a head
	// of its own. The day is the server's here; yard.js reads it again in the
	// browser's zone.
	Grouped bool `json:"grouped,omitempty"`
	// Day is the words of the day's separator standing above the letter, ""
	// where none does: the first letter of the page and the first of each day.
	Day string `json:"day,omitempty"`
	// Differs is whether a grouped letter's Sender differs from the letter
	// above it. Grouping goes by From alone, so the device, the name it
	// speaks as or the addressee that differs is said above its words, as a
	// head would say it.
	Differs bool `json:"differs,omitempty"`
	// Missing are the hashes of the letter's files the yard no longer
	// holds: the page draws those as a line saying so, never a broken
	// image. HTML-only: the JSON names the files as the letter has them.
	Missing map[string]bool `json:"-"`
}

// talkLimit is how many letters the talk page shows: the newest page of
// the conversation, an older page a ?before= away.
const talkLimit = 50

func (s *Server) handleTalk(w http.ResponseWriter, r *http.Request) { s.conversation(w, r, false) }

// conversation answers r with the talk page, the stage of /talk and of /crew:
// the conversation of the user the view acts as with the party ?with= names,
// else with the one the page opens on. /crew is that page with nothing named
// and the crew's rows as its JSON, so asCrew ignores the query and answers the
// crew where /talk answers the conversation.
func (s *Server) conversation(w http.ResponseWriter, r *http.Request, asCrew bool) {
	ctx := r.Context()
	head, err := s.st.Head(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// The crew once for the head, the list and /crew's JSON, which the shell
	// would otherwise read again.
	rows, err := s.crewRows(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	answer := func(t Talk, title, live string, box *store.Mailbox) {
		p := Page{Template: "talk", Kind: "talk", Title: title, Data: t, talk: &t, head: head, inbox: box, crew: rows, with: t.With, live: live}
		if asCrew {
			p.Title, p.Data, p.live = "Crew", Crew{Crew: rows, Head: head}, liveStage
		}
		s.respond(w, r, p)
	}
	as := s.acting(ctx)
	if as == "" {
		// The view does not act, or not for this browser: no user to show a
		// conversation of.
		answer(Talk{NeedsToken: s.token != nil}, "Talk", "", nil)
		return
	}
	users, err := s.st.Users(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	settings, err := s.st.Settings(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	crew := members(rows)
	var with string
	var before uint64
	if !asCrew {
		with = r.URL.Query().Get("with")
	}
	switch {
	case with == "":
		with = defaultTalkPeer(crew, users, as, yardmasterName(settings))
	case isTalkPeer(crew, users, as, with):
	default:
		s.fail(w, r, fmt.Errorf("talk partner %s: %w", with, store.ErrNotFound))
		return
	}
	if v := r.URL.Query().Get("before"); v != "" && !asCrew {
		if before, err = strconv.ParseUint(v, 10, 64); err != nil || before == 0 {
			s.respond(w, r, Page{Status: http.StatusBadRequest, Template: "error", Title: "Bad request",
				Data: errorData{Error: "before is an event's seq, not " + v}})
			return
		}
	}
	out := Talk{As: as, With: with, Crew: []TalkPeer{}, People: []TalkPeer{}, Letters: []TalkLetter{},
		Before: before, Head: head, MaxMail: store.MaxMailParts * store.MaxMailBytes}
	if with == "" {
		// The yard has no crew: the page says whose command that is.
		answer(out, "Talk", "", nil)
		return
	}
	box, err := s.readTalk(ctx, &out, rows, users, before)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	answer(out, "Talk with "+with, talkLive(before), box)
}

// readTalk fills t, which names its user and party, with what the page shows
// of the conversation: the picker's two lists with their unread marks, the
// party's state and the member behind it, and the newest page of the letters
// or the page under before. It returns the user's inbox read for the marks,
// which the rail's badge counts from. rows are the crew as crewRows read it.
func (s *Server) readTalk(ctx context.Context, t *Talk, rows []CrewRow, users []store.User, before uint64) (*store.Mailbox, error) {
	// Whose unread mail the user has, for the peers' marks: the letters are
	// not needed here.
	box, err := s.st.UserInbox(ctx, t.As, 1, t.As)
	if err != nil {
		return nil, err
	}
	crew := members(rows)
	t.Crew, t.People = talkPeers(crew, users, t.As, box.From)
	t.WithStatus = withStatus(crew, t.With)
	if i := slices.IndexFunc(rows, func(r CrewRow) bool { return r.Name == t.With }); i >= 0 {
		t.Member = &rows[i]
	}
	letters, more, err := s.st.Conversation(ctx, t.As, t.With, 0, talkLimit, before)
	if err != nil {
		return nil, err
	}
	t.More = more
	if t.Letters, err = s.talkLetters(ctx, t.As, t.With, letters); err != nil {
		return nil, err
	}
	return box, nil
}

// members are the crew rows as the page sees them. A row carries the derived
// unknown state while the daemon is lost, but its embedded store value keeps
// the last state the daemon reported.
func members(rows []CrewRow) []store.CrewMember {
	out := make([]store.CrewMember, len(rows))
	for i := range rows {
		out[i] = rows[i].CrewMember
		out[i].State, out[i].Status = rows[i].State, rows[i].Status
	}
	return out
}

// talkLive is how the talk page reads itself again: the newest page of the
// letters does, on every event of the stream, and an older page is a
// snapshot, read undisturbed.
func talkLive(before uint64) string {
	if before != 0 {
		return ""
	}
	return liveStage
}

// talkLetters is the conversation's letters as the talk page shows them:
// each with whether the user wrote it and who the page names for it, and
// with the files the yard no longer holds marked missing, as the bead
// page marks its notes'. A file the view would serve is not missing: the
// mail's own, or one a note names too. They are grouped by this machine's
// days and clock, which is the reader's where the view is served on
// loopback; yard.js reads the grouping again in the browser's zone.
func (s *Server) talkLetters(ctx context.Context, as, with string, letters []store.Letter) ([]TalkLetter, error) {
	shown := make([]TalkLetter, 0, len(letters))
	for _, l := range letters {
		t := TalkLetter{Letter: l, Own: l.From == as || l.As == as, Sender: sender(l, as, with)}
		for _, f := range l.Files {
			// The peer's word says this yard never took the file; it is
			// different from one the yard held and later lost.
			if f.NotTaken != "" {
				continue
			}
			if _, err := s.st.MailBlob(ctx, as, f.Hash); err == nil {
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			if _, err := s.st.NoteBlob(ctx, f.Hash); err == nil {
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			if t.Missing == nil {
				t.Missing = map[string]bool{}
			}
			t.Missing[f.Hash] = true
		}
		shown = append(shown, t)
	}
	groupTalk(shown, time.Local, time.Now())
	return shown, nil
}

// yardmasterName is the name the yard's coordinator carries: the yardmaster
// setting, its default where unset.
func yardmasterName(all map[string]string) string {
	if ym := all[store.YardmasterSetting]; ym != "" {
		return ym
	}
	return "yardmaster"
}

// groupWindow is how many seconds after the letter above one still reads
// under its head, as footplate groups its mail.
const groupWindow = 5 * 60

// grouped reports whether after reads under before's head: from the same
// sender, within five minutes of it, on the same day in zone. The sender is
// the letter's From, not the line the page names for it (Sender), which
// folds in the device, the name it speaks as and the addressee. The gap is
// in whole seconds, as the page's <time datetime> says them, so yard.js
// judges the same two letters the same way.
func grouped(before, after TalkLetter, zone *time.Location) bool {
	gap := after.At.Unix() - before.At.Unix()
	return before.From == after.From && -groupWindow <= gap && gap <= groupWindow &&
		sameDay(before.At, after.At, zone)
}

// sameDay reports whether a and b fall on one calendar day in zone.
func sameDay(a, b time.Time, zone *time.Location) bool {
	ay, am, ad := a.In(zone).Date()
	by, bm, bd := b.In(zone).Date()
	return ay == by && am == bm && ad == bd
}

// groupTalk marks how the letters, oldest first, read in zone: whether each
// is grouped under the head above it, the day it opens where it is the first
// of the page or the first of a day, and whether a grouped letter says
// something the letter above it does not. now is when the page is read, for
// the day's words.
func groupTalk(letters []TalkLetter, zone *time.Location, now time.Time) {
	for i := range letters {
		l := &letters[i]
		l.Grouped, l.Day, l.Differs = false, "", false
		if i == 0 {
			l.Day = dayWords(l.At, now, zone)
			continue
		}
		before := letters[i-1]
		l.Grouped = grouped(before, *l, zone)
		if !sameDay(before.At, l.At, zone) {
			l.Day = dayWords(l.At, now, zone)
		}
		l.Differs = l.Grouped && l.Sender != before.Sender
	}
}

// clockTime is a time as the time of day alone, for a time that stands under
// a day the page names already: a letter under its day's separator. It is
// this machine's time; yard.js shows the browser's.
func clockTime(t time.Time) string { return t.Local().Format("15:04") }

// dayWords is the day's separator: Today, Yesterday, or the weekday with the
// day and the month (Saturday 17 Oct), as footplate words it.
func dayWords(day, now time.Time, zone *time.Location) string {
	switch {
	case sameDay(day, now, zone):
		return "Today"
	case sameDay(day, now.In(zone).AddDate(0, 0, -1), zone):
		return "Yesterday"
	}
	return day.In(zone).Format("Monday 2 Jan")
}

// defaultTalkPeer is the partner the talk page opens on: the yardmaster
// where a crew member bears that name, else its first crew member, else
// its first user other than the one the view acts as.
func defaultTalkPeer(crew []store.CrewMember, users []store.User, as, yardmaster string) string {
	if slices.ContainsFunc(crew, func(r store.CrewMember) bool { return r.Name == yardmaster }) {
		return yardmaster
	}
	if len(crew) > 0 {
		return crew[0].Name
	}
	for _, u := range users {
		if u.Name != as {
			return u.Name
		}
	}
	return ""
}

// isTalkPeer reports whether name is a crew member or another user of the
// yard: the talk page holds a conversation with either.
func isTalkPeer(crew []store.CrewMember, users []store.User, as, name string) bool {
	if name == as {
		return false
	}
	return slices.ContainsFunc(crew, func(r store.CrewMember) bool { return r.Name == name }) ||
		slices.ContainsFunc(users, func(u store.User) bool { return u.Name == name })
}

// talkPeers is the talk picker's two lists: the crew in the yard's order,
// then the users but the one the view acts as, each saying whether the
// user has unread mail from it.
func talkPeers(crew []store.CrewMember, users []store.User, as string, unread []string) (peers, people []TalkPeer) {
	peers, people = []TalkPeer{}, []TalkPeer{}
	for _, r := range crew {
		peers = append(peers, TalkPeer{Name: r.Name, Unread: slices.Contains(unread, r.Name)})
	}
	for _, u := range users {
		if u.Name == as {
			continue
		}
		people = append(people, TalkPeer{Name: u.Name, Unread: slices.Contains(unread, u.Name)})
	}
	return peers, people
}

// withStatus is the state of the member in conversation, in footplate's
// words (memberStatus). "" when with names no crew member.
func withStatus(crew []store.CrewMember, with string) string {
	for _, r := range crew {
		if r.Name == with {
			return memberStatus(r)
		}
	}
	return ""
}

// memberStatus is the state of a crew member in footplate's words. It uses the
// crew state and status directly, but for a login screen: a member there reads
// no mail, however its runtime reads it, and "idle" would say it does
// (yardr-302g). yardr chat adds a refinement for failed retries.
func memberStatus(r store.CrewMember) string {
	switch r.State {
	case "failed":
		return chat.Failed
	case "running":
		if r.LoginScreen {
			return chat.NotLoggedIn
		}
		switch r.Status {
		case "working":
			return chat.Working
		case "idle":
			return chat.Idle
		case "blocked":
			return chat.Blocked
		default:
			return chat.Running
		}
	}
	return chat.NotRunning
}

// sender is who the talk page names for l: the sender with its device, as
// `yardr chat` shows it, and the addressee where it is neither party of
// the conversation (mail to a name a link trusted as the user).
func sender(l store.Letter, me, with string) string {
	from := store.ViaMark(l.From, l.Via)
	if l.As != "" {
		from += " (as " + l.As + ")"
	}
	if l.To != with && l.To != me {
		from += " to " + l.To
	}
	return store.ReMark(from, l.Answered)
}

// handleTalkPost takes the talk page's form: mail to the member or user in
// conversation, sent as the user the request acts as. A text over one
// mail's size is sent as numbered mails, as yardr mail sends it. It is
// registered only on a view that acts (ActAs, or UseJWT for JWT mode):
// without either the mux answers 405 itself, as on every other route. Mail
// sent, it redirects to the conversation; mail refused re-reads the page
// with the reason, as 200 so htmx puts it in place (it leaves an error's
// page out).
func (s *Server) handleTalkPost(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); !ownOrigin(origin, r.Host) {
		http.Error(w, "yardr web takes mail only from its own pages: the request's origin is "+quoted(origin)+", not this view's. A page on another site cannot send mail as this view's user",
			http.StatusForbidden)
		return
	}
	ctx := r.Context()
	crew, err := s.st.Crew(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	users, err := s.st.Users(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	settings, err := s.st.Settings(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	with := r.URL.Query().Get("with")
	if with == "" {
		with = defaultTalkPeer(crew, users, s.acting(ctx), yardmasterName(settings))
	}
	if with == "" || !isTalkPeer(crew, users, s.acting(ctx), with) {
		s.fail(w, r, fmt.Errorf("talk partner %s: %w", with, store.ErrNotFound))
		return
	}
	text := r.PostFormValue("text")
	if err := store.MailText(s.st, ctx, with, text, s.acting(ctx)); err != nil {
		s.talkAgain(w, r, with, text, err.Error())
		return
	}
	http.Redirect(w, r, "/talk?with="+url.QueryEscape(with), http.StatusSeeOther)
}

// talkAgain re-reads the talk page after a POST that sent nothing, with
// the reason and the text back in the form.
func (s *Server) talkAgain(w http.ResponseWriter, r *http.Request, with, text, why string) {
	ctx := r.Context()
	head, err := s.st.Head(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.crewRows(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	users, err := s.st.Users(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := Talk{As: s.acting(ctx), With: with, Crew: []TalkPeer{}, People: []TalkPeer{}, Letters: []TalkLetter{},
		Head: head, MaxMail: store.MaxMailParts * store.MaxMailBytes, Error: why, Draft: text}
	box, err := s.readTalk(ctx, &out, rows, users, 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.respond(w, r, Page{Template: "talk", Kind: "talk", Title: "Talk with " + with, Data: out, talk: &out, head: head, inbox: box, crew: rows, live: liveStage})
}

// ownOrigin reports whether origin is this request's own: an http or
// https URL whose host and explicit port are the request's. The request's
// host was checked by ServeHTTP against the names the view answers to.
// Other names and ports are distinct origins: a page on one cannot send
// mail through a request addressed to another. The scheme may belong to
// an edge in front (https, behind tailscale serve) or to the view itself,
// so it is not compared.
func ownOrigin(origin, requestHost string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host != strings.ToLower(hostOf(requestHost)) {
		return false
	}
	_, port, err := net.SplitHostPort(requestHost)
	if err != nil {
		// Both omit a port, as when the edge serves on a default port.
		return u.Port() == ""
	}
	return u.Port() == port
}

// quoted is origin as the refusal names it: what a request without one
// sent.
func quoted(origin string) string {
	if origin == "" {
		return "none"
	}
	return strconv.Quote(origin)
}
