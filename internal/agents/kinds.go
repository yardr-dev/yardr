package agents

import "yardr.dev/yardr/internal/store"

// HerdrKinds are the agent kinds herdr 0.9 can start (herdr agent start
// --help); a harness file is named for one of them, and a custom kind must be
// one of them.
var HerdrKinds = []string{"pi", "claude", "codex", "gemini", "cursor", "devin", "agy", "cline", "omp",
	"mastracode", "opencode", "copilot", "kimi", "kiro", "droid", "amp", "grok", "hermes", "kilo",
	"qodercli", "qwen", "letta", "maki", "muse"}

// executables are the kinds whose command is not named after the kind.
var executables = map[string]string{"cursor": "cursor-agent", "kiro": "kiro-cli"}

// Executable is the command herdr runs for kind.
func Executable(kind string) string {
	if e, ok := executables[kind]; ok {
		return e
	}
	return kind
}

// Catalogue is what a store is opened with: the harness files as Load read
// them, the kinds herdr starts, the command of each, and the kind a config
// that names none runs. The store holds no copy of any of it.
func Catalogue() store.Catalogue {
	return store.Catalogue{Harnesses: Harnesses, Kinds: HerdrKinds, Executable: Executable, DefaultKind: DefaultKind}
}
