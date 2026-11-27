package isolatedyard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/testload"
)

// stockView runs stock-view from the repository's root, in this binary's
// environment with env beside it, and returns what it printed and its exit
// status.
func stockView(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command(filepath.Join(root, "contrib/isolated-yard/stock-view"), args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	}
	t.Fatalf("stock-view %s: %v\n%s", strings.Join(args, " "), err, out)
	return "", 0
}

// TestStockViewRefuses holds stock-view to the yards it takes, one that `up`
// made and nothing has used, and has it refuse any other before it runs a
// command in it. What it looks at is files, so a directory that holds them
// stands for the yard: the mark `up` leaves (its own path), a D/e, the home
// D/y, and the herdr copy in D/bin that a bare yard has none of. A real yard
// would cost a build and a herdr for a refusal that no yard is needed to see.
func TestStockViewRefuses(t *testing.T) {
	t.Parallel()
	fakeYard := func(t *testing.T, mark, herdr, used bool) string {
		t.Helper()
		d := t.TempDir()
		// D/e is never run: a script that runs a command in a yard it
		// refuses fails at its first, and says so.
		files := map[string]string{"e": "#!/bin/sh\necho 'stock-view ran a command in a yard it refuses' >&2\nexit 99\n"}
		if mark {
			files[".isolated-yard"] = d + "\n"
		}
		if herdr {
			files["bin/herdr"] = "#!/bin/sh\nexit 0\n"
		}
		if used {
			files["y/daemon.log"] = "a home that init has been run in\n"
		}
		if err := os.MkdirAll(filepath.Join(d, "y"), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, text := range files {
			path := filepath.Join(d, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	for _, tc := range []struct {
		name string
		args func(t *testing.T) []string
		env  []string
		code int
		said string
	}{
		{"no yard named", func(*testing.T) []string { return nil }, nil, 2, "usage: stock-view D"},
		{"a directory that is no yard", func(t *testing.T) []string { return []string{fakeYard(t, false, true, false)} }, nil, 2, "was not made by isolated-yard up"},
		{"a bare yard", func(t *testing.T) []string { return []string{fakeYard(t, true, false, false)} }, nil, 1, "is a bare yard"},
		{"a yard that has been used", func(t *testing.T) []string { return []string{fakeYard(t, true, true, true)} }, nil, 1, "has a yard already"},
		{"a fuse that is no number", func(t *testing.T) []string { return []string{fakeYard(t, true, true, false)} }, []string{"STOCK_VIEW_WAIT=ten"}, 2, "STOCK_VIEW_WAIT must be a whole number of seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, code := stockView(t, tc.env, tc.args(t)...)
			if code != tc.code || !strings.Contains(out, tc.said) {
				t.Fatalf("stock-view: exit %d, printed %q; want exit %d and %q", code, out, tc.code, tc.said)
			}
		})
	}
}

// TestStockView stocks a yard that `up` made and reads back through the
// yard's own commands what the pages of Crew, a conversation, a delegated
// run's record and Settings draw: the members and their states, the letters
// with their files and what is read, the record, the name. It brings up a
// herdr server and a stand-in agent of its own and takes a minute, so like
// TestYardShapes it runs in the quiet run alone (docs/testing.md R6).
func TestStockView(t *testing.T) {
	if os.Getenv("YARDR_QUIET_RUN") == "" {
		t.Skip("stock-view's yard needs herdr and a minute, so the test runs in the quiet run only, which sets YARDR_QUIET_RUN=1 (docs/testing.md R6)")
	}
	// A yard of its own: a directory under /tmp, a herdr server and a home
	// that nothing else shares, and no port.
	t.Parallel()
	d := up(t)
	// Each wait of the script is written for an idle box, and the fuse of it
	// is scaled to the machine as this suite's own are.
	fuse := "STOCK_VIEW_WAIT=" + strconv.Itoa(int(testload.Scale(60*time.Second)/time.Second))
	out, code := stockView(t, []string{fuse}, d)
	if code != 0 {
		t.Fatalf("stock-view %s: exit %d\n%s", d, code, out)
	}
	ada := func(v any, args ...string) {
		t.Helper()
		if code := drive(t, d, v, append([]string{"--actor", "ada"}, args...)...); code != 0 {
			t.Fatalf("yardr %s: exit %d, %+v", strings.Join(args, " "), code, v)
		}
	}

	type member struct {
		Name    string `json:"name"`
		State   string `json:"state"`
		Enabled bool   `json:"enabled"`
	}
	var crew []member
	ada(&crew, "crew", "list")
	byName := map[string]member{}
	for _, m := range crew {
		byName[m.Name] = m
	}
	for _, want := range []member{{"scribe", "running", true}, {"night", "stopped", false}, {"yardmaster", "running", true}} {
		if got, ok := byName[want.Name]; !ok || got != want {
			t.Errorf("crew %s: %+v, want %+v (crew list: %+v)", want.Name, got, want, crew)
		}
	}

	var letters []struct {
		Seq   uint64 `json:"seq"`
		From  string `json:"from"`
		To    string `json:"to"`
		Read  bool   `json:"read"`
		Files []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"files"`
	}
	ada(&letters, "mail", "--with", "yardmaster")
	if len(letters) < 4 {
		t.Errorf("the conversation with the yardmaster holds %d letters, want at least 4: %+v", len(letters), letters)
	}
	// The stand-in yardmaster's greeting is the first letter: the script waits
	// for it before it writes any of its own.
	if len(letters) > 0 && (letters[0].From != "yardmaster" || letters[0].To != "ada") {
		t.Errorf("the conversation opens with %+v, want the yardmaster's greeting to ada", letters[0])
	}
	var newest uint64
	var toAda, toYardmaster, withFile int
	for _, l := range letters {
		switch {
		case l.From == "ada" && l.To == "yardmaster":
			toYardmaster++
			newest = max(newest, l.Seq)
			if !l.Read {
				t.Errorf("ada's letter %d to the yardmaster is not read", l.Seq)
			}
		case l.From == "yardmaster" && l.To == "ada":
			toAda++
			if l.Read {
				t.Errorf("the yardmaster's letter %d to ada is read: the stocked yard leaves her inbox unread", l.Seq)
			}
		default:
			t.Errorf("letter %d is from %s to %s, in a conversation of ada and the yardmaster", l.Seq, l.From, l.To)
		}
		for _, f := range l.Files {
			if f.Type != "image/png" || f.Name == "" {
				t.Errorf("letter %d carries %+v, want a named PNG", l.Seq, f)
			}
			withFile++
		}
	}
	if toAda == 0 || toYardmaster == 0 || withFile == 0 {
		t.Errorf("the conversation has %d letters to ada, %d to the yardmaster and %d files, want some of each: %+v", toAda, toYardmaster, withFile, letters)
	}

	var rec struct {
		ID      uint64 `json:"id"`
		For     string `json:"for"`
		Mail    uint64 `json:"mail"`
		Command string `json:"command"`
	}
	ada(&rec, "delegation", "show", "1")
	if rec.ID != 1 || rec.For != "ada" || rec.Mail != newest || !strings.Contains(rec.Command, "group set writers --limit 10") {
		t.Errorf("delegation 1: %+v; want the yardmaster's group set for ada on mail %d, her newest letter to it", rec, newest)
	}

	var name struct {
		Value string `json:"value"`
	}
	ada(&name, "settings", "get", "name")
	if name.Value != "kestrel" {
		t.Errorf("settings get name: %q, want kestrel", name.Value)
	}

	// Every stand-in that takes a first prompt runs D/run, so the script
	// leaves none behind.
	if _, err := os.Stat(filepath.Join(d, "run")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s/run is left behind (stat: %v): the next member to start would run it", d, err)
	}

	// The last lines are the two commands that serve the view, as the
	// README's recipe has them, so the one cannot drift from the other.
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(readme), "\n## Stocking a yard for Crew, talk and Settings\n")
	if !ok {
		t.Fatal("the README has no section \"Stocking a yard for Crew, talk and Settings\"")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	for _, line := range []string{
		d + "/e yardr web serve --as ada --addr 127.0.0.1:0 --pages-addr 127.0.0.1:0 > " + d + "/web.log 2>&1 &",
		d + "/e yardr web open --print",
	} {
		if !slices.Contains(strings.Split(out, "\n"), line) {
			t.Errorf("stock-view did not print %q", line)
		}
		if !strings.Contains(section, "\n"+strings.ReplaceAll(line, d, "D")) {
			t.Errorf("the README's section does not hold %q as a line", strings.ReplaceAll(line, d, "D"))
		}
	}

	// A yard it has stocked has a yard: a second run is refused, not stacked
	// on the first.
	if again, code := stockView(t, nil, d); code != 1 || !strings.Contains(again, "has a yard already") {
		t.Errorf("stock-view on the stocked yard: exit %d, printed %q; want exit 1 and a refusal", code, again)
	}
}
