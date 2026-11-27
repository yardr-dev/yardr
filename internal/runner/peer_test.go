package runner

import (
	"context"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// A yard without a key can send its peers nothing, which is the group's to
// be told of and no failure of the bead: the start is refused as blocked.
// Its session is one that waits. That it is one of the built-in runners is
// internal/cli's TestBuiltinRunners.
func TestPeerRunner(t *testing.T) {
	t.Parallel()
	r := &Peer{Home: t.TempDir()}
	if w, ok := dispatch.Runner(r).(dispatch.Waiter); !ok || w.WaitLimit() != PeerWait {
		t.Fatal("a peer session is not one that waits")
	}
	if alive, err := r.Alive(context.Background(), nil); !alive || err != nil {
		t.Fatalf("Alive = %v, %v", alive, err)
	}
	job := dispatch.Job{Bead: store.Bead{ID: "web-1"}, Group: store.Group{Name: "at-north", Runner: store.PeerRunner, Config: map[string]any{"peer": "north"}}}
	if _, err := r.Start(context.Background(), job); err == nil || !strings.Contains(err.Error(), "no store") {
		t.Fatalf("Start without a store: %v", err)
	}
	r.Store = storetest.New(t)
	if _, err := r.Start(context.Background(), job); !dispatch.IsBlocked(err) || !strings.Contains(err.Error(), "yardr peer key") {
		t.Fatalf("Start in a yard without a key: %v", err)
	}
}
