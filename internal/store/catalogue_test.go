package store

import (
	"bytes"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.yaml.in/yaml/v3"

	"yardr.dev/yardr/harnesses"
)

// testCatalogue is the catalogue a test inside this package opens a store
// with. agents builds yardr's own from the same files, but imports this
// package, so a test here reads them itself, as agents.Load does: each
// decoded with every unknown key refused, checked, and of the kind its file
// is named for. The kinds are the files' alone and a kind's program is its
// own name, so the fixture is no copy of what a file says: a file that
// changes is read as it is.
//
// Each call returns a catalogue of its own, a test's doctored copy no other
// test's: it may replace a harness of Harnesses or a field of one, but not
// change a slice a harness holds, which the copies share.
func testCatalogue() Catalogue {
	built := testHarnesses()
	cat := Catalogue{Harnesses: slices.Clone(built), Executable: func(kind string) string { return kind }, DefaultKind: "claude"}
	for _, h := range built {
		cat.Kinds = append(cat.Kinds, h.Kind)
	}
	return cat
}

// testHarnesses reads the files built into the binary once for the whole
// run.
var testHarnesses = sync.OnceValue(func() []Harness {
	files, err := fs.Glob(harnesses.FS, "*.yaml")
	if err != nil {
		panic(err)
	}
	var out []Harness
	for _, file := range files {
		body, err := fs.ReadFile(harnesses.FS, file)
		if err != nil {
			panic(err)
		}
		h := Harness{Kind: strings.TrimSuffix(file, ".yaml")}
		dec := yaml.NewDecoder(bytes.NewReader(body))
		dec.KnownFields(true)
		if err := dec.Decode(&h); err != nil {
			panic(fmt.Sprintf("%s: %v", file, err))
		}
		if err := h.Check(); err != nil {
			panic(fmt.Sprintf("%s: %v", file, err))
		}
		out = append(out, h)
	}
	return out
})

// openStore is Open with testCatalogue, for a test that opens a store by its
// path and, as Open does, looks at the error itself.
func openStore(t testing.TB, path string) (*Store, error) {
	t.Helper()
	return Open(path, testCatalogue())
}

// The catalogue answers what the store asks of a kind from its harnesses
// alone: of two harnesses this test invents and a kind herdr starts that has
// no file, so no answer is one a file's own words happen to give.
func TestCatalogueAnswers(t *testing.T) {
	t.Parallel()
	alpha := Harness{Kind: "alpha", Version: FileVersion, Label: "Alpha", LoginEnv: []string{"ALPHA_KEY"},
		Flags: Flags{Model: "--model", Effort: &Effort{Args: []string{"--effort", LevelArg}, Levels: []string{"low", "high"}},
			Permissions: []Permission{{Label: "ask less", Args: []string{"--less"}}, {Label: "never ask", Args: []string{"--never"}, Strong: true}}},
		Offers: []Offer{{Model: "a1", Label: "A1", Recommended: "yardmaster"}, {Model: "a2", Label: "A2"}}}
	beta := Harness{Kind: "beta", Version: FileVersion, Label: "Beta",
		Flags:  Flags{ModelEnv: &ModelEnv{Var: "BETA_MODEL", Value: "{model}"}, Permissions: []Permission{{Label: "never ask", Args: []string{"--no-ask"}, Strong: true}}},
		Offers: []Offer{{Model: "b1", Label: "B1", Recommended: "yardmaster"}, {Model: "b2", Label: "B2"}}}
	cat := Catalogue{Harnesses: []Harness{beta, alpha}, Kinds: []string{"alpha", "beta", "gamma"}, Executable: func(kind string) string { return kind + "-cli" }, DefaultKind: "alpha"}
	if err := cat.check(); err != nil {
		t.Fatal(err)
	}
	indexed := cat.indexed()
	if got := indexed.label("alpha"); got != "Alpha" || indexed.label("gamma") != "" || indexed.of("gamma") != nil {
		t.Errorf("indexed catalogue labels: alpha %q, gamma %q", got, indexed.label("gamma"))
	}
	if p, ok := indexed.preset("beta", "never-ask"); !ok || !p.Strong {
		t.Errorf("indexed catalogue preset(beta, never-ask) = %+v, %v", p, ok)
	}
	if cat.label("alpha") != "Alpha" || cat.label("gamma") != "" || !cat.known("beta") || cat.known("gamma") || cat.known("nope") {
		t.Errorf("labels and kinds with a file: alpha %q, gamma %q, beta known %v, gamma known %v", cat.label("alpha"), cat.label("gamma"), cat.known("beta"), cat.known("gamma"))
	}
	if got := cat.harnessKinds(); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Errorf("harnessKinds = %v, want the kinds with a file, by name", got)
	}
	if !slices.Equal(cat.loginEnv("alpha"), []string{"ALPHA_KEY"}) || cat.loginEnv("beta") != nil || cat.loginEnv("gamma") != nil {
		t.Errorf("loginEnv: alpha %v, beta %v, gamma %v", cat.loginEnv("alpha"), cat.loginEnv("beta"), cat.loginEnv("gamma"))
	}
	// A preset is found in either spelling, with its mark, and only in its own kind.
	if p, ok := cat.preset("alpha", "never-ask"); !ok || !p.Strong || !slices.Equal(p.Args, []string{"--never"}) {
		t.Errorf("preset(alpha, never-ask) = %+v, %v", p, ok)
	}
	if p, ok := cat.preset("alpha", "Ask Less"); !ok || p.Strong || p.Label != "ask less" {
		t.Errorf("preset(alpha, Ask Less) = %+v, %v", p, ok)
	}
	if _, ok := cat.preset("beta", "ask less"); ok {
		t.Error("beta has the preset of alpha")
	}
	if _, ok := cat.preset("gamma", "never ask"); ok {
		t.Error("a kind with no file has a preset")
	}
	if got := cat.presetLabels("alpha"); !slices.Equal(got, []string{"ask less", "never ask"}) {
		t.Errorf("presetLabels(alpha) = %v, want the labels as the file writes them, in its order", got)
	}
	if got := cat.presetNames(); !slices.Equal(got, []string{"ask-less", "never-ask"}) {
		t.Errorf("presetNames = %v, want each name once, in its one-word form, by name", got)
	}
	if !slices.Equal(cat.offers("beta"), []string{"b1", "b2"}) || cat.offers("gamma") != nil || !slices.Equal(cat.offeringKinds(), []string{"alpha", "beta"}) {
		t.Errorf("offers: beta %v, gamma %v, kinds %v", cat.offers("beta"), cat.offers("gamma"), cat.offeringKinds())
	}
	if e, ok := cat.effort("alpha"); !ok || !slices.Equal(e.Levels, []string{"low", "high"}) {
		t.Errorf("effort(alpha) = %+v, %v", e, ok)
	}
	if _, ok := cat.effort("beta"); ok || !slices.Equal(cat.effortKinds(), []string{"alpha"}) {
		t.Errorf("effort(beta) known: %v, kinds %v", ok, cat.effortKinds())
	}
	// A kind with no model flag is one with a file that names none: with no
	// file it takes the runner's own.
	if cat.noModelFlag("alpha") || !cat.noModelFlag("beta") || cat.noModelFlag("gamma") {
		t.Errorf("noModelFlag: alpha %v, beta %v, gamma %v", cat.noModelFlag("alpha"), cat.noModelFlag("beta"), cat.noModelFlag("gamma"))
	}
	if e, ok := cat.modelEnv("beta"); !ok || e.Var != "BETA_MODEL" {
		t.Errorf("modelEnv(beta) = %+v, %v", e, ok)
	}
	if _, ok := cat.modelEnv("alpha"); ok {
		t.Error("alpha takes its model in its environment")
	}
	if !cat.isKind("gamma") || cat.isKind("delta") {
		t.Errorf("isKind: gamma %v, delta %v", cat.isKind("gamma"), cat.isKind("delta"))
	}
	if got := cat.configKind(nil); got != "alpha" {
		t.Errorf("a config that names no kind runs %q, want the default kind", got)
	}
	if got := cat.configKind(map[string]any{"kind": "beta"}); got != "beta" {
		t.Errorf("a config that names beta runs %q", got)
	}
	if got := cat.configKind(map[string]any{"kind": ""}); got != "alpha" {
		t.Errorf("a config that names an empty kind runs %q, want the default kind", got)
	}
}
