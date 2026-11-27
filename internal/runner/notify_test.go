package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

func TestManualNotifiesOncePerBeadAndStage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := storetest.New(t)
	mustT(t, st.PutGroup(ctx, store.Group{Name: "humans", Runner: "manual", Config: map[string]any{"notify": true}}, "me"))
	_, err := st.AddRoute(ctx, store.Route{Stage: "review", Group: "humans"}, "me")
	mustT(t, err)
	b, err := st.CreateBead(ctx, store.NewBead{Title: "Add login", Stage: "review"}, "t")
	mustT(t, err)

	var s shown
	mustT(t, daemon(st, s.show(nil)).Reconcile(ctx))
	if len(s.titles) != 1 {
		t.Fatalf("notifications after first claim = %d, want 1", len(s.titles))
	}
	if !strings.Contains(s.titles[0], b.ID) || !strings.Contains(s.titles[0], "review") ||
		!strings.Contains(s.bodies[0], "Add login") || !strings.Contains(s.bodies[0], "yardr bead show "+b.ID) {
		t.Fatalf("notification lacks bead, stage, title or action: %q / %q", s.titles[0], s.bodies[0])
	}

	// The manual session is released and re-claimed by a restarted daemon:
	// same bead, same stage, no second notification.
	_, err = st.Release(ctx, b.ID, "restart", store.Expect{}, "t")
	mustT(t, err)
	mustT(t, daemon(st, s.show(nil)).Reconcile(ctx))
	if live, _ := st.Sessions(ctx, false); len(live) != 1 {
		t.Fatalf("bead not re-claimed: %v", live)
	}
	if len(s.titles) != 1 {
		t.Fatalf("re-claim after restart notified again: %v", s.titles)
	}

	// Sent back and returning to review is a new arrival.
	_, err = st.Advance(ctx, b.ID, "new", store.Expect{}, "t")
	mustT(t, err)
	_, err = st.Advance(ctx, b.ID, "review", store.Expect{}, "t")
	mustT(t, err)
	mustT(t, daemon(st, s.show(nil)).Reconcile(ctx))
	if len(s.titles) != 2 {
		t.Fatalf("re-entering the stage: %d notifications, want 2", len(s.titles))
	}
}

func TestManualNotifyIsOptIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := storetest.New(t)
	mustT(t, st.PutGroup(ctx, store.Group{Name: "backlog", Runner: "manual"}, "me"))
	_, err := st.AddRoute(ctx, store.Route{Stage: "backlog", Group: "backlog"}, "me")
	mustT(t, err)
	_, err = st.CreateBead(ctx, store.NewBead{Title: "Someday", Stage: "backlog"}, "t")
	mustT(t, err)
	var s shown
	mustT(t, daemon(st, s.show(nil)).Reconcile(ctx))
	if len(s.titles) != 0 {
		t.Fatalf("group without notify=true notified: %v", s.titles)
	}
}

// A failing notifier never fails the claim, and is retried on the next
// claim since nothing was recorded as shown.
func TestManualNotifyFailureKeepsClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := storetest.New(t)
	mustT(t, st.PutGroup(ctx, store.Group{Name: "humans", Runner: "manual", Config: map[string]any{"notify": "true"}}, "me"))
	_, err := st.AddRoute(ctx, store.Route{Stage: "review", Group: "humans"}, "me")
	mustT(t, err)
	b, err := st.CreateBead(ctx, store.NewBead{Title: "x", Stage: "review"}, "t")
	mustT(t, err)

	var s shown
	mustT(t, daemon(st, s.show(errors.New("herdr down"))).Reconcile(ctx))
	live, _ := st.Sessions(ctx, false)
	if len(live) != 1 || live[0].State != "running" {
		t.Fatalf("claim failed with the notifier: %+v", live)
	}
	_, err = st.Release(ctx, b.ID, "retry", store.Expect{}, "t")
	mustT(t, err)
	mustT(t, daemon(st, s.show(nil)).Reconcile(ctx))
	if len(s.titles) != 1 {
		t.Fatalf("notification not retried after a failure: %v", s.titles)
	}
}

// Nobody attached to herdr: the notification is retried on the dispatcher's
// liveness passes until it is shown, then never again.
func TestManualRetriesUnshownNotification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := storetest.New(t)
	mustT(t, st.PutGroup(ctx, store.Group{Name: "humans", Runner: "manual", Config: map[string]any{"notify": true}}, "me"))
	_, err := st.AddRoute(ctx, store.Route{Stage: "review", Group: "humans"}, "me")
	mustT(t, err)
	b, err := st.CreateBead(ctx, store.NewBead{Title: "x", Stage: "review"}, "t")
	mustT(t, err)

	calls, fail := 0, errors.New("herdr did not show the notification: no_foreground_client")
	m := &Manual{Store: st, Show: func(context.Context, string, string) error {
		calls++
		return fail
	}}
	d := &dispatch.Dispatcher{Store: st, Runners: map[string]dispatch.Runner{"manual": m}}
	mustT(t, d.Reconcile(ctx))
	mustT(t, d.Reconcile(ctx))
	if calls != 2 {
		t.Fatalf("unshown notification tried %d times over two passes, want 2", calls)
	}
	fail = nil
	mustT(t, d.Reconcile(ctx))
	mustT(t, d.Reconcile(ctx))
	if calls != 3 {
		t.Fatalf("shown notification repeated: %d calls, want 3", calls)
	}
	// A restarted daemon checks the store once and stays quiet.
	m2 := &Manual{Store: st, Show: func(context.Context, string, string) error { calls++; return nil }}
	d2 := &dispatch.Dispatcher{Store: st, Runners: map[string]dispatch.Runner{"manual": m2}}
	mustT(t, d2.Reconcile(ctx))
	if calls != 3 {
		t.Fatalf("restarted daemon notified again: %d calls", calls)
	}
	evs, _ := st.Events(ctx, b.ID, 100)
	n := 0
	for _, e := range evs {
		if e.Kind == Notified {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d %s events, want 1", n, Notified)
	}
}

// Notifications disabled in herdr will not change by retrying.
func TestManualGivesUpWhenNotificationsOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := storetest.New(t)
	mustT(t, st.PutGroup(ctx, store.Group{Name: "humans", Runner: "manual", Config: map[string]any{"notify": true}}, "me"))
	_, err := st.AddRoute(ctx, store.Route{Stage: "review", Group: "humans"}, "me")
	mustT(t, err)
	_, err = st.CreateBead(ctx, store.NewBead{Title: "x", Stage: "review"}, "t")
	mustT(t, err)
	calls := 0
	m := &Manual{Store: st, Show: func(context.Context, string, string) error {
		calls++
		return fmt.Errorf("herdr: %w in its config", ErrNotifyOff)
	}}
	d := &dispatch.Dispatcher{Store: st, Runners: map[string]dispatch.Runner{"manual": m}}
	mustT(t, d.Reconcile(ctx))
	mustT(t, d.Reconcile(ctx))
	if calls != 1 {
		t.Fatalf("disabled notifications retried: %d calls", calls)
	}
}
