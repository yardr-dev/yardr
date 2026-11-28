package pack

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"yardr.dev/yardr/internal/store"
)

// files are the pack's roles, scripts and page frame, copied under Install,
// and the stage files of the flows the import uses, copied to where the yard
// reads a store flow's: <home>/flows/<pack>.<flow>/<stage>.md.
func (b *planner) files(flows []Flow) error {
	for _, rel := range b.p.Files() {
		op := &FileOp{To: b.installed(rel), Fill: b.p.Filled(rel)}
		if err := b.file(rel, op); err != nil {
			return err
		}
	}
	for _, f := range flows {
		name := FlowName(b.p.Name, f.Name)
		dir := store.HomeStageDir(b.o.Home, name)
		for _, s := range f.Stages {
			if s.File == "" {
				continue
			}
			// The reader refuses a name that leads out of the flow's
			// directory; nothing is written by a pack that got past it.
			rel := filepath.FromSlash(s.Name) + ".md"
			if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel {
				return fmt.Errorf("stage %s of flow %s: its stage file would be written outside %s", s.Name, f.Name, dir)
			}
			op := &FileOp{To: filepath.Join(dir, rel), Flow: name, Stage: s.Name, Fill: b.p.Filled(s.File)}
			if err := b.file(s.File, op); err != nil {
				return err
			}
		}
	}
	return nil
}

// file plans the copy of the pack's file rel to op.To. What is compared
// with the installed copy is the text as it would be installed: filled, for
// a role or stage file. Where the yard lacks the file, the plan creates it;
// where the yard has it, the plan says unchanged when content and executable
// mode match and differs otherwise, writing nothing.
func (b *planner) file(rel string, op *FileOp) error {
	op.From = joinName(b.p.Dir, rel)
	text, err := b.p.read(rel)
	if err != nil {
		return err
	}
	if op.Fill {
		text = Fill(text, b.plan.Fill)
	}
	op.Mode, op.SHA256 = "0644", sum(text)
	// A script, by the pack's saying so and not by its mode in the source
	// (Pack.script): the frame is not one, though nothing is filled in it.
	if b.p.script(rel) {
		op.Mode = "0755"
	}
	var changes []Diff
	have, err := os.ReadFile(op.To)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if exists {
		if !bytes.Equal(have, text) {
			changes = append(changes, Diff{"content", "sha256 " + sum(have)[:12], "sha256 " + op.SHA256[:12]})
		}
		if hi, err := os.Stat(op.To); err == nil && (hi.Mode().Perm()&0o111 != 0) != (op.Mode == "0755") {
			changes = append(changes, Diff{"mode", fmt.Sprintf("%04o", hi.Mode().Perm()), op.Mode})
		}
	}
	act := Create
	if exists {
		act = Unchanged
		if len(changes) > 0 {
			act = Differs
		}
	}
	b.add(Object{Kind: "file", Name: rel, Action: act, Changes: changes, File: op})
	return nil
}

// StageFile says op writes the file of a stage of one of the pack's flows: the
// plan marks it with the store flow and the stage (files).
func (op *FileOp) StageFile() bool { return op.Flow != "" && op.Stage != "" }
