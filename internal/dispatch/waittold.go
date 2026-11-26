package dispatch

import (
	"context"
	"slices"

	"yardr.dev/yardr/internal/store"
)

// waitKinds are the events that tell a bead waits for its route's groups.
// Each carries the stage and the route the bead waits on.
var waitKinds = []string{"quota_wait", "group_wait", "limits_wait"}

// waitLookback bounds the events notedWait reads per bead for what an earlier
// daemon told: a wait with more events than this after it is told again.
const waitLookback = 200

// notedWait is noted for a wait of waitKinds, keyed by the stage and route the
// bead waits on, that holds across a daemon that replaces itself. d.reported
// starts empty with the new daemon, so a bead that waits on through the change
// would be told again, with the figures of the moment, though nothing of its
// wait has changed. The log holds what was told: a daemon that has told no
// wait of the bead takes it from there, once.
func (d *Dispatcher) notedWait(ctx context.Context, b store.Bead, kind, route string) bool {
	if !d.toldWait(b.ID) {
		d.seedWaits(ctx, b)
	}
	return d.noted(b.ID, kind, b.Stage+"|"+route)
}

// toldWait is whether this daemon has told a wait of the bead since it
// started or since the bead's claim. It does not ask whether the bead has a
// slot at all: reportOnce's kinds (capped, backoff, ...) fill the same slot,
// and a pass may reach one of them before a wait.
func (d *Dispatcher) toldWait(bead string) bool {
	for _, kind := range waitKinds {
		if d.reported[bead][kind] != "" {
			return true
		}
	}
	return false
}

// seedWaits notes the waits the bead's log says were told in its stay in its
// stage, which runs back from the newest event to its last claim or advance:
// the claim clears the bead's slot, and an advance starts a stay of its own,
// even into a stage the bead was in before. The newest event of a kind is the
// one that stands, as the key of the last wait told is what noted holds. A
// read that fails seeds nothing, and the wait is told as it would be without
// the log.
func (d *Dispatcher) seedWaits(ctx context.Context, b store.Bead) {
	evs, err := d.Store.Events(ctx, b.ID, waitLookback)
	if err != nil {
		d.Log.Warn("reading a bead's earlier waits", "bead", b.ID, "err", err)
		return
	}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Kind == "claimed" || e.Kind == "advanced" {
			return
		}
		if !slices.Contains(waitKinds, e.Kind) || e.Data["stage"] != b.Stage || d.reported[b.ID][e.Kind] != "" {
			continue
		}
		route, _ := e.Data["route"].(string)
		d.noted(b.ID, e.Kind, b.Stage+"|"+route)
	}
}
