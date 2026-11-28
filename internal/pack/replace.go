package pack

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// replaceable reports whether obj is a file under the line's own installed
// directory (a role's copy, a script or the page frame), which --replace may
// overwrite when it differs. Stage files live under <home>/flows/ and are
// changed with yardr flow set-stage instead.
func (o Object) replaceable(install string) bool {
	if o.Kind != "file" || o.File == nil || o.File.StageFile() {
		return false
	}
	rel, err := filepath.Rel(install, o.File.To)
	return err == nil && filepath.IsLocal(rel)
}

// replaces reports whether the import is told to overwrite obj, a differing
// file under the line's directory, with the line's bytes.
func (o ApplyOptions) replaces(obj Object, install string) bool {
	return obj.Action == Differs && obj.replaceable(install) && slices.Contains(o.Replace, obj.Name)
}

// replaceProblems are the refusals of the file names an import is told to
// replace (ApplyOptions.Replace), each a sentence, before anything is written:
// a path that is not a clean relative path, a stage file (which flow set-stage
// changes), a file the plan does not have under the line's directory, and one
// that does not differ in the plan. A name that is refused leaves the whole
// import unwritten.
func (p *Plan) replaceProblems(replace []string) []string {
	var avail []string
	for _, o := range p.Objects {
		if o.Action == Differs && o.replaceable(p.Install) {
			avail = append(avail, o.Name)
		}
	}
	slices.Sort(avail)
	differs := "no file under the line's directory differs"
	if len(avail) > 0 {
		differs = "files that differ: " + strings.Join(avail, ", ")
	}
	var out []string
	for _, name := range replace {
		if !fs.ValidPath(name) || name == "." {
			out = append(out, fmt.Sprintf("--replace %s: name a file under the line's directory, such as scripts/merge-local or roles/builder.md", name))
			continue
		}
		i := slices.IndexFunc(p.Objects, func(o Object) bool { return o.Kind == "file" && o.Name == name })
		switch {
		case i < 0:
			out = append(out, fmt.Sprintf("--replace %s: the plan has no such file under the line's directory (%s)", name, differs))
		case p.Objects[i].File != nil && p.Objects[i].File.StageFile():
			out = append(out, fmt.Sprintf("--replace %s: stage files are changed with yardr flow set-stage %s %s, not --replace", name, p.Objects[i].File.Flow, p.Objects[i].File.Stage))
		case !p.Objects[i].replaceable(p.Install):
			out = append(out, fmt.Sprintf("--replace %s: not a file under the line's directory (%s)", name, differs))
		case p.Objects[i].Action != Differs:
			out = append(out, fmt.Sprintf("--replace %s: it does not differ in the plan (it is %s; %s)", name, p.Objects[i].Action, differs))
		}
	}
	return out
}

// CheckReplace refuses file names to replace that the plan cannot carry out,
// in the words Apply gives them, so that a command or card can refuse before
// it shows or asks anything.
func (p *Plan) CheckReplace(replace []string) error {
	if len(replace) > 0 && p.Kind == KindSource {
		return fmt.Errorf("nothing was changed: pack %s is of kind source: --replace sets the original's version of a differing file under a line's directory, and a pack of kind source has none", p.Pack)
	}
	if problems := p.replaceProblems(replace); len(problems) > 0 {
		return fmt.Errorf("nothing was changed: %s", strings.Join(problems, "; "))
	}
	return nil
}

// Replacing is the plan as an import told to replace these files would carry
// it out, for a command to show: each differing file it replaces is a change.
// The plan itself is not changed, since Apply takes the names from its options.
func (p *Plan) Replacing(replace []string) *Plan {
	if len(replace) == 0 {
		return p
	}
	c := *p
	c.Objects = slices.Clone(p.Objects)
	for i, o := range c.Objects {
		if (ApplyOptions{Replace: replace}).replaces(o, p.Install) {
			c.Objects[i].Action = Change
		}
	}
	return &c
}
