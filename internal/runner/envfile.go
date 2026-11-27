package runner

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/otel"
	"yardr.dev/yardr/internal/store"
)

// EnvFile reads the env_file cfg names, if any, for the environment of a
// session of who, a herdr pane's or an exec command's. A file that is
// missing, not a regular file or open to others is refused as Blocked: the
// session must not start without its credentials, nor with credentials
// others can read. Only key names are logged.
func EnvFile(log *slog.Logger, cfg map[string]any, who string) ([]string, error) {
	path := str(cfg, "env_file", "")
	if path == "" {
		return nil, nil
	}
	f, err := store.ReadEnvFile(path)
	if err != nil {
		return nil, dispatch.Blocked(fmt.Errorf("%s: %w", who, err))
	}
	if log != nil {
		log.Debug("env_file loaded", "for", who, "path", f.Path, "keys", f.Keys())
	}
	return f.Env, nil
}

// WithEnvFile is env with the env file's entries added. A key yardr sets
// itself (the session's identity, the yard's home) is left out of the file's
// entries, so a credentials file cannot make an agent someone else. The one
// such key that is a list, OTEL_RESOURCE_ATTRIBUTES, keeps the file's
// attributes in front of yardr's: a group's env file is where an operator
// turns a harness's telemetry on, and may name attributes of its own.
func WithEnvFile(env, file []string) []string {
	set := make(map[string]int, len(env))
	out := slices.Clone(env)
	for i, e := range out {
		k, _, _ := strings.Cut(e, "=")
		set[k] = i
	}
	for _, e := range file {
		k, v, _ := strings.Cut(e, "=")
		i, ours := set[k]
		switch {
		case !ours:
			out = append(out, e)
		case k == otel.ResourceVar:
			if theirs := otel.ResourceAttributes(v); theirs != "" {
				_, mine, _ := strings.Cut(out[i], "=")
				out[i] = k + "=" + theirs + "," + mine
			}
		}
	}
	return out
}
