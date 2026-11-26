package store_test

import (
	"testing"

	"yardr.dev/yardr/internal/store"
)

// A harness file says how its first user line holds the prompt a session was
// started with (session.index.prompt), and Matches reads that, not the words
// of any one harness. The first seven rows are Antigravity's own shape
// (harnesses/agy.yaml): the wrapper may be followed by metadata, and anything
// else is another session's file. The rest are a made-up shape no file
// declares, which a method that kept one harness's words in it would refuse.
func TestSessionPromptMatches(t *testing.T) {
	t.Parallel()
	agy := store.SessionPrompt{Type: "USER_INPUT", Field: "content", Open: "<USER_REQUEST>\n", Close: "\n</USER_REQUEST>"}
	other := store.SessionPrompt{Type: "user", Field: "text", Open: "[", Close: "]"}
	wrap := func(content string) string {
		return `{"type":"USER_INPUT","content":"<USER_REQUEST>\n` + content + `\n</USER_REQUEST>"}` + "\n"
	}
	for _, tc := range []struct {
		name  string
		shape store.SessionPrompt
		line  string
		want  bool
	}{
		{"wrapped", agy, wrap("go"), true},
		{"metadata after", agy, `{"type":"USER_INPUT","content":"<USER_REQUEST>\ngo\n</USER_REQUEST>\n<metadata/>"}` + "\n", true},
		{"another prompt", agy, wrap("stay"), false},
		{"unwrapped", agy, `{"type":"USER_INPUT","content":"go"}` + "\n", false},
		{"unclosed", agy, `{"type":"USER_INPUT","content":"<USER_REQUEST>\ngo"}` + "\n", false},
		{"another line", agy, `{"type":"GENERIC","content":"<USER_REQUEST>\ngo\n</USER_REQUEST>"}` + "\n", false},
		{"no JSON", agy, "not json\n", false},
		{"another shape's wrapper", other, `{"type":"user","text":"[go] and its metadata"}` + "\n", true},
		{"another shape's key holds no text", other, `{"type":"user","content":"[go]"}` + "\n", false},
		{"another shape's text is no string", other, `{"type":"user","text":["[go]"]}` + "\n", false},
		{"one shape's line is no other's", other, wrap("go"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.shape.Matches([]byte(tc.line), "go"); got != tc.want {
				t.Errorf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}
