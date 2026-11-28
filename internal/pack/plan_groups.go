package pack

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
)

// groupName is the yard's group for the pack's g, and whether that group
// takes g as it is instead of being set by the pack: the one a flag names,
// else the one the last import recorded for it, else GroupName, the depot's
// own. A group the last import mapped g to and did not record among the
// pack's objects took it then, and does again while it still fits.
func (b *planner) groupName(g Group, have []store.Group) (name string, takes bool, err error) {
	own := GroupName(b.o.Depot, g)
	has := func(name string) *store.Group {
		if i := slices.IndexFunc(have, func(h store.Group) bool { return h.Name == name }); i >= 0 {
			return &have[i]
		}
		return nil
	}
	if to, told := b.o.Groups[g.Name]; told {
		// The import's own name for it is how a flag says no group takes
		// it: the depot gets its own group, which ends an earlier --group.
		// But a group the yard has under that name is the yard's, whatever
		// the flag names it for: it takes the pack's as it is, unless the
		// last import made it the pack's own.
		if to == own && (has(own) == nil || b.owned(own)) {
			return own, false, nil
		}
		h := has(to)
		switch {
		case h == nil:
			return "", false, fmt.Errorf("--group %s=%s: the yard has no group %s (it has, to take %s: %s)", g.Name, to, to, groupKind(g.runner()), or(strings.Join(fitting(g, have), ", "), "none"))
		case !Fits(g, *h):
			return "", false, fmt.Errorf("--group %s=%s: the pack's %s is %s, and %s is %s", g.Name, to, g.Name, groupKind(g.runner()), to, groupKind(h.Runner))
		}
		return to, true, nil
	}
	if b.rec != nil {
		if name := b.rec.Groups[g.Name]; name != "" && has(name) != nil {
			if b.owned(name) {
				return name, false, nil
			}
			if Fits(g, *has(name)) {
				return name, true, nil
			}
		}
	}
	return own, false, nil
}

// owned reports whether the last import of the pack into the depot made
// name the pack's own group, rather than one of the yard's taking it.
func (b *planner) owned(name string) bool {
	return b.rec != nil && slices.ContainsFunc(b.rec.Objects, func(po store.PackObject) bool { return po.Kind == "group" && po.Name == name })
}

// fitting are the names of the yard's groups that may take the pack's g.
func fitting(g Group, have []store.Group) []string {
	var out []string
	for _, h := range have {
		if Fits(g, h) {
			out = append(out, h.Name)
		}
	}
	return out
}

func or(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}

// taken is the plan's object for old, a group of the yard that takes the
// pack's g: the group as it stays, and under Keeps what the pack carries
// for g and does not set on it.
func (b *planner) taken(g Group, old store.Group) Object {
	o := Object{Kind: "group", Name: old.Name, Action: Takes, Group: &old}
	const why = ": the group is the yard's, and the pack's %s is not set on it"
	switch {
	case g.Agent != "":
		if prompt, _ := old.Config["prompt"].(string); prompt != b.installed(g.Agent) {
			o.Keeps = append(o.Keeps, fmt.Sprintf("prompt %s"+why, or(prompt, "(none)"), "role "+g.Agent))
		}
	case g.Run != "":
		cmd, _ := old.Config["cmd"].(string)
		if cmd != setScript(cmd, b.installed(g.Run)) {
			o.Keeps = append(o.Keeps, fmt.Sprintf("cmd %s"+why, or(cmd, "(none)"), "script "+g.Run))
		}
		o.Keeps = append(o.Keeps, keptEnv(g, cmd)...)
	}
	if g.Limit > 0 && g.Limit != old.Limit {
		o.Keeps = append(o.Keeps, fmt.Sprintf("limit %d"+why, old.Limit, fmt.Sprintf("limit %d", g.Limit)))
	}
	return o
}

// bindsAs settles a new agent group that binds as another group of the
// pack (Group.BindsAs) and names the yard's group it takes the agent from,
// or "" when it is asked about as any new agent group is: it binds as no
// other, the caller binds it itself (Options.Bound), or the group it binds
// as is no agent group of the yard. A group the yard has gives its agent
// kind, model and arguments as they are, copied into want; one this import
// makes is asked about, and Apply copies what the caller answers.
func (b *planner) bindsAs(g Group, have []store.Group, want *store.Group) string {
	if g.BindsAs == "" || slices.Contains(b.o.Bound, g.Name) {
		return ""
	}
	// Groups are planned in the pack's order, and the reader holds a group
	// to binding as one before it.
	name := b.plan.Groups[g.BindsAs]
	if i := slices.IndexFunc(have, func(h store.Group) bool { return h.Name == name }); i >= 0 {
		if have[i].Runner != "herdr" {
			return ""
		}
		for _, k := range []string{"kind", "model", "args"} {
			if v, ok := have[i].Config[k]; ok {
				want.Config[k] = v
			}
		}
		return name
	}
	if !slices.ContainsFunc(b.plan.Objects, func(o Object) bool {
		return o.Kind == "group" && o.Name == name && o.Group != nil && o.Group.Runner == "herdr"
	}) {
		return ""
	}
	return name
}

func (b *planner) groups() error {
	have, err := b.st.Groups(b.ctx)
	if err != nil {
		return err
	}
	settings, err := b.st.Settings(b.ctx)
	if err != nil {
		return err
	}
	for _, g := range b.p.Groups {
		name, takes, err := b.groupName(g, have)
		if err != nil {
			return err
		}
		b.plan.Groups[g.Name] = name
		if takes {
			b.add(b.taken(g, have[slices.IndexFunc(have, func(h store.Group) bool { return h.Name == name })]))
			continue
		}
		i := slices.IndexFunc(have, func(h store.Group) bool { return h.Name == name })
		if i >= 0 {
			old := have[i]
			o := Object{Kind: "group", Name: name, Group: &old}
			var changes []Diff
			if wantRunner := g.runner(); old.Runner != wantRunner {
				changes = append(changes, Diff{"runner", old.Runner, wantRunner})
			}
			switch {
			case g.Agent != "":
				installed := b.installed(g.Agent)
				own, err := b.depotRole(g.Agent)
				if err != nil {
					return err
				}
				prompt, _ := old.Config["prompt"].(string)
				if prompt != installed && (own == "" || prompt != g.Agent) {
					wantPrompt := installed
					if own != "" && b.plan.DepotRoles {
						wantPrompt = g.Agent
					}
					changes = append(changes, Diff{"prompt", prompt, wantPrompt})
				}
			case g.Run != "":
				cmd, _ := old.Config["cmd"].(string)
				if wantCmd := setScript(cmd, b.installed(g.Run)); cmd != wantCmd {
					changes = append(changes, Diff{"cmd", cmd, wantCmd})
				}
			}
			if g.Limit > 0 && g.Limit != old.Limit {
				changes = append(changes, Diff{"limit", old.Limit, g.Limit})
			}
			o.Action, o.Changes = kept(true, changes), changes
			b.setGroupDiff(&o, g, old)
			// A group the yard has keeps the last import's binding in the
			// record while the pack still says binds_as: the import binds
			// nothing anew, so the record must not change either, or no
			// re-import is ever unchanged.
			if o.Binds == "" && g.BindsAs != "" && !slices.Contains(b.o.Bound, g.Name) && b.rec != nil {
				if j := slices.IndexFunc(b.rec.Objects, func(po store.PackObject) bool { return po.Kind == "group" && po.Name == name }); j >= 0 {
					o.bindsKept = b.rec.Objects[j].Binds
				}
			}
			b.add(o)
			continue
		}
		want := store.Group{Name: name, Config: map[string]any{}}
		if g.Limit > 0 {
			want.Limit = g.Limit
		}
		o := Object{Kind: "group", Name: name}
		// A new group may be one the yard has instead. Not a script group:
		// its command is the pack's script, and nothing is asked for one.
		// Nor a group that binds as another, which carries no question
		// unless the caller binds it itself: --group names the one that
		// takes it.
		binds := g.BindsAs != "" && !slices.Contains(b.o.Bound, g.Name)
		if _, told := b.o.Groups[g.Name]; !told && g.Run == "" && !binds {
			if fit := fitting(g, have); len(fit) > 0 {
				b.ask(&o, "group", fmt.Sprintf("which group of the yard takes the pack's %s (--group %s=<name>)? The yard has: %s.", g.Name, g.Name, strings.Join(fit, ", ")), "a new group "+name)
			}
		}
		switch {
		case g.Agent != "":
			want.Runner = "herdr"
			want.Config["prompt"] = b.installed(g.Agent)
			own, err := b.depotRole(g.Agent)
			if err != nil {
				return err
			}
			switch {
			case own == "":
			case b.plan.DepotRoles:
				want.Config["prompt"] = g.Agent
				o.Keeps = append(o.Keeps, fmt.Sprintf("prompt %s: the depot has its own %s", g.Agent, own))
			default:
				o.Shadows = append(o.Shadows, own)
			}
			if o.Binds = b.bindsAs(g, have, &want); o.Binds == "" {
				b.ask(&o, "kind", "which agent runs group "+name+" (claude, codex, ...)?", settings["default_kind"])
				b.ask(&o, "model", "which model does group "+name+" run (empty: the agent's own default)?", "")
				b.ask(&o, "args", "which permission arguments does group "+name+"'s agent run with (empty: none, and it stops at every permission prompt)?", "")
			}
		case g.Run != "":
			want.Runner = "exec"
			// The assignments go before the script, where setScript keeps
			// what a cmd has: from here on they are the yard's.
			o.Env = g.EnvWords()
			want.Config["cmd"] = setScript(strings.Join(o.Env, " "), b.installed(g.Run))
		default:
			want.Runner = "manual"
			b.ask(&o, "members", "who is in group "+name+" (actors who may move a bead out of its stages; empty: anyone)?", "")
		}
		if g.Limit == 0 {
			// A pack that names no limit leaves it to the kind of group: a
			// people group is not held back to one bead at a time.
			want.Limit = store.DefaultLimit(want.Runner)
		}
		o.Action, o.Group = kept(false, nil), &want
		b.add(o)
	}
	return nil
}

// envNamePattern is what the shell takes for a variable's name. A cmd's
// assignments are told from its command by it, and a pack's env is held to
// it, so every variable a pack names is one that stays where the import
// puts it.
const envNamePattern = `[A-Za-z_][A-Za-z0-9_]*`

var envName = regexp.MustCompile(`^` + envNamePattern + `$`)

// setScript puts script in place of the command a group's cmd runs and
// keeps the rest: the variable assignments before it and any arguments
// after it. An empty cmd becomes the script alone.
func setScript(cmd, script string) string {
	script = shellQuote(script)
	if _, i, end := config.SplitCmd(cmd); i < end {
		return cmd[:i] + script + cmd[end:]
	}
	if strings.TrimSpace(cmd) == "" {
		return script
	}
	return strings.TrimRight(cmd, " \t") + " " + script
}

// EnvWords are the group's env as the assignment words of a cmd, names
// sorted: a map has no order, and a plan is the same every time it is
// made. A value is quoted as a script's path is, which config.WordEnd reads back.
func (g Group) EnvWords() []string {
	var words []string
	for _, name := range slices.Sorted(maps.Keys(g.Env)) {
		words = append(words, name+"="+shellQuote(g.Env[name]))
	}
	return words
}

// keptEnv says what of the pack's env a group the yard has does not carry,
// in a line for Keeps, or nothing when its cmd sets every variable to the
// pack's value. The pack's env is set on a group the import creates and on
// no other: once the group is made its assignments are the operator's, and
// an import of the same pack with another env must flip no yard. So the
// import never writes one, and the plan says what it leaves.
func keptEnv(g Group, cmd string) []string {
	assigns, _, _ := config.SplitCmd(cmd)
	have := map[string]string{}
	for _, w := range assigns {
		name, value, _ := strings.Cut(w, "=")
		if literal, ok := shellLiteral(value); ok {
			have[name] = literal
		}
	}
	var missing []string
	for _, name := range slices.Sorted(maps.Keys(g.Env)) {
		if v, ok := have[name]; !ok || v != g.Env[name] {
			missing = append(missing, name+"="+shellQuote(g.Env[name]))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("cmd %s: the pack's env %s is set on a new group only", or(cmd, "(none)"), strings.Join(missing, " "))}
}

// shellLiteral reads a shell word only when it has a fixed literal value.
// A yard's command may use expansions, but those do not prove that it sets
// the pack's value, so keptEnv reports them as missing instead of guessing.
func shellLiteral(word string) (string, bool) {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(word); i++ {
		c := word[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			} else {
				out.WriteByte(c)
			}
		case '"':
			switch c {
			case '"':
				quote = 0
			case '\\':
				if i+1 == len(word) {
					return "", false
				}
				next := word[i+1]
				if next == '\n' {
					return "", false
				}
				if strings.ContainsRune("$`\\\"", rune(next)) {
					i++
					out.WriteByte(next)
				} else {
					out.WriteByte(c)
				}
			case '$', '`':
				return "", false
			default:
				out.WriteByte(c)
			}
		default:
			switch c {
			case '\'':
				quote = '\''
			case '"':
				quote = '"'
			case '\\':
				if i+1 == len(word) || word[i+1] == '\n' {
					return "", false
				}
				i++
				out.WriteByte(word[i])
			case '$', '`':
				return "", false
			case '~':
				if i == 0 || word[i-1] == ':' {
					return "", false
				}
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		}
	}
	return out.String(), quote == 0
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
