package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoCatalogue is what Open refuses a catalogue that is not complete with:
// the store reads what the harness files say from its catalogue alone, so one
// without harnesses, kinds, a program for each or a default kind would leave
// every question about a kind unanswered.
var ErrNoCatalogue = errors.New("no harness catalogue")

// Catalogue is what a store is told of the harness kinds when it is opened:
// the harness files and the kinds herdr starts, which it holds a group's or a
// crew member's kind, preset, model and effort to and names in the cards it
// writes. The store keeps no copy of what the files say. internal/agents
// reads them and builds the catalogue (agents.Catalogue), since the store may
// not import it; a test inside this package reads them itself (testCatalogue).
type Catalogue struct {
	// Harnesses are the harness files built into the binary, as agents.Load
	// read them: one for each kind yardr knows more of than herdr does.
	Harnesses []Harness
	// Kinds are the agent kinds herdr starts. A harness is of one of them.
	Kinds []string
	// Executable is the command herdr runs for a kind.
	Executable func(kind string) string
	// DefaultKind is the kind a config that names none runs.
	DefaultKind string
	// byKind is built when Open takes the catalogue. Callers that have not
	// opened a store, such as setting validation, read the harnesses directly.
	byKind map[string]int
}

// check refuses what a store cannot be opened with, naming the part that is
// missing.
func (c Catalogue) check() error {
	switch {
	case len(c.Harnesses) == 0:
		return fmt.Errorf("%w: it holds no harnesses", ErrNoCatalogue)
	case len(c.Kinds) == 0:
		return fmt.Errorf("%w: it holds no kinds herdr starts", ErrNoCatalogue)
	case c.Executable == nil:
		return fmt.Errorf("%w: it names no way to find the program of a kind", ErrNoCatalogue)
	case c.DefaultKind == "":
		return fmt.Errorf("%w: it names no default kind", ErrNoCatalogue)
	}
	for _, h := range c.Harnesses {
		if !slices.Contains(c.Kinds, h.Kind) {
			return fmt.Errorf("%w: the harness %s is of no kind herdr starts", ErrNoCatalogue, h.Kind)
		}
	}
	return nil
}

// catalogue is the catalogue the write's store was opened with. The hand-made
// wtx of a migration has no store and asks for none, as it asks for no
// policy (Store.may); it reads as an empty one.
func (w *wtx) catalogue() Catalogue {
	if w.store == nil {
		return Catalogue{}
	}
	return w.store.cat
}

// indexed returns a catalogue with a kind index for the store to keep.
func (c Catalogue) indexed() Catalogue {
	c.byKind = make(map[string]int, len(c.Harnesses))
	for i := range c.Harnesses {
		if _, exists := c.byKind[c.Harnesses[i].Kind]; !exists {
			c.byKind[c.Harnesses[i].Kind] = i
		}
	}
	return c
}

// of is the harness file of kind, nil for a kind the catalogue has none for.
// Only the kind itself names it, never an alias. It is read, not changed.
func (c Catalogue) of(kind string) *Harness {
	if c.byKind != nil {
		i, ok := c.byKind[kind]
		if !ok || i < 0 || i >= len(c.Harnesses) {
			return nil
		}
		return &c.Harnesses[i]
	}
	for i := range c.Harnesses {
		if c.Harnesses[i].Kind == kind {
			return &c.Harnesses[i]
		}
	}
	return nil
}

// known reports whether the catalogue has a harness file for kind.
func (c Catalogue) known(kind string) bool { return c.of(kind) != nil }

// label is the label of kind's harness file, as the file writes it; "" for a
// kind with none.
func (c Catalogue) label(kind string) string {
	if h := c.of(kind); h != nil {
		return h.Label
	}
	return ""
}

// harnessKinds are the kinds the catalogue has a file for, by name.
func (c Catalogue) harnessKinds() []string {
	out := make([]string, len(c.Harnesses))
	for i, h := range c.Harnesses {
		out[i] = h.Kind
	}
	slices.Sort(out)
	return out
}

// loginEnv are the API-key variables kind's file names; nil when it names none.
func (c Catalogue) loginEnv(kind string) []string {
	if h := c.of(kind); h != nil {
		return h.LoginEnv
	}
	return nil
}

// presets are kind's permission presets as its file writes them, in file
// order; nil for a kind with none.
func (c Catalogue) presets(kind string) []Permission {
	if h := c.of(kind); h != nil {
		return h.Flags.Permissions
	}
	return nil
}

// preset is kind's permission preset called label, in either spelling
// (PresetLabel or as the file writes it). None is no preset.
func (c Catalogue) preset(kind, label string) (Permission, bool) {
	want := PresetLabel(label)
	for _, p := range c.presets(kind) {
		if PresetLabel(p.Label) == want {
			return p, true
		}
	}
	return Permission{}, false
}

// presetLabels are kind's permission presets' labels as its file writes
// them, in file order.
func (c Catalogue) presetLabels(kind string) []string {
	var out []string
	for _, p := range c.presets(kind) {
		out = append(out, p.Label)
	}
	return out
}

// presetNames are the permission presets some harness offers, each in its
// one-word form (PresetLabel), by name.
func (c Catalogue) presetNames() []string {
	var out []string
	for _, h := range c.Harnesses {
		for _, p := range h.Flags.Permissions {
			if name := PresetLabel(p.Label); !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	slices.Sort(out)
	return out
}

// offers are the model ids kind's file offers a person to pick from, in file
// order; nil for a kind that offers none.
func (c Catalogue) offers(kind string) []string {
	h := c.of(kind)
	if h == nil {
		return nil
	}
	var out []string
	for _, o := range h.Offers {
		out = append(out, o.Model)
	}
	return out
}

// offeringKinds are the kinds whose file offers models, by name.
func (c Catalogue) offeringKinds() []string {
	var out []string
	for _, h := range c.Harnesses {
		if len(h.Offers) > 0 {
			out = append(out, h.Kind)
		}
	}
	slices.Sort(out)
	return out
}

// effort is how kind takes a reasoning effort, as its file says it; false for
// a kind whose file names none.
func (c Catalogue) effort(kind string) (Effort, bool) {
	if h := c.of(kind); h != nil && h.Flags.Effort != nil {
		return *h.Flags.Effort, true
	}
	return Effort{}, false
}

// effortKinds are the kinds that take an effort, by name.
func (c Catalogue) effortKinds() []string {
	var out []string
	for _, h := range c.Harnesses {
		if h.Flags.Effort != nil {
			out = append(out, h.Kind)
		}
	}
	slices.Sort(out)
	return out
}

// noModelFlag reports whether kind has a file that names no model flag: no flag
// gives its agents a model. A kind with no file is not one, since it takes the
// runner's own --model.
func (c Catalogue) noModelFlag(kind string) bool {
	h := c.of(kind)
	return h != nil && h.Flags.Model == ""
}

// modelEnv is how kind takes a model in its pane's environment, as its file
// says it; false for a kind whose file names none.
func (c Catalogue) modelEnv(kind string) (ModelEnv, bool) {
	if h := c.of(kind); h != nil && h.Flags.ModelEnv != nil {
		return *h.Flags.ModelEnv, true
	}
	return ModelEnv{}, false
}

// isKind reports whether herdr starts kind.
func (c Catalogue) isKind(kind string) bool { return slices.Contains(c.Kinds, kind) }

// configKind is the kind a herdr config runs: the one it names, else the one
// the runner starts for a config that names none.
func (c Catalogue) configKind(cfg map[string]any) string {
	if kind, _ := cfg["kind"].(string); kind != "" {
		return kind
	}
	return c.DefaultKind
}

// Valid refuses a value the setting's readers would not take: what Check
// holds it to, then what the catalogue does (Catalogued).
func (set Setting) Valid(cat Catalogue, v string) error {
	if set.Check != nil {
		if err := set.Check(v); err != nil {
			return err
		}
	}
	if set.Catalogued != nil {
		return set.Catalogued(cat, v)
	}
	return nil
}

// checkDefaultKind holds default_kind to the kinds herdr starts.
func checkDefaultKind(cat Catalogue, v string) error {
	if !cat.isKind(v) {
		return fmt.Errorf("%s %q: herdr starts %s", DefaultKindSetting, v, strings.Join(cat.Kinds, ", "))
	}
	return nil
}

// checkPermissionsPreset takes what names a permission preset of some
// harness: the one-word form of a preset's label (PresetLabel), as
// yardr init --permissions takes it. Which presets the yard's own harness
// offers is init's to hold the value to; this holds a stored value to a
// preset some harness knows, so a misspelling is refused where it is set
// instead of read as no preset later.
func checkPermissionsPreset(cat Catalogue, v string) error {
	names := cat.presetNames()
	if slices.Contains(names, PresetLabel(v)) {
		return nil
	}
	return fmt.Errorf("%s %q: no harness has a permission preset of that name (there is: %s); yardr init --permissions names what the yard's harness offers", PermissionsPresetSetting, v, strings.Join(names, ", "))
}
