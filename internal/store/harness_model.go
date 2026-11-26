package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ModelArg is where the model id goes in the value of a ModelEnv.
const ModelArg = "{model}"

// ModelEnv is how a harness kind takes a model in its pane's environment,
// as its file says (flags.model_env): the variable yardr sets when it
// starts an agent of the kind whose group names model=<id>, and its value
// with ModelArg where the id goes. Entry JSON-escapes the model's string
// contents before substituting it; for opencode's JSON-string template,
// that keeps an id from adding config fields. A kind whose file names
// none takes no model this way.
type ModelEnv struct {
	// Var is the variable's name ("OPENCODE_CONFIG_CONTENT").
	Var string `yaml:"var" json:"var"`
	// Value is the variable's value with the id in it:
	// `{"model":"{model}"}` where the harness takes a whole config there.
	Value string `yaml:"value" json:"value"`
}

// Entry is the KEY=value entry of the environment that gives an agent the
// model: the value with ModelArg replaced by the model's JSON-escaped
// string contents.
func (e ModelEnv) Entry(model string) string {
	encoded, _ := json.Marshal(model) // strings always marshal successfully
	return e.Var + "=" + strings.ReplaceAll(e.Value, ModelArg, string(encoded[1:len(encoded)-1]))
}

// check holds the model environment of a harness file to what Entry can
// render: a variable that can be set, and the model in the value once.
func (e *ModelEnv) check() error {
	if e == nil {
		return nil
	}
	switch {
	case e.Var == "":
		return fmt.Errorf("flags.model_env.var: the variable that carries the model is needed")
	case !harnessEnvKey.MatchString(e.Var):
		return fmt.Errorf("flags.model_env.var: %q is no name of a variable", e.Var)
	case strings.Count(e.Value, ModelArg) != 1:
		return fmt.Errorf("flags.model_env.value: %s is in the value, once: where the model goes", ModelArg)
	}
	return nil
}

// modelProblems holds the model of a herdr group's config cfg to its kind:
// the kind's file names a model flag or a model environment for it to go
// to. A write that leaves model and kind as old holds them is not judged,
// so a group stored before a harness file dropped its kind's flag can
// still be changed, one key at a time, as configProblems has it.
func modelProblems(cat Catalogue, subject string, cfg, old map[string]any) []error {
	model, ok := cfg["model"].(string)
	if !ok || model == "" {
		return nil
	}
	if was, had := old["model"].(string); had && was == model && cat.configKind(old) == cat.configKind(cfg) {
		return nil
	}
	kind := cat.configKind(cfg)
	if _, takesEnv := cat.modelEnv(kind); takesEnv {
		return nil
	}
	if !cat.noModelFlag(kind) {
		return nil
	}
	return []error{fmt.Errorf("%s: model is for the kinds whose harness file names a model flag or a model environment, and %s is not: its file names neither. Unset model, or set the group's kind to one that takes it",
		subject, kind)}
}

// fallbackProblems holds a crew member's fallback_model to the kind it
// runs: a fallback is started by the same harness as the session it
// rescues, so a model of another kind would fail only at the failover,
// in the night. A write that leaves fallback_model and kind as old holds
// them is not judged, as modelProblems has it. Which kind a model
// belongs to is read from the models each kind's file offers
// (Catalogue.offers): one offered by another kind and not by this one is
// that kind's, and refused; one no kind offers, a full model id, cannot
// be told another kind's from the catalogue and is let through, as a crew
// member's model is (configkeys_test).
func fallbackProblems(cat Catalogue, subject string, cfg, old map[string]any) []error {
	fallback, ok := cfg["fallback_model"].(string)
	if !ok || fallback == "" {
		return nil
	}
	if was, had := old["fallback_model"].(string); had && was == fallback && cat.configKind(old) == cat.configKind(cfg) {
		return nil
	}
	kind := cat.configKind(cfg)
	if _, takesEnv := cat.modelEnv(kind); !takesEnv && cat.noModelFlag(kind) {
		return []error{fmt.Errorf("%s: fallback_model is for the kinds whose harness file names a model flag or a model environment, and %s is not: its file names neither. Unset fallback_model, or set the crew member's kind to one that takes a model",
			subject, kind)}
	}
	if slices.Contains(cat.offers(kind), fallback) {
		return nil
	}
	for _, other := range cat.offeringKinds() {
		if other != kind && slices.Contains(cat.offers(other), fallback) {
			return []error{fmt.Errorf("%s: fallback_model %q is a %s model, not a %s one: a fallback starts under the crew member's own harness, so name a %s model as fallback_model, or set its kind to %s",
				subject, fallback, other, kind, kind, other)}
		}
	}
	return nil
}
