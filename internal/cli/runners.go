package cli

import (
	"log/slog"
	"os"
	"path/filepath"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/runner"
	"yardr.dev/yardr/internal/runner/herdr"
	"yardr.dev/yardr/internal/store"
)

// builtinRunners returns every built-in runner keyed by the name groups
// refer to. It is the one place the runner list is built: the daemon's
// start, yardr down and group add's check of a runner's name all call it, so
// a new runner is a package beside internal/runner/herdr and one entry here.
// st and log, if set, let manual groups notify humans; onError, if set,
// receives background errors (herdr event stream down).
func builtinRunners(home string, st store.API, log *slog.Logger, onError func(error)) map[string]dispatch.Runner {
	return map[string]dispatch.Runner{
		"manual": &runner.Manual{Store: st, Show: herdr.Notifier("herdr"), Log: log},
		"exec":   &runner.Exec{Home: home, Bin: selfPath(), Log: log},
		"herdr": &herdr.Herdr{Bin: "herdr", WorktreeRoot: config.At(home).Worktrees(), YardHome: home,
			YardBin: selfPath(), OnError: onError, Store: st, Log: log},
		store.PeerRunner: &runner.Peer{Store: st, Home: home},
	}
}

// selfPath is this yard's executable, so agents run the same version.
func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
