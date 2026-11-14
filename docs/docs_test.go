package docs

import (
	"encoding/xml"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Every page the binary carries is listed, and every listed name is a
// page: a .md added beside docs.go without its row in README.md's
// index, or a rename that leaves one behind, fails here.
func TestOrderListsEveryPage(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	var embedded []string
	for _, e := range entries {
		embedded = append(embedded, strings.TrimSuffix(e.Name(), ".md"))
	}
	if len(Order) != len(embedded) {
		t.Fatalf("Order lists %d pages, the binary carries %d: %q against %q", len(Order), len(embedded), Order, embedded)
	}
	seen := map[string]bool{}
	for _, n := range Order {
		seen[n] = true
	}
	for _, n := range embedded {
		if !seen[n] {
			t.Errorf("the binary carries %s.md, which Order does not list", n)
		}
	}
	for _, n := range Order {
		if text, ok := Read(n); !ok || text == "" {
			t.Errorf("Read(%q) = %d bytes, %v", n, len(text), ok)
		} else if title, ok := Title(n); !ok || title == "" {
			t.Errorf("Title(%q) = %q, %v: every page starts with a heading", n, title, ok)
		}
	}
}

// Lookup is what a name on the command line goes through: it finds a
// page however it is cased, and refuses an unknown name and a walk
// out of the pages.
func TestLookup(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"flows", "flows"},
		{"README", "README"},
		{"readme", "README"},
		{"Line", "line"},
	} {
		if got, ok := Lookup(tc.in); !ok || got != tc.want {
			t.Errorf("Lookup(%q) = %q, %v, want %q, true", tc.in, got, ok, tc.want)
		}
	}
	for _, in := range []string{"", "flow", "../go.mod", "flows.md", "design/notes"} {
		if got, ok := Lookup(in); ok || got != "" {
			t.Errorf("Lookup(%q) = %q, %v, want no page", in, got, ok)
		}
		if _, ok := Read(in); ok {
			t.Errorf("Read(%q) reads a page for an unknown name", in)
		}
	}
}

// Section prints one heading's part of a page: the match ignores case,
// the section runs to the next heading of the same or higher level,
// and deeper headings stay in. A line in a code fence is no heading, so
// a sample's `# comment` or a template's `## Ask` ends no section.
func TestSection(t *testing.T) {
	text, ok := Section("groups", "runners")
	if !ok {
		t.Fatal("Section(groups, runners) finds nothing")
	}
	if !strings.HasPrefix(text, "## Runners\n") {
		t.Fatalf("Section(groups, runners) starts %q", firstLine(text))
	}
	if strings.Contains(text, "\n## ") {
		t.Errorf("Section(groups, runners) runs past its next ## heading:\n%s", lastLines(text, 3))
	}
	if _, ok := Section("groups", "groups and runners"); ok {
		t.Errorf("Section(groups, groups and runners) matches the title, which would print the whole page")
	}
	if _, ok := Section("groups", "no such heading"); ok {
		t.Errorf("Section(groups, no such heading) finds a section")
	}
	if _, ok := Section("no such page", "runners"); ok {
		t.Errorf("Section(no such page, runners) finds a section")
	}
	if _, ok := Section("groups", "  "); ok {
		t.Errorf("Section(groups, blank) finds a section")
	}

	// The ask template's `## Ask` and an integration pack's
	// `# packs/linear/pack.yaml` stand in fences of their sections, which run
	// on past them: to the fence's close, and the text after it. (yardr-z53c:
	// each section ended at that line, with its fence open.)
	for _, c := range []struct{ page, words, fenced string }{
		{"flows", "ask template", "\n## Ask\nkind: choice\n"},
		{"packs", "integration pack", "\n# packs/linear/pack.yaml\nname: linear\n"},
	} {
		section, ok := Section(c.page, c.words)
		if !ok {
			t.Errorf("Section(%s, %s) finds nothing", c.page, c.words)
			continue
		}
		_, inFence, ok := strings.Cut(section, c.fenced)
		if !ok {
			t.Errorf("Section(%s, %s) stops before %q, a line in a fence:\n%s", c.page, c.words, strings.TrimSpace(c.fenced), lastLines(section, 3))
			continue
		}
		if _, after, closed := strings.Cut(inFence, "\n```\n"); !closed || strings.TrimSpace(after) == "" {
			t.Errorf("Section(%s, %s) ends in the fence that holds %q, or at its close:\n%s", c.page, c.words, strings.TrimSpace(c.fenced), lastLines(section, 3))
		}
	}

	// The rules, on texts no page carries: a fence's lines end no section
	// and start none, and a fence left open runs to the end of the page.
	for _, c := range []struct{ name, page, words, want string }{
		{"a fenced heading ends no section",
			"# T\n\n### Sample\n\n```\n## Ask\n# comment\n```\n\nafter\n\n### Next\n\nrest\n", "sample",
			"### Sample\n\n```\n## Ask\n# comment\n```\n\nafter\n"},
		{"a fenced heading starts no section",
			"# T\n\n## Intro\n\n```\n## Sample in a fence\n```\n\n## Sample\n\ntext\n", "sample",
			"## Sample\n\ntext\n"},
		{"an open fence runs to the end of the page",
			"# T\n\n## Sample\n\n```\n## Next\ntext\n", "sample",
			"## Sample\n\n```\n## Next\ntext\n"},
	} {
		if got, ok := sectionOf(c.page, c.words); !ok || got != c.want {
			t.Errorf("%s: sectionOf(%q, %q) = %q, %v, want %q", c.name, c.page, c.words, got, ok, c.want)
		}
	}
}

// Read prints words alone where a page carries pictures: the glossary's
// term cells hold picture blocks, and flows' drawing section markdown
// image spans, and neither shows in a terminal. A line in a code fence
// keeps what looks like markup, whatever it holds: api.md's JSON
// samples name .png files. Anything else stays byte for byte, even the
// gap a picture stood in.
func TestStripImages(t *testing.T) {
	pic := `<picture><source media="(prefers-color-scheme: dark)" srcset="assets/pictograms/track-plan-bead-light.svg"><img alt="bead" src="assets/pictograms/track-plan-bead-dark.svg" height="24"></picture>`
	for _, c := range []struct{ name, in, want string }{
		{"no markup stays byte for byte",
			"# T\n\nplain text, no trailing newline",
			"# T\n\nplain text, no trailing newline"},
		{"a picture block drops, its gap stays",
			"| **bead** " + pic + " | one durable piece of work |\n",
			"| **bead**  | one durable piece of work |\n"},
		{"a picture block at the end without a newline",
			"a " + pic,
			"a "},
		{"a picture block with attributes",
			`a <picture class="x">b</picture> c`,
			"a  c"},
		{"a picture block across lines",
			"a <picture>\nb\nc</picture> d\n",
			"a \n\n d\n"},
		{"a picture block left open runs to the end",
			"a <picture>b\nc\n",
			"a \n\n"},
		{"a self-closed picture tag drops alone",
			"a <picture/> b",
			"a  b"},
		{"a longer tag name is no picture",
			"a <picturesque>b</picturesque> c",
			"a <picturesque>b</picturesque> c"},
		{"an image span drops, its gap stays",
			"see ![the flow](flows/starter.svg) here\n",
			"see  here\n"},
		{"an image span alone leaves an empty line",
			"![The starter pack's flow](flows/starter.svg)\n",
			"\n"},
		{"an image span with a title",
			`![alt](img.svg "title")`,
			""},
		{"two image spans on one line",
			"![a](a.svg) and ![b](b.svg)",
			" and "},
		{"a span the line does not close stays",
			"a ![b](c and ![d e\n",
			"a ![b](c and ![d e\n"},
		{"a link is no image span",
			"[users.md](users.md#devices) and [pack](pack.md)\n",
			"[users.md](users.md#devices) and [pack](pack.md)\n"},
		{"a fence keeps image markup",
			"# T\n\n```json\n{\"name\": \"shed.png\"}\n```\n\n![a](a.svg)\n",
			"# T\n\n```json\n{\"name\": \"shed.png\"}\n```\n\n\n"},
		{"a fence keeps a picture block",
			"```\n" + pic + "\n```\n",
			"```\n" + pic + "\n```\n"},
		{"a fence left open runs to the end",
			"```\n![a](a.svg)\n",
			"```\n![a](a.svg)\n"},
	} {
		if got := stripImages(c.in); got != c.want {
			t.Errorf("%s: stripImages(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// Every glossary term with a track-plan icon stands with its twin
// pair in its row, and every twin is well-formed SVG: a row that
// loses its pictures, or a twin that stops parsing, fails here. The
// twins' bytes are the other repository's to change, so presence and
// shape are held, never the drawings.
func TestGlossaryIconsHaveTwins(t *testing.T) {
	bold := map[string]bool{
		"yard": true, "depot": true, "bead": true, "train": true,
		"wagon": true, "siding": true, "carrier": true, "fold": true,
		"crew": true, "line": true, "ask": true,
	}
	terms := []string{
		"yard", "depot", "bead", "train", "wagon", "siding",
		"carrier", "fold", "crew", "line", "ask", "flow", "stage",
		"route", "group", "hook", "source", "peer", "pack", "edge",
	}
	raw, err := FS.ReadFile("glossary.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "<picture"); n != len(terms) {
		t.Fatalf("glossary.md carries %d pictures, want one per term in %d rows", n, len(terms))
	}
	for _, term := range terms {
		cell := "| " + term + " <picture>"
		if bold[term] {
			cell = "| **" + term + "** <picture>"
		}
		var row string
		for line := range strings.Lines(string(raw)) {
			if strings.Contains(line, cell) {
				row = line
				break
			}
		}
		if row == "" {
			t.Errorf("no glossary row starts its term cell %q", cell)
			continue
		}
		for _, twin := range []string{"dark", "light"} {
			path := "assets/pictograms/track-plan-" + term + "-" + twin + ".svg"
			if !strings.Contains(row, path) {
				t.Errorf("%s's row names no %s", term, path)
				continue
			}
			svg, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("ReadFile(%s): %v", path, err)
				continue
			}
			var v struct {
				XMLName xml.Name
			}
			if err := xml.Unmarshal(svg, &v); err != nil {
				t.Errorf("%s is no well-formed SVG: %v", path, err)
			} else if v.XMLName.Local != "svg" {
				t.Errorf("%s holds <%s>, want <svg>", path, v.XMLName.Local)
			}
		}
		if !strings.Contains(row, `alt="`+term+`"`) {
			t.Errorf("%s's row carries no alt text naming the term", term)
		}
		if !strings.Contains(row, `height="24"`) {
			t.Errorf("%s's row carries no 24px height", term)
		}
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
