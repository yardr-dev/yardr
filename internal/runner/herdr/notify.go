package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"yardr.dev/yardr/internal/runner"
)

// Notify shows a notification through herdr, as Notifier does: the
// dispatcher tells through it beside yard-mail. It implements
// dispatch.Notifier.
func (h *Herdr) Notify(ctx context.Context, title, body string) error {
	return Notifier(h.Bin)(ctx, title, body)
}

// Notifier shows notifications with `herdr notification show`. herdr
// answers success even when it showed nothing, so its result is checked:
// only "shown" counts.
func Notifier(bin string) func(ctx context.Context, title, body string) error {
	return func(ctx context.Context, title, body string) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := HerdrCommand(ctx, bin, "notification", "show", title,
			"--body", body, "--sound", "request").CombinedOutput()
		if err != nil {
			return fmt.Errorf("herdr notification show: %w: %s", err, strings.TrimSpace(string(out)))
		}
		var resp struct {
			Result struct {
				Shown  bool   `json:"shown"`
				Reason string `json:"reason"`
			} `json:"result"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return fmt.Errorf("herdr notification show: unparseable output: %s", out)
		}
		switch {
		case resp.Result.Shown:
			return nil
		case resp.Result.Reason == "disabled":
			return fmt.Errorf("herdr: %w in its config", runner.ErrNotifyOff)
		}
		return fmt.Errorf("herdr did not show the notification: %s", resp.Result.Reason)
	}
}
