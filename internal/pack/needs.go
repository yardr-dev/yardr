package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/source"
	"yardr.dev/yardr/internal/store"
)

// Kinds of a line need.
const (
	LineNeedTool      = "tool"
	LineNeedConnector = "connector"
)

// Status words of a checked line need.
const (
	NeedOK             = "ok"
	NeedMissing        = "missing"
	NeedNotCheckedHere = "not checked here"
	NeedNotRun         = "not run"
)

// CheckNotRunDetail is shown beside a tool need with a check in places that
// do not execute checks (pack check, pack show, and a card's plan).
const CheckNotRunDetail = "its check runs when the line is applied"

// LineNeedCheckTimeout bounds one run of a tool need's check command. A
// variable so tests need not wait it out.
var LineNeedCheckTimeout = 30 * time.Second

// MaxLineNeedCheckOutput caps stdout read from a tool need's check command.
const MaxLineNeedCheckOutput = 8 << 10

// LineNeed is one tool or connector a line says it needs (pack.yaml's needs).
// Either Name is set (a tool, with optional Any, Env and Check) or Connector
// is set (a declared source name).
type LineNeed struct {
	Name      string   `json:"name,omitempty"`
	Any       []string `json:"any,omitempty"`
	Env       string   `json:"env,omitempty"`
	Check     []string `json:"check,omitempty"`
	Connector string   `json:"connector,omitempty"`
}

// Kind is LineNeedConnector when Connector is set, else LineNeedTool.
func (n LineNeed) Kind() string {
	if n.Connector != "" {
		return LineNeedConnector
	}
	return LineNeedTool
}

// Target is the tool or connector name.
func (n LineNeed) Target() string {
	if n.Connector != "" {
		return n.Connector
	}
	return n.Name
}

// Word is the single-word form used by yardr pack needs add and rm: the tool
// name, or "connector:<name>".
func (n LineNeed) Word() string {
	if n.Connector != "" {
		return "connector:" + n.Connector
	}
	return n.Name
}

// SourceNeed converts a tool LineNeed to a source.Need so NeedPath shares one
// lookup code path between connectors and lines.
func (n LineNeed) SourceNeed() source.Need {
	return source.Need{Program: n.Name, Env: n.Env, Any: n.Any}
}

// LineNeedStatus is the checked state of one need of a line.
type LineNeedStatus struct {
	Need   string `json:"need"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Check is the argv an apply will run, shown where it has not run yet so
	// that whoever says yes has seen the command.
	Check []string `json:"check,omitempty"`
}

// needs reads pack.yaml's root needs key: a non-empty list of tools (bare
// name or mapping with name and optional any, env, check) and connectors
// ({connector: <name>}). Requires api: 9 or higher and is refused on kind
// source (which keeps its declaration's own needs).
func (r *reader) needs(p *Pack, e *entry) []LineNeed {
	if e == nil || p.Kind == KindSource {
		return nil
	}
	if p.API < 9 {
		r.errf(e.k, e.path, "needs requires api: 9 or higher (this pack says api: %d; docs/pack-api.md)", p.API)
	}
	if e.v.Kind != yaml.SequenceNode {
		r.errf(e.v, e.path, "want a list of tools and connectors, got %s", describe(e.v))
		return nil
	}
	if len(e.v.Content) == 0 {
		r.errf(e.v, e.path, "empty: list at least one tool or connector, or leave needs out")
		return nil
	}
	var out []LineNeed
	seen := map[string]bool{}
	for i, item := range e.v.Content {
		itemPath := fmt.Sprintf("%s.%d", e.path, i)
		n, ok := r.lineNeed(item, itemPath)
		if !ok {
			continue
		}
		key := n.Kind() + "\x00" + n.Target()
		if seen[key] {
			r.errf(item, itemPath, "%s %s is listed twice", n.Kind(), n.Target())
			continue
		}
		seen[key] = true
		out = append(out, n)
	}
	return out
}

func (r *reader) lineNeed(item *yaml.Node, path string) (LineNeed, bool) {
	switch item.Kind {
	case yaml.ScalarNode:
		s, ok := r.str(item, path)
		if !ok {
			return LineNeed{}, false
		}
		if !r.checkToolName(item, path, s) {
			return LineNeed{}, false
		}
		return LineNeed{Name: s}, true
	case yaml.MappingNode:
		if hasKey(item, "connector") {
			var conn string
			for _, f := range r.entries(item, path, "connector") {
				if s, ok := r.str(f.v, f.path); ok {
					if !validName.MatchString(s) {
						r.errf(f.v, f.path, "connector %q: use letters, digits, '.', '_' or '-'", s)
					} else {
						conn = s
					}
				}
			}
			if conn == "" {
				return LineNeed{}, false
			}
			return LineNeed{Connector: conn}, true
		}
		var n LineNeed
		hasName := false
		for _, f := range r.entries(item, path, "name", "any", "env", "check") {
			switch f.key {
			case "name":
				hasName = true
				if s, ok := r.str(f.v, f.path); ok && r.checkToolName(f.v, f.path, s) {
					n.Name = s
				}
			case "any":
				n.Any = r.needAny(f)
			case "env":
				if s, ok := r.str(f.v, f.path); ok {
					if !envName.MatchString(s) {
						r.errf(f.v, f.path, "env %q: use letters, digits and '_', and no digit first", s)
					} else {
						n.Env = s
					}
				}
			case "check":
				n.Check = r.needCheck(f)
			}
		}
		if !hasName {
			r.errf(item, join(path, "name"), "missing: name the tool this line needs (or use connector: <source>)")
		}
		if n.Name == "" {
			return LineNeed{}, false
		}
		return n, true
	default:
		r.errf(item, path, "want a tool name or a mapping, got %s", describe(item))
		return LineNeed{}, false
	}
}

func hasKey(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			return true
		}
	}
	return false
}

func validToolName(s string) string {
	switch {
	case s == "" || strings.TrimSpace(s) != s || !printableOneLine(s) || strings.ContainsFunc(s, unicode.IsSpace):
		return fmt.Sprintf("tool %q is not a program name: one word, on one line", s)
	case strings.ContainsAny(s, "/\\") || s == "." || s == "..":
		return fmt.Sprintf("tool %q is not a program name: name the program on PATH, not a path", s)
	}
	return ""
}

func (r *reader) checkToolName(n *yaml.Node, path, s string) bool {
	if msg := validToolName(s); msg != "" {
		r.errf(n, path, "%s", msg)
		return false
	}
	return true
}

func printableOneLine(s string) bool {
	return strings.IndexFunc(s, func(c rune) bool { return !unicode.IsPrint(c) }) < 0
}

func (r *reader) needAny(e entry) []string {
	if e.v.Kind != yaml.SequenceNode {
		r.errf(e.v, e.path, "want a list of program names or absolute paths, got %s", describe(e.v))
		return nil
	}
	if len(e.v.Content) == 0 {
		r.errf(e.v, e.path, "empty: list at least one program name or absolute path, or leave any out")
		return nil
	}
	var out []string
	for _, c := range e.v.Content {
		s, ok := r.str(c, e.path)
		if !ok {
			continue
		}
		if !printableOneLine(s) {
			i := strings.IndexFunc(s, func(ch rune) bool { return !unicode.IsPrint(ch) })
			ch, _ := utf8.DecodeRuneInString(s[i:])
			r.errf(c, e.path, "%q holds %U, which is not printable", s, ch)
			continue
		}
		if strings.HasPrefix(s, "/") {
			// Absolute paths may contain spaces (such as macOS app bundles).
		} else if msg := validToolName(s); msg != "" {
			r.errf(c, e.path, "%q must be a program name on PATH or an absolute path", s)
			continue
		}
		if slices.Contains(out, s) {
			r.errf(c, e.path, "%s is listed twice", s)
			continue
		}
		out = append(out, s)
	}
	return out
}

func (r *reader) needCheck(e entry) []string {
	if e.v.Kind != yaml.SequenceNode {
		r.errf(e.v, e.path, "want an argv list ([program, arg, ...]), got %s", describe(e.v))
		return nil
	}
	if len(e.v.Content) == 0 {
		r.errf(e.v, e.path, "empty: check is an argv list of at least one word, or leave check out")
		return nil
	}
	var out []string
	for i, c := range e.v.Content {
		if booleanLike(c) {
			r.errf(c, e.path, "YAML reads the bare word %s as a boolean, not a string: quote it (%q)", c.Value, c.Value)
			continue
		}
		if c.Kind != yaml.ScalarNode || c.Tag != "!!str" {
			r.errf(c, e.path, "want a string, got %s", describe(c))
			continue
		}
		if i == 0 && strings.TrimSpace(c.Value) == "" {
			r.errf(c, e.path, "check program is empty")
			continue
		}
		out = append(out, c.Value)
	}
	return out
}

// ParseLineNeedWord parses one word for yardr pack needs <line> add|rm <need>:
// either "connector:<source>" or a bare tool name.
func ParseLineNeedWord(word string) (LineNeed, error) {
	if name, ok := strings.CutPrefix(word, "connector:"); ok {
		if !validName.MatchString(name) {
			return LineNeed{}, fmt.Errorf("connector %q: use letters, digits, '.', '_' or '-'", name)
		}
		return LineNeed{Connector: name}, nil
	}
	if msg := validToolName(word); msg != "" {
		return LineNeed{}, fmt.Errorf("%s", msg)
	}
	return LineNeed{Name: word}, nil
}

// MarshalLineNeeds encodes needs as the JSON text kept in store.PackImport.Needs.
// An empty list returns "" so records of lines with no needs omit the field.
func MarshalLineNeeds(needs []LineNeed) (string, error) {
	if len(needs) == 0 {
		return "", nil
	}
	b, err := json.Marshal(needs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalLineNeeds decodes the JSON text kept in store.PackImport.Needs.
func UnmarshalLineNeeds(raw string) ([]LineNeed, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []LineNeed
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("reading recorded line needs: %w", err)
	}
	return out, nil
}

// AddLineNeed appends need to a record's Needs JSON unless a need of the same
// kind and name is already present.
func AddLineNeed(raw string, need LineNeed) (updated string, changed bool, err error) {
	list, err := UnmarshalLineNeeds(raw)
	if err != nil {
		return "", false, err
	}
	if slices.ContainsFunc(list, func(n LineNeed) bool { return n.Kind() == need.Kind() && n.Target() == need.Target() }) {
		return raw, false, nil
	}
	list = append(list, need)
	b, err := json.Marshal(list)
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

// RemoveLineNeed removes need (matched by kind and name, including a tool with
// any, env or check) from a record's Needs JSON. Removing the last entry
// writes "[]" rather than "" so a later apply preserves the yard's empty list
// instead of restoring the line file's defaults.
func RemoveLineNeed(raw string, need LineNeed) (updated string, changed bool, err error) {
	list, err := UnmarshalLineNeeds(raw)
	if err != nil {
		return "", false, err
	}
	i := slices.IndexFunc(list, func(n LineNeed) bool { return n.Kind() == need.Kind() && n.Target() == need.Target() })
	if i < 0 {
		return raw, false, nil
	}
	list = slices.Delete(list, i, i+1)
	if list == nil {
		list = []LineNeed{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

// CheckLineNeeds evaluates needs on this machine. When peerOnly is true,
// every need is reported as "not checked here". When runChecks is false,
// a found tool with a check command is reported as "not run" with
// CheckNotRunDetail and nothing is executed.
func CheckLineNeeds(ctx context.Context, st store.API, needs []LineNeed, peerOnly, runChecks bool) ([]LineNeedStatus, error) {
	if len(needs) == 0 {
		return nil, nil
	}
	var srcs []store.Source
	if !peerOnly && slices.ContainsFunc(needs, func(n LineNeed) bool { return n.Kind() == LineNeedConnector }) {
		var err error
		if srcs, err = st.Sources(ctx); err != nil {
			return nil, err
		}
	}
	return checkLineNeedsWith(ctx, srcs, needs, peerOnly, runChecks), nil
}

func checkLineNeedsWith(ctx context.Context, srcs []store.Source, needs []LineNeed, peerOnly, runChecks bool) []LineNeedStatus {
	if len(needs) == 0 {
		return nil
	}
	haveSource := map[string]bool{}
	for _, s := range srcs {
		haveSource[s.Name] = true
	}
	out := make([]LineNeedStatus, 0, len(needs))
	for _, n := range needs {
		if peerOnly {
			out = append(out, LineNeedStatus{Need: n.Target(), Kind: n.Kind(), Status: NeedNotCheckedHere})
			continue
		}
		if n.Kind() == LineNeedConnector {
			if haveSource[n.Connector] {
				out = append(out, LineNeedStatus{Need: n.Connector, Kind: LineNeedConnector, Status: NeedOK})
			} else {
				out = append(out, LineNeedStatus{Need: n.Connector, Kind: LineNeedConnector, Status: NeedMissing, Detail: "yardr pack apply " + n.Connector})
			}
			continue
		}
		if !source.NeedPresent(n.SourceNeed()) {
			out = append(out, LineNeedStatus{Need: n.Name, Kind: LineNeedTool, Status: NeedMissing})
			continue
		}
		if len(n.Check) == 0 {
			out = append(out, LineNeedStatus{Need: n.Name, Kind: LineNeedTool, Status: NeedOK})
			continue
		}
		if !runChecks {
			out = append(out, LineNeedStatus{Need: n.Name, Kind: LineNeedTool, Status: NeedNotRun, Detail: CheckNotRunDetail, Check: n.Check})
			continue
		}
		out = append(out, runLineNeedCheck(ctx, n))
	}
	return out
}

func runLineNeedCheck(ctx context.Context, n LineNeed) LineNeedStatus {
	cmd := config.Command{
		What:    "check of " + n.Name,
		Argv:    n.Check,
		Env:     (&source.Declaration{}).CommandEnv(""),
		Timeout: LineNeedCheckTimeout,
		Max:     MaxLineNeedCheckOutput,
	}
	stdout, stderr, err := cmd.OutputWithStderr(ctx)
	if err != nil {
		return LineNeedStatus{Need: n.Name, Kind: LineNeedTool, Status: NeedMissing, Detail: firstStderrLine(stderr)}
	}
	return LineNeedStatus{Need: n.Name, Kind: LineNeedTool, Status: NeedOK, Detail: firstLine(string(stdout))}
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func firstStderrLine(stderr string) string {
	rest := strings.TrimPrefix(stderr, "; its stderr ended: ")
	if rest == "" {
		return ""
	}
	first, _, _ := strings.Cut(rest, " | ")
	return strings.TrimSpace(first)
}

// RecordGroupsPeerOnly reports whether every group of rec that exists in
// groups is worked by the peer runner. A line with no group (or none present
// in groups) returns false so its needs are checked on this machine.
func RecordGroupsPeerOnly(rec store.PackImport, groups []store.Group) bool {
	if len(rec.Groups) == 0 {
		return false
	}
	byName := map[string]store.Group{}
	for _, g := range groups {
		byName[g.Name] = g
	}
	found := 0
	for _, yardName := range rec.Groups {
		g, ok := byName[yardName]
		if !ok {
			continue
		}
		found++
		if g.Runner != store.PeerRunner {
			return false
		}
	}
	return found > 0
}

// CheckRecordNeeds evaluates the needs recorded on an applied line, running
// any check commands. Shared by yardr pack needs and yardr doctor.
func CheckRecordNeeds(ctx context.Context, srcs []store.Source, groups []store.Group, rec store.PackImport) ([]LineNeedStatus, error) {
	needs, err := UnmarshalLineNeeds(rec.Needs)
	if err != nil || len(needs) == 0 {
		return nil, err
	}
	peerOnly := RecordGroupsPeerOnly(rec, groups)
	return checkLineNeedsWith(ctx, srcs, needs, peerOnly, true), nil
}

// FormatLineNeed formats one LineNeedStatus in plain words. yardr is the
// yardr command prefix ("yardr" when empty, or "yardr --home <dir>").
func FormatLineNeed(s LineNeedStatus, yardr string) string {
	if yardr == "" {
		yardr = "yardr"
	}
	if s.Kind == LineNeedConnector {
		switch s.Status {
		case NeedMissing:
			return fmt.Sprintf("missing connector %s: %s pack apply %s", s.Need, yardr, s.Need)
		default:
			return fmt.Sprintf("connector %s: %s", s.Need, s.Status)
		}
	}
	switch s.Status {
	case NeedOK:
		if s.Detail != "" {
			return fmt.Sprintf("%s: ok %s", s.Need, s.Detail)
		}
		return fmt.Sprintf("%s: ok", s.Need)
	case NeedMissing:
		if s.Detail != "" {
			return fmt.Sprintf("%s: missing (%s)", s.Need, s.Detail)
		}
		return fmt.Sprintf("%s: missing", s.Need)
	case NeedNotRun:
		if s.Detail != "" {
			return fmt.Sprintf("%s: not run (%s)", s.Need, s.Detail)
		}
		return fmt.Sprintf("%s: not run", s.Need)
	default:
		return fmt.Sprintf("%s: %s", s.Need, s.Status)
	}
}

// FormatLineNeedLine formats one need status of an applied line (pack in
// depot), shared by yardr pack needs and yardr doctor so both print the same
// words for the same yard.
func FormatLineNeedLine(packName, depot string, s LineNeedStatus, yardr string) string {
	return fmt.Sprintf("line %s in %s: %s", packName, inDepot(depot), FormatLineNeed(s, yardr))
}
