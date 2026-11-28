package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// WaitingBead is one row of a person's parked queue: a bead standing in a
// manual stage of theirs, with the moves they may make of it. A bead that
// carries an open ask to the person is not a row: it stands among their
// decisions (UserAsks), where answering the ask moves it or not.
type WaitingBead struct {
	Bead  string `json:"bead"`
	Title string `json:"title"`
	// Depot is the depot it is worked in, absent where it has none.
	Depot string `json:"depot,omitempty"`
	// Stage is where it stands, and Group the manual group whose stage it is.
	Stage string `json:"stage"`
	Group string `json:"group"`
	// Since is when the bead last changed: about when it began to wait.
	Since time.Time `json:"since"`
	// Revision is the bead's revision the row was read at: a move names it,
	// so that a bead that changed since is refused and read again.
	Revision string `json:"revision"`
	// Actions are the moves offered to the person, in review's order. None
	// where FlowError stands: no move exists until the flow is fixed.
	Actions []Move `json:"actions"`
	// FlowError says why the bead's flow does not resolve.
	FlowError string `json:"flow_error,omitempty"`
	// TemplateError is why entering the stage put no stage ask, where the
	// ask would stand (NextStages). The bead still moves by hand.
	TemplateError string `json:"template_error,omitempty"`
}

// repoRead is one depot's repository flows as read between the passes of
// Waiting: the flows, or why they could not be read.
type repoRead struct {
	flows *RepoFlows
	err   error
}

// Waiting reads the beads parked for the person name, longest waiting first
// (the bead's last change, then its id), for a client that is not yardr. It
// is what yardr review walks, less what is not a move of the person's to
// make:
//
//   - the bead is open and a work bead (a journal or a memory is none), and
//     the viewer may see it; an omitted viewer is the yard itself;
//   - a manual group works its stage (ManualGroup), and the person is one of
//     its members where it lists any; a group that lists none is anyone's;
//   - the bead stands past the first stage of the flow that governs it, so a
//     backlog, where beads start, is never here; no flow, no first stage;
//   - nothing is the matter with it: a held bead, and one in a stage its flow
//     lacks, are faults to mend and not moves to make, and stand among the
//     web's waits and never its parked beads, which this call keeps to;
//   - no ask to the person stands open on it, which would be among their
//     decisions;
//   - and the person's part in it is not in (PartIn): with another member
//     left in a group of several, a bead the person answered, declined or
//     put aside an ask to them on is the other member's to move. An ask its
//     asker took back is nobody's answer, and takes the bead from no one's
//     queue.
//
// Each flow is resolved once for the call, and each depot's repository once
// (between two reads of the store, since git must not run inside one), not
// once for each bead as NextStages would. Only the user reads their queue,
// and an operator any user's (mayReadInbox); actor is who reads, and left
// out is the user.
func (s *Store) Waiting(ctx context.Context, name, actor string, viewer ...string) ([]WaitingBead, error) {
	// The gate runs on every call, as UserAsks' does; only the rows
	// below it are reused, per viewer at the head they were computed at
	// (headCache), since a following device asks for them on every bead
	// event beside the asks and the boards.
	if err := s.read(func(tx *bolt.Tx) error {
		if _, err := getUser(tx, name); err != nil {
			return err
		}
		_, err := s.mayReadInbox(ctx, tx, name, actor, "read")
		return err
	}); err != nil {
		return nil, err
	}
	key := viewerKey{name: name, actor: actor, viewer: firstViewer(viewer)}
	compute := func() ([]WaitingBead, time.Time, error) { return s.waiting(ctx, name, actor, key.viewer) }
	head, err := s.Head(ctx)
	if err != nil {
		rows, _, err := compute()
		return rows, err
	}
	return s.waitsCache.do(ctx, key, head, now(), compute, cloneWaiting)
}

// waiting computes the rows Waiting serves, for viewer, and says when
// they next change with no write at all: the earliest return of a
// put-aside ask to the person on a bead the pass read (zero where none
// stands aside), which bounds how long the rows are the answer.
func (s *Store) waiting(ctx context.Context, name, actor, viewer string) ([]WaitingBead, time.Time, error) {
	repos := map[string]repoRead{}
	for {
		rows, missing, until, err := s.parked(ctx, name, actor, viewer, repos)
		if err != nil || len(missing) == 0 {
			return rows, until, err
		}
		for _, depot := range missing {
			rf, err := s.RepoFlows(ctx, depot)
			if ctx.Err() != nil {
				return nil, time.Time{}, ctx.Err()
			}
			repos[depot] = repoRead{rf, err}
		}
	}
}

// BeadMoves reads one bead's moves for the person name: the same row Waiting
// would give, with a flow's first stage served too. It answers nil where the
// bead has no row for the person (closed, held, non-work, stranded, not in a
// manual stage of theirs, carrying an open ask to them, answered with another
// member left, missing, or hidden from viewer). Only the bead's own depot
// repository is read, between store reads as Waiting does. The gate is the
// same: only the user reads their own, and an operator any user's.
func (s *Store) BeadMoves(ctx context.Context, bead, name, actor string, viewer ...string) (*WaitingBead, error) {
	v := firstViewer(viewer)
	repos := map[string]repoRead{}
	for {
		var row *WaitingBead
		var need string
		err := s.read(func(tx *bolt.Tx) error {
			row, need = nil, ""
			p, err := s.newParkedPass(ctx, tx, name, actor, v, repos, true)
			if err != nil {
				return err
			}
			b, err := loadBead(tx, bead)
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			b.Train = trainOf(tx, b.UUID)
			row, need, _, err = s.parkedBead(ctx, p, b)
			return err
		})
		if err != nil || need == "" {
			return row, err
		}
		rf, err := s.RepoFlows(ctx, need)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		repos[need] = repoRead{rf, err}
	}
}

// parkedPass holds the store state read once for a Waiting or BeadMoves pass.
type parkedPass struct {
	tx       *bolt.Tx
	name     string
	viewer   string
	operator string
	routes   []Route
	groups   map[string]Group
	repos    map[string]repoRead
	memo     flowMemo
	at       time.Time
	first    bool
}

func (s *Store) newParkedPass(ctx context.Context, tx *bolt.Tx, name, actor, viewer string, repos map[string]repoRead, first bool) (*parkedPass, error) {
	if _, err := getUser(tx, name); err != nil {
		return nil, err
	}
	if _, err := s.mayReadInbox(ctx, tx, name, actor, "read"); err != nil {
		return nil, err
	}
	routes, err := sortedRoutes(tx)
	if err != nil {
		return nil, err
	}
	operator, err := operatorIn(tx)
	if err != nil {
		return nil, err
	}
	groups := map[string]Group{}
	if err := each(tx, bGroups, "", func(_ []byte, g *Group) error {
		groups[g.Name] = *g
		return nil
	}); err != nil {
		return nil, err
	}
	return &parkedPass{
		tx: tx, name: name, viewer: viewer, operator: operator,
		routes: routes, groups: groups, repos: repos, at: now(), first: first,
	}, nil
}

// parkedBead builds the WaitingBead row for one bead under p, or nil where the
// bead has no row for p.name. needRepo names a depot whose repository flows p
// lacked; back is the earliest return of a put-aside ask on the bead.
func (s *Store) parkedBead(ctx context.Context, p *parkedPass, b *beadRow) (row *WaitingBead, needRepo string, back time.Time, err error) {
	if b.Status != "open" || NotWork[b.Type] || b.Hold {
		return nil, "", time.Time{}, nil
	}
	g, ok := ManualGroup(p.routes, b.Bead, p.operator, p.groups)
	if !ok || len(g.Members) > 0 && !slices.Contains(g.Members, p.name) {
		return nil, "", time.Time{}, nil
	}
	if s.may(ctx, p.tx, p.viewer, ActSee, Object{Kind: KindBead, Name: b.ID}) != nil {
		return nil, "", time.Time{}, nil
	}
	var repo *RepoFlows
	if b.Depot != nil {
		repo = p.repos[*b.Depot].flows
	}
	f, src, flowErr := p.memo.resolve(p.tx, b.Depot, b.Type, PinnedFlow(&b.Bead), repo)
	if errors.Is(flowErr, errNeedRepo) && b.Depot != nil {
		read, ok := p.repos[*b.Depot]
		if !ok {
			return nil, *b.Depot, time.Time{}, nil
		}
		// The repository could not be read: the flow is not known.
		flowErr = read.err
	}
	if flowErr == nil && f != nil && (!slices.Contains(f.Stages, b.Stage) || !p.first && f.Stages[0] == b.Stage) {
		// Stranded, which is a fault, or (unless first is true) at the flow's
		// first stage, where its beads start.
		return nil, "", time.Time{}, nil
	}
	notes, err := beadNotes(p.tx, b.UUID)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	asked, tookPart, back := asksOf(notes, p.name, p.at)
	if asked || PartIn(g, p.name, tookPart) {
		return nil, "", back, nil
	}
	w := WaitingBead{Bead: b.ID, Title: b.Title, Stage: b.Stage, Group: g.Name,
		Since: b.UpdatedAt, Revision: b.Revision, Actions: []Move{}}
	if b.Depot != nil {
		w.Depot = *b.Depot
	}
	if flowErr != nil {
		w.FlowError = flowErr.Error()
	} else {
		next := &NextStages{Bead: b.ID, Stage: b.Stage, Stages: []string{}}
		if f != nil {
			if err := next.fill(p.tx, b, f, src, repo); err != nil {
				return nil, "", back, err
			}
		}
		w.TemplateError = next.TemplateError
		if moves := MovesFor(&b.Bead, next, p.routes, p.operator); moves != nil {
			w.Actions = moves
		}
	}
	return &w, "", back, nil
}

// parked is one pass of Waiting over the repositories it has read so far.
// missing names the depots whose repository the flows of some bead depend on
// and that the pass lacked: the rows are then not final, and the caller reads
// those repositories and passes again. until is the earliest return of a
// put-aside ask the pass read, as waiting reports it.
func (s *Store) parked(ctx context.Context, name, actor, viewer string, repos map[string]repoRead) ([]WaitingBead, []string, time.Time, error) {
	var rows []WaitingBead
	var missing []string
	var until time.Time
	err := s.read(func(tx *bolt.Tx) error {
		rows, missing, until = []WaitingBead{}, nil, time.Time{}
		p, err := s.newParkedPass(ctx, tx, name, actor, viewer, repos, false)
		if err != nil {
			return err
		}
		open, err := s.openBeads(tx)
		if err != nil {
			return err
		}
		for _, b := range open {
			row, need, back, err := s.parkedBead(ctx, p, b)
			if err != nil {
				return err
			}
			if !back.IsZero() && (until.IsZero() || back.Before(until)) {
				until = back
			}
			if need != "" {
				if !slices.Contains(missing, need) {
					missing = append(missing, need)
				}
				continue
			}
			if row != nil {
				rows = append(rows, *row)
			}
		}
		slices.SortStableFunc(rows, func(a, b WaitingBead) int {
			if c := a.Since.Compare(b.Since); c != 0 {
				return c
			}
			return strings.Compare(a.Bead, b.Bead)
		})
		return nil
	})
	return rows, missing, until, err
}

// asksOf says, from a bead's notes, whether an ask to the person stands open
// on it, whether they took part in one (partStates), and when the earliest
// put-aside ask to them comes back (zero where none stands aside).
func asksOf(notes []Note, name string, at time.Time) (open, tookPart bool, back time.Time) {
	for _, r := range ResolveAsks(notes, at) {
		if r.AskErr != nil || r.Ask.To != name {
			continue
		}
		if r.State == AskOpen {
			open = true
		} else if r.State.tookPart() {
			tookPart = true
		}
		if r.State == AskAside && (back.IsZero() || r.ReturnAt.Before(back)) {
			back = r.ReturnAt
		}
	}
	return open, tookPart, back
}

// partStates are the states in which an ask stands for a part the person it
// is put to took in it: they answered it, declined it or put it aside. One
// still open has none of theirs yet, and one taken back never will: its asker
// ended it, which is no answer of the person, though UserAsks lists it among
// the decided. A state a later build adds is none of theirs until it is
// listed here, so that a bead is never left to nobody by a word this build
// does not know.
var partStates = []AskState{AskAnswered, AskAside, AskDeclined}

// tookPart reports whether an ask in this state is one the person took part
// in (partStates).
func (s AskState) tookPart() bool { return slices.Contains(partStates, s) }

// TookPart reports whether the person took part in the ask (partStates), as
// State says it in words, which is all a client of the socket holds.
func (d DecidedAsk) TookPart() bool {
	return slices.ContainsFunc(partStates, func(s AskState) bool { return s.String() == d.State })
}

// PartIn reports whether the person's part in a bead parked in a stage of the
// manual group g is in, which leaves the bead to another member to move: g
// lists several members, the person is one of them, and they took part in an
// ask to them on it (asksOf, DecidedAsk.TookPart). It says nothing of an ask
// open to the person: while one stands the bead is theirs to answer, and each
// caller deals with that its own way (Waiting leaves the bead among their
// decisions, the web's waits shows the ask with the wait). Both ask PartIn
// the rest, so that Home, /line and the call count the same beads.
func PartIn(g Group, name string, tookPart bool) bool {
	return tookPart && len(g.Members) > 1 && slices.Contains(g.Members, name)
}
