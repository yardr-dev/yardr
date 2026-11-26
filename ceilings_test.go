package main

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The core must not grow unseen. docs/contracts/core.md holds, under Line
// ceilings, a table of the packages that hold core code, each with a ceiling
// in non-test Go lines, and this test holds the code to it. It counts the
// package, not the file: the gate's filesizes table caps a file, so a
// package that outgrows its budget by splitting files would pass it, and it
// does not pass this. It fails when a package is over its ceiling, when a
// row names a directory where no package stands, and when the page names a
// package as core and the table has no row for it. A package under its
// ceiling by any amount passes: lowering a ceiling is the work of a bead
// that moves code out, and this test never raises or rewrites one. The page
// names this test as the check of its fifth rule. It reads files and starts
// no process.
func TestCorePackagesKeepUnderTheirCeilings(t *testing.T) {
	t.Parallel()
	// The test runs in the repository root, the root package's directory.
	page, err := os.ReadFile("docs/contracts/core.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := func(pkg string) (int, bool) {
		n, found, err := packageLines(filepath.FromSlash(pkg))
		if err != nil {
			t.Fatalf("counting the lines of %s: %v", pkg, err)
		}
		return n, found
	}
	for _, problem := range ceilingProblems(string(page), lines) {
		t.Errorf("docs/contracts/core.md: %s", problem)
	}
}

// packageLines is the size of the package whose files stand in dir: the
// lines of every non-test .go file directly in it, none of a subdirectory,
// each file counted by its newlines as wc -l counts it, which is the measure
// the design of the ceilings used. found is false when dir does not exist
// and when it holds no such file, so that a package with no lines is told
// from a directory that is no package, which a ceiling row may not name.
func packageLines(dir string) (lines int, found bool, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, false, err
		}
		lines += bytes.Count(src, []byte{'\n'})
		found = true
	}
	return lines, found, nil
}

// ceilingCols is the header of the Line ceilings table. The measured cell is
// for the reader, who sees there what the ceiling was set from; the test
// holds that it is written and no more, since only git can say what a
// package held at a commit.
var ceilingCols = []string{"package", "ceiling", "measured"}

var (
	// ceilingPackage is a package cell: one path under internal/ in
	// backticks, so the test never opens a directory the page does not mean.
	ceilingPackage = regexp.MustCompile("^`(internal/[a-z][a-z0-9_]*(/[a-z][a-z0-9_]*)*)`$")
	// ceilingNumber is a ceiling cell: a whole number of lines, with its
	// thousands grouped by commas as the rest of the page writes them, or not
	// grouped at all.
	ceilingNumber = regexp.MustCompile(`^([1-9][0-9]{0,2}(,[0-9]{3})+|[1-9][0-9]*)$`)
	// namedPath is a path under internal/ in backticks, as the page writes
	// the files and packages it speaks of.
	namedPath = regexp.MustCompile("`(internal/[^`\\s]+)`")
)

// ceilingProblems reads a page's Line ceilings table and says what is wrong
// with it, one sentence for each fault, the sentence naming the package it
// is about: a row that cannot be read, a package over its ceiling (with its
// count and its ceiling), a row for a package that is not there, a package
// twice, and a package the page names as core that has no row. lines is the
// count of a package's lines, and whether a package stands in that
// directory; it is a parameter so that the refusals can be tried against
// made-up pages and counts.
func ceilingProblems(page string, lines func(pkg string) (n int, found bool)) []string {
	rows, problem := sectionTable(page, "Line ceilings", ceilingCols)
	if problem != "" {
		return []string{problem}
	}
	var problems []string
	hasRow := map[string]bool{}
	for i, cells := range rows {
		row := "row " + strconv.Itoa(i+1) + ": "
		if len(cells) != len(ceilingCols) {
			problems = append(problems, row+"the row has "+strconv.Itoa(len(cells))+" cells, want 3: package | ceiling | measured")
			continue
		}
		m := ceilingPackage.FindStringSubmatch(cells[0])
		if m == nil {
			problems = append(problems, row+"the package cell "+strconv.Quote(cells[0])+" is not one path under internal/ in backticks")
			continue
		}
		pkg := m[1]
		if hasRow[pkg] {
			problems = append(problems, pkg+" has two rows in Line ceilings, want one")
			continue
		}
		// A row with a ceiling that cannot be read still stands for its
		// package, so that the one fault is not told again as a missing row.
		hasRow[pkg] = true
		if cells[2] == "" {
			problems = append(problems, pkg+": the measured cell is blank: write the lines the package held and the commit"+
				" the ceiling was set from")
		}
		if !ceilingNumber.MatchString(cells[1]) {
			problems = append(problems, pkg+": the ceiling cell "+strconv.Quote(cells[1])+" is not a whole number of lines such as 6,000")
			continue
		}
		ceiling, err := strconv.Atoi(strings.ReplaceAll(cells[1], ",", ""))
		if err != nil {
			problems = append(problems, pkg+": the ceiling cell "+strconv.Quote(cells[1])+" is not a whole number of lines: "+err.Error())
			continue
		}
		n, found := lines(pkg)
		switch {
		case !found:
			problems = append(problems, pkg+" has a row in Line ceilings and no package stands there (renamed or removed?): change the row with it")
		case n > ceiling:
			problems = append(problems, pkg+" holds "+strconv.Itoa(n)+" non-test Go lines, "+strconv.Itoa(n-ceiling)+
				" over its ceiling of "+strconv.Itoa(ceiling)+": move how-and-what out of the package before adding core code"+
				" (docs/contracts/core.md, Line ceilings)")
		}
	}
	named, more := corePackagesNamed(page)
	problems = append(problems, more...)
	for _, pkg := range slices.Sorted(maps.Keys(named)) {
		if !hasRow[pkg] {
			problems = append(problems, pkg+" is named as core by "+named[pkg]+" and has no row in Line ceilings: add one,"+
				" set at the package's lines plus the room that section gives")
		}
	}
	return problems
}

// corePackagesNamed returns the packages under internal/ that a page names as
// core, each with the place that names it, so that a package which is core
// by the page's own word cannot go without a ceiling. The page names them in
// two places. The list of what is core, between its lead-in line and the
// how-and-what list's, names files by path (internal/store/carry.go), and the
// package of each path is core. The Core types table names the owning package
// of each core type. A package the how-and-what list names is not read.
func corePackagesNamed(page string) (named map[string]string, problems []string) {
	named = map[string]string{}
	name := func(text, where string) {
		for _, m := range namedPath.FindAllStringSubmatch(text, -1) {
			pkg := m[1]
			if strings.HasSuffix(pkg, ".go") {
				pkg = path.Dir(pkg)
			}
			if _, ok := named[pkg]; !ok {
				named[pkg] = where
			}
		}
	}
	const coreLead, howLead = "\nBy this rule these are core:\n", "\nThese are how-and-what:\n"
	_, rest, _ := strings.Cut(page, coreLead)
	list, _, found := strings.Cut(rest, howLead)
	if found {
		name(list, "the list of what is core")
	} else {
		problems = append(problems, "has no list of what is core: it stands between the lines "+
			strconv.Quote(strings.TrimSpace(coreLead))+" and "+strconv.Quote(strings.TrimSpace(howLead)))
	}
	rows, problem := sectionTable(page, "Core types", []string{"type", "owning package", "what it is"})
	if problem != "" {
		return named, append(problems, problem)
	}
	for i, cells := range rows {
		if len(cells) != 3 {
			problems = append(problems, "Core types row "+strconv.Itoa(i+1)+" has "+strconv.Itoa(len(cells))+" cells, want 3: type | owning package | what it is")
			continue
		}
		name(cells[1], "the Core types table")
	}
	return named, problems
}

// The refusals fire. Each page below is right or wrong in one way, and the
// problem ceilingProblems gives for it must name the package and the fault,
// and be the only one. The pages and the counts are made up here because the
// real page has no fault to show. The rest of the test holds what a
// package's lines are, on a directory built for it.
func TestCoreCeilingsRefuse(t *testing.T) {
	t.Parallel()
	// counts are the lines of the packages that stand in the made-up tree.
	counts := map[string]int{"internal/a": 10, "internal/b": 5, "internal/c": 7}
	const (
		// coreList names internal/a as core by a file of it.
		coreList = "- beads (`internal/a/beads.go`)\n"
		// coreTypes names internal/b as the owning package of a core type, and
		// internal/impl only where a type is implemented, which names no core.
		coreTypes = "| bead | `internal/a` | a piece of work |\n" +
			"| runner | `internal/b` | how work starts, implemented in `internal/impl` |\n"
		rowA = "| `internal/a` | 10 | 10 lines at abc1234 |"
		rowB = "| `internal/b` | 1,000 | 5 lines at abc1234 |"
	)
	// build is a made-up core.md: the list of what is core, a how-and-what
	// list that names internal/e (a package the page does not name as core,
	// so needs no row), the Core types table and the Line ceilings rows given,
	// followed by a section whose table is no ceiling, which no one reads.
	build := func(core, types string, rows ...string) string {
		return "# The core\n\n## What is core\n\nBy this rule these are core:\n\n" + core +
			"\nThese are how-and-what:\n\n- a view (`internal/e/view.go`)\n\n" +
			"## Core types\n\n| type | owning package | what it is |\n|---|---|---|\n" + types +
			"\n## Line ceilings\n\nText.\n\n| package | ceiling | measured |\n|---|---:|---|\n" +
			strings.Join(rows, "\n") + "\n\nText.\n\n## After\n\n| 1 | not a ceiling | |\n"
	}
	for _, tc := range []struct {
		name   string
		page   string
		counts map[string]int // lines that replace the made-up counts
		want   []string       // parts of the one problem; none means the page is right
	}{
		{"at its ceiling and under it", build(coreList, coreTypes, rowA, rowB), nil, nil},
		{"a ceiling written without commas", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 700 | 7 lines at abc1234 |"), nil, nil},
		{"a package no one names as core", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 7 | 7 lines at abc1234 |"), nil, nil},
		{"over its ceiling", build(coreList, coreTypes, rowA, rowB), map[string]int{"internal/a": 11},
			[]string{"internal/a", "holds 11 non-test Go lines", "1 over its ceiling of 10"}},
		{"far over a ceiling with commas", build(coreList, coreTypes, rowA, rowB), map[string]int{"internal/b": 1500},
			[]string{"internal/b", "holds 1500 non-test Go lines", "500 over its ceiling of 1000"}},
		{"a package that is not there", build(coreList, coreTypes, rowA, rowB, "| `internal/gone` | 500 | 0 lines at abc1234 |"), nil,
			[]string{"internal/gone", "no package stands there"}},
		{"a core package of the list with no row", build(coreList, coreTypes, rowB), nil,
			[]string{"internal/a", "named as core by the list of what is core", "no row"}},
		{"the package of a core type with no row", build(coreList, coreTypes, rowA), nil,
			[]string{"internal/b", "named as core by the Core types table", "no row"}},
		{"a core directory with no row", build("- the pass (`internal/c`)\n"+coreList, coreTypes, rowA, rowB), nil,
			[]string{"internal/c", "named as core by the list of what is core", "no row"}},
		{"two rows for one package", build(coreList, coreTypes, rowA, rowB, rowA), nil, []string{"internal/a", "two rows"}},
		{"a ceiling that is no number", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | lots | 7 lines at abc1234 |"), nil,
			[]string{"internal/c", "ceiling cell", `"lots"`}},
		{"a ceiling of nothing", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 0 | 7 lines at abc1234 |"), nil,
			[]string{"internal/c", "ceiling cell", `"0"`}},
		{"a ceiling with its commas misplaced", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 1,00 | 7 lines at abc1234 |"), nil,
			[]string{"internal/c", "ceiling cell", `"1,00"`}},
		{"a ceiling with words", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 700 lines | 7 lines at abc1234 |"), nil,
			[]string{"internal/c", "ceiling cell", `"700 lines"`}},
		{"a ceiling too large to count", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 99999999999999999999 | 7 lines at abc1234 |"), nil,
			[]string{"internal/c", "ceiling cell", "out of range"}},
		{"a package outside internal", build(coreList, coreTypes, rowA, rowB, "| `packs` | 700 | 7 lines at abc1234 |"), nil,
			[]string{"row 3: ", "package cell", "`packs`"}},
		{"a package without backticks", build(coreList, coreTypes, rowA, rowB, "| internal/c | 700 | 7 lines at abc1234 |"), nil,
			[]string{"row 3: ", "package cell", "internal/c"}},
		{"a package path that climbs", build(coreList, coreTypes, rowA, rowB, "| `internal/../a` | 700 | 7 lines at abc1234 |"), nil,
			[]string{"row 3: ", "package cell"}},
		{"a row of two cells", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 700 |"), nil, []string{"row 3: ", "2 cells"}},
		{"a blank measured cell", build(coreList, coreTypes, rowA, rowB, "| `internal/c` | 700 |  |"), nil,
			[]string{"internal/c", "measured cell is blank"}},
		{"no Line ceilings section", "# The core\n\n## Layers\n\ntext\n", nil, []string{"no ## Line ceilings section"}},
		{"no table row", "# The core\n\n## Line ceilings\n\ntext\n", nil, []string{"no table row"}},
		{"another header", "# The core\n\n## Line ceilings\n\n| package | limit |\n|---|---|\n| `internal/a` | 10 |\n", nil,
			[]string{"header", "| package | ceiling | measured |"}},
		{"no list of what is core", strings.Replace(build(coreList, coreTypes, rowA, rowB), "By this rule these are core:", "Core:", 1), nil,
			[]string{"no list of what is core", "By this rule these are core:"}},
		{"no Core types section", strings.Replace(build(coreList, coreTypes, rowA, rowB), "## Core types", "## Types", 1), nil,
			[]string{"no ## Core types section"}},
		{"a Core types row of two cells", build(coreList, coreTypes+"| flow | `internal/a` |\n", rowA, rowB), nil,
			[]string{"Core types row 3", "2 cells"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ceilingProblems(tc.page, func(pkg string) (int, bool) {
				if n, ok := tc.counts[pkg]; ok {
					return n, true
				}
				n, ok := counts[pkg]
				return n, ok
			})
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Errorf("a right page is refused: %q", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("a page that is wrong in %q gives %d problems, want one: %q", tc.want, len(got), got)
			}
			for _, part := range tc.want {
				if !strings.Contains(got[0], part) {
					t.Errorf("the problem %q does not say %q", got[0], part)
				}
			}
		})
	}

	// What a package's lines are: the non-test .go files directly in its
	// directory, each counted by its newlines as wc -l counts, so that the
	// last line of a file with no newline at its end is not one. A directory
	// with no such file, or none at all, is no package.
	root := t.TempDir()
	for name, body := range map[string]string{
		"p/a.go":          "package p\n\nfunc A() {}\n",
		"p/b.go":          "package p\n\nvar B = 1",
		"p/a_test.go":     "package p\n\nfunc TestA() {}\n",
		"p/notes.txt":     "one\ntwo\n",
		"p/sub/c.go":      "package sub\n",
		"p/testdata/d.go": "package d\n",
		"tests/a_test.go": "package q\n",
		"empty/.keep":     "",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		dir   string
		lines int
		found bool
	}{
		{"p", 5, true}, // a.go 3 and b.go 2, not a_test.go, notes.txt or what is under p
		{"p/sub", 1, true},
		{"tests", 0, false},
		{"empty", 0, false},
		{"missing", 0, false},
	} {
		lines, found, err := packageLines(filepath.Join(root, tc.dir))
		if err != nil || lines != tc.lines || found != tc.found {
			t.Errorf("packageLines(%s) = %d, %v, %v; want %d, %v, nil", tc.dir, lines, found, err, tc.lines, tc.found)
		}
	}
	// A read that fails for any reason but a missing directory is no package
	// that is not there: it is an error the test stops on.
	if _, _, err := packageLines(filepath.Join(root, "p", "a.go")); err == nil {
		t.Error("packageLines of a file is no error, so a row naming one would read as a missing package")
	}
}
