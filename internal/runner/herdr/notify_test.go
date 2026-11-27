package herdr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/runner"
)

// herdr exits 0 with shown=false when it showed nothing; that must not
// count as notified.
func TestHerdrNotifierChecksShown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := filepath.Join(t.TempDir(), "calls")
	bin := fakeHerdrBin(t, `printf '%s\n' "$@" >> `+log+`
case "$3" in
  shown)    echo '{"id":"x","result":{"reason":"shown","shown":true,"type":"notification_show"}}' ;;
  nobody)   echo '{"id":"x","result":{"reason":"no_foreground_client","shown":false,"type":"notification_show"}}' ;;
  disabled) echo '{"id":"x","result":{"reason":"disabled","shown":false,"type":"notification_show"}}' ;;
  *)        echo '{"error":{"code":"server_not_running","message":"no herdr server"},"id":"x"}' >&2; exit 1 ;;
esac`)
	show := Notifier(bin)
	if err := show(ctx, "shown", "line 1\nline 2"); err != nil {
		t.Fatalf("shown: %v", err)
	}
	if err := show(ctx, "nobody", "b"); err == nil || errors.Is(err, runner.ErrNotifyOff) ||
		!strings.Contains(err.Error(), "no_foreground_client") {
		t.Fatalf("no client: %v", err)
	}
	if err := show(ctx, "disabled", "b"); !errors.Is(err, runner.ErrNotifyOff) {
		t.Fatalf("disabled: %v", err)
	}
	if err := show(ctx, "down", "b"); err == nil {
		t.Fatal("herdr down not reported")
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "notification\nshow\nshown\n--body\nline 1\nline 2\n--sound\nrequest") {
		t.Fatalf("argv:\n%s", calls)
	}
}
