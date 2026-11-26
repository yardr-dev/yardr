package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"yardr.dev/yardr/internal/agents"
	"yardr.dev/yardr/internal/store"
)

// A store is opened with a catalogue of the harness kinds that is complete,
// and Open refuses one that is not before it touches the file: no read of it
// later asks whether it is there, so the refusal is the one place that does.
func TestOpenRefusesAnIncompleteCatalogue(t *testing.T) {
	t.Parallel()
	whole := agents.Catalogue()
	// The kinds with the first file's kind taken out: that file's harness is of a
	// kind herdr does not start.
	strayed := whole
	strayed.Kinds = slices.DeleteFunc(slices.Clone(whole.Kinds), func(kind string) bool { return kind == whole.Harnesses[0].Kind })
	for name, cat := range map[string]store.Catalogue{
		"the zero catalogue": {},
		"no harnesses":       {Kinds: whole.Kinds, Executable: whole.Executable, DefaultKind: whole.DefaultKind},
		"no kinds":           {Harnesses: whole.Harnesses, Executable: whole.Executable, DefaultKind: whole.DefaultKind},
		"no program":         {Harnesses: whole.Harnesses, Kinds: whole.Kinds, DefaultKind: whole.DefaultKind},
		"no default kind":    {Harnesses: whole.Harnesses, Kinds: whole.Kinds, Executable: whole.Executable},
		"a harness of a kind herdr does not start": strayed,
	} {
		path := filepath.Join(t.TempDir(), "y.db")
		s, err := store.Open(path, cat)
		if err == nil {
			s.Close(t.Context())
			t.Errorf("%s: opened", name)
			continue
		}
		if !errors.Is(err, store.ErrNoCatalogue) {
			t.Errorf("%s: %v, want it to wrap ErrNoCatalogue", name, err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: the refused store left %s behind: %v", name, path, err)
		}
	}
	path := filepath.Join(t.TempDir(), "y.db")
	s, err := store.Open(path, whole)
	if err != nil {
		t.Fatalf("yardr's own catalogue: %v", err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
