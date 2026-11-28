package pack

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"yardr.dev/yardr/internal/depotfile"
	"yardr.dev/yardr/internal/runner"
	"yardr.dev/yardr/internal/store"
)

// setFileDiff populates YardText, OriginalText and Diff on a differing file
// object when both sides are valid UTF-8 text.
func setFileDiff(o *Object, have, want []byte) {
	if o.Action != Differs || !utf8.Valid(have) || !utf8.Valid(want) {
		return
	}
	setTextDiff(o, string(have), string(want))
}

// setTextDiff populates YardText, OriginalText and their unified diff on a
// differing object.
func setTextDiff(o *Object, yard, original string) {
	if o.Action != Differs {
		return
	}
	o.YardText = &yard
	o.OriginalText = &original
	diff := store.Unified(yard, original)
	o.Diff = &diff
}

// setChangesDiff renders a differing object's changed settings as plain
// field: value lines on each side with their unified diff.
func setChangesDiff(o *Object) {
	if o.Action != Differs || len(o.Changes) == 0 {
		return
	}
	var yard, orig strings.Builder
	for _, d := range o.Changes {
		fmt.Fprintf(&yard, "%s: %s\n", d.Field, diffTextValue(d.Old))
		fmt.Fprintf(&orig, "%s: %s\n", d.Field, diffTextValue(d.New))
	}
	setTextDiff(o, yard.String(), orig.String())
}

// setGroupDiff sets the compare texts for a differing group: when its prompt
// differs and both prompt texts can be read, YardText is the text of the file
// the group's prompt names (so a role edited by a card shows whole beside the
// line's role) and OriginalText is the line's filled role. Otherwise the
// differing group settings are rendered from Changes.
func (b *planner) setGroupDiff(o *Object, g Group, old store.Group) {
	if o.Action != Differs {
		return
	}
	if g.Agent != "" && slices.ContainsFunc(o.Changes, func(d Diff) bool { return d.Field == "prompt" }) {
		prompt, _ := old.Config["prompt"].(string)
		if yard, ok := b.readPromptText(prompt); ok {
			if orig, ok := b.readPackRoleText(g.Agent); ok {
				setTextDiff(o, yard, orig)
				return
			}
		}
	}
	setChangesDiff(o)
}

// setCrewDiff sets the compare texts for a differing crew member, reading the
// prompt files when its prompt differs and rendering Changes otherwise.
func (b *planner) setCrewDiff(o *Object, pr CrewMember, old store.CrewMember) {
	if o.Action != Differs {
		return
	}
	if pr.Name != "yardmaster" && pr.Role != "" && slices.ContainsFunc(o.Changes, func(d Diff) bool { return d.Field == "prompt" }) {
		prompt, _ := old.Config["prompt"].(string)
		if yard, ok := b.readPromptText(prompt); ok {
			if orig, ok := b.readPackRoleText(pr.Role); ok {
				setTextDiff(o, yard, orig)
				return
			}
		}
	}
	setChangesDiff(o)
}

// readPromptText reads the UTF-8 text of the prompt file p names for the
// planner's depot and home, returning "", true when p is empty.
func (b *planner) readPromptText(prompt string) (string, bool) {
	if prompt == "" {
		return "", true
	}
	src, err := runner.LocatePrompt(prompt, b.depot, b.o.Home)
	if err != nil {
		return "", false
	}
	var text []byte
	if src.Kind == runner.PromptDepot {
		if b.depot == nil {
			return "", false
		}
		text, _, _, err = depotfile.ReadDepotFile(b.depot, src.Path)
	} else {
		text, err = os.ReadFile(src.Path)
	}
	if err != nil || !utf8.Valid(text) {
		return "", false
	}
	return string(text), true
}

// readPackRoleText reads rel from the pack, filling placeholders as install
// does, and returns it when it is valid UTF-8.
func (b *planner) readPackRoleText(rel string) (string, bool) {
	text, err := b.p.read(rel)
	if err != nil {
		return "", false
	}
	if b.p.Filled(rel) {
		text = Fill(text, b.plan.Fill)
	}
	if !utf8.Valid(text) {
		return "", false
	}
	return string(text), true
}

// diffTextValue renders one setting value on a yard/original compare line.
func diffTextValue(v any) string {
	switch v := v.(type) {
	case nil:
		return "(none)"
	case string:
		if v == "" {
			return "(none)"
		}
		return v
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}
