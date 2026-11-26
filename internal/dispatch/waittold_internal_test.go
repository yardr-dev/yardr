package dispatch

import (
	"context"
	"errors"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// waitLog is a store that answers a bead's events with the ones it holds,
// oldest first as the store does, or with an error, and counts the reads.
type waitLog struct {
	store.API
	events []store.Event
	err    error
	reads  int
}

func (l *waitLog) Events(context.Context, string, int) ([]store.Event, error) {
	l.reads++
	return l.events, l.err
}

// A daemon that has told no wait of a bead takes what an earlier one told from
// the bead's log, once: the newest wait of each kind in the bead's stay in its
// stage, which a claim or an advance ends.
func TestNotedWaitStartsFromTheWaitsInTheLog(t *testing.T) {
	t.Parallel()
	wait := func(kind, stage, route string) store.Event {
		return store.Event{Kind: kind, Data: map[string]any{"stage": stage, "route": route}}
	}
	quota := func(route string) store.Event { return wait("quota_wait", "new", route) }
	group := func(route string) store.Event { return wait("group_wait", "new", route) }
	type ask struct {
		kind, route string
		told        bool
	}
	for _, tc := range []struct {
		name  string
		log   []store.Event // oldest first
		prior string        // a kind of another report this daemon told of the bead first
		err   error         // what the read of the log answers with
		asks  []ask
		reads int // reads of the log over all the asks
	}{
		{name: "an empty log is told once", asks: []ask{{"quota_wait", "r1", false}, {"quota_wait", "r1", true}}, reads: 1},
		{name: "a wait in the log is told", log: []store.Event{quota("r1")}, asks: []ask{{"quota_wait", "r1", true}}, reads: 1},
		{name: "a limits wait in the log is told", log: []store.Event{wait("limits_wait", "new", "r1")}, asks: []ask{{"limits_wait", "r1", true}}, reads: 1},
		{
			name:  "one read seeds every kind it finds, and only those",
			log:   []store.Event{group("r1"), quota("r1")},
			asks:  []ask{{"quota_wait", "r1", true}, {"group_wait", "r1", true}, {"limits_wait", "r1", false}},
			reads: 1,
		},
		{name: "another route is another wait", log: []store.Event{quota("r1")}, asks: []ask{{"quota_wait", "r2", false}, {"quota_wait", "r2", true}}, reads: 1},
		{name: "the newest wait of a kind stands", log: []store.Event{quota("r1"), quota("r2")}, asks: []ask{{"quota_wait", "r2", true}}, reads: 1},
		{
			name:  "a claim ends the stay",
			log:   []store.Event{quota("r1"), {Kind: "claimed", Data: map[string]any{"stage": "new"}}},
			asks:  []ask{{"quota_wait", "r1", false}, {"quota_wait", "r1", true}},
			reads: 1,
		},
		{
			name:  "an advance ends the stay",
			log:   []store.Event{quota("r1"), {Kind: "advanced", Data: map[string]any{"to": "new"}}},
			asks:  []ask{{"quota_wait", "r1", false}},
			reads: 1,
		},
		{name: "a wait in another stage is not this one's", log: []store.Event{wait("quota_wait", "review", "r1")}, asks: []ask{{"quota_wait", "r1", false}}, reads: 1},
		{
			// reportOnce's kinds share the bead's slot, and a pass may reach
			// one of them before a wait.
			name:  "another kind's report does not stand for the waits",
			log:   []store.Event{quota("r1")},
			prior: "capped",
			asks:  []ask{{"quota_wait", "r1", true}},
			reads: 1,
		},
		{
			name:  "a read that fails seeds nothing and is not tried again",
			log:   []store.Event{quota("r1")},
			err:   errors.New("the store is gone"),
			asks:  []ask{{"quota_wait", "r1", false}, {"quota_wait", "r1", true}},
			reads: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := &waitLog{events: tc.log, err: tc.err}
			d := &Dispatcher{Store: l}
			d.defaults()
			b := store.Bead{ID: "yd-aaaa", Stage: "new"}
			if tc.prior != "" {
				d.noted(b.ID, tc.prior, "new|other")
			}
			for _, a := range tc.asks {
				if got := d.notedWait(context.Background(), b, a.kind, a.route); got != a.told {
					t.Errorf("notedWait(%s, %s) = %t, want %t", a.kind, a.route, got, a.told)
				}
			}
			if l.reads != tc.reads {
				t.Errorf("reads of the log = %d, want %d", l.reads, tc.reads)
			}
		})
	}
}
