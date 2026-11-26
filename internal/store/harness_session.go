package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// SessionSource locates a session file without requiring a model reader.
// Index maps exact directory paths to session ids in a JSON file.
type SessionSource struct {
	Home    string        `yaml:"home" json:"home"`
	HomeEnv string        `yaml:"home_env" json:"home_env,omitempty"`
	Glob    string        `yaml:"glob" json:"glob"`
	Index   *SessionIndex `yaml:"index" json:"index,omitempty"`
}

// SessionIndex names a JSON object keyed by exact directory path. Field is
// the dot path to the id in its value, or "." when the value is the id. At is
// the path to a transcript line's own time, read as a WorkspaceSource's is:
// the transcript's first timed line must fall in the session's window, so a
// record an earlier session left for the same directory is no match. Prompt
// is how that first line holds the prompt the session was started with, so a
// transcript another session left in the same directory is no match either.
type SessionIndex struct {
	File   string         `yaml:"file" json:"file"`
	Field  string         `yaml:"field" json:"field"`
	At     string         `yaml:"at" json:"at"`
	AtUnit string         `yaml:"at_unit" json:"at_unit,omitempty"`
	Prompt *SessionPrompt `yaml:"prompt" json:"prompt,omitempty"`
}

// SessionPrompt is the shape of a transcript's first user line, the one a
// harness writes the session's prompt into, inside a wrapper of its own. Type
// is the value of the line's type key, Field the key of its message text, and
// Open and Close the text the harness puts before and after the prompt in it.
// What follows Close is the harness's own and no part of the prompt. The
// line's type is always read from the key type, where a ContextSource names
// its own: the one harness with an index needs no other, and a key for it can
// be added later without refusing a file written before.
type SessionPrompt struct {
	Type  string `yaml:"type" json:"type"`
	Field string `yaml:"field" json:"field"`
	Open  string `yaml:"open" json:"open"`
	Close string `yaml:"close" json:"close"`
}

// SessionFile is the session file locator, falling back to the model reader's
// location for harnesses that already declare one there.
func (h Harness) SessionFile() *SessionSource {
	if h.Session != nil {
		return h.Session
	}
	if h.Model != nil {
		return &SessionSource{Home: h.Model.Home, HomeEnv: h.Model.HomeEnv, Glob: h.Model.Glob}
	}
	return nil
}

// Pattern is the transcript glob under dir with id in place of {session}.
func (s SessionSource) Pattern(dir, id string) string {
	return filepath.Join(dir, strings.ReplaceAll(s.Glob, sessionID, id))
}

// IDIn reads the session id from one index value, rejecting an unfamiliar
// shape without including the file's contents in an error.
func (s SessionIndex) IDIn(value any) (string, error) {
	if s.Field != "." {
		v, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("session index value is not an object")
		}
		value = valueAt(v, s.Field)
	}
	id, ok := value.(string)
	if !ok || id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\*?[]`) {
		return "", fmt.Errorf("session index id is not a plain session id")
	}
	return id, nil
}

// LineAt is when one transcript line was written, zero when it has no time
// that reads.
func (s SessionIndex) LineAt(line []byte) time.Time {
	var v map[string]any
	if json.Unmarshal(line, &v) != nil {
		return time.Time{}
	}
	return atTime(v, s.At, s.AtUnit)
}

// Matches tells whether line is a first user line that holds prompt inside
// the wrapper p describes. The wrapper may be followed by metadata, so the
// prompt is what lies between Open and the first Close, not the whole message.
func (p SessionPrompt) Matches(line []byte, prompt string) bool {
	var v map[string]any
	if json.Unmarshal(line, &v) != nil || v["type"] != p.Type {
		return false
	}
	text, _ := v[p.Field].(string)
	text, wrapped := strings.CutPrefix(text, p.Open)
	if !wrapped {
		return false
	}
	request, _, closed := strings.Cut(text, p.Close)
	return closed && request == prompt
}

func (s SessionSource) check() error {
	switch {
	case s.Home == "":
		return fmt.Errorf("home is needed")
	case !strings.Contains(s.Glob, sessionID):
		return fmt.Errorf("glob has no %s", sessionID)
	case filepath.IsAbs(s.Glob) || hasParentSegment(s.Glob):
		return fmt.Errorf("glob must stay under the harness home")
	}
	if idx := s.Index; idx != nil {
		switch {
		case idx.File == "" || idx.Field == "" || idx.At == "":
			return fmt.Errorf("index: file, field and at are needed")
		case filepath.IsAbs(idx.File) || hasParentSegment(idx.File):
			return fmt.Errorf("index: file must stay under the harness home")
		case idx.Field != "." && strings.Contains("."+idx.Field+".", ".."):
			return fmt.Errorf("index: field names an empty key")
		}
		if _, ok := atUnit(idx.AtUnit); !ok {
			return fmt.Errorf("index: at_unit %q is none of s, ms, us", idx.AtUnit)
		}
		// A crew launch puts a reference of its own in the first prompt of
		// every indexed kind, so a kind with no shape to find it by could
		// never be pinned.
		if p := idx.Prompt; p == nil || p.Type == "" || p.Field == "" || p.Open == "" || p.Close == "" {
			return fmt.Errorf("index: prompt needs type, field, open and close")
		}
	}
	return nil
}

func hasParentSegment(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
