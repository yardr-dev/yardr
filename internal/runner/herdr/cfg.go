package herdr

import "yardr.dev/yardr/internal/config"

// str reads a string of a group's or crew member's config. internal/runner has
// the same one, unexported: it is three lines, so a copy costs less than a
// name exported for one other package to call. strs is only this package's.

func str(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
	}
	return def
}

func strs(cfg map[string]any, key string) []string {
	return config.StringList(cfg[key])
}
