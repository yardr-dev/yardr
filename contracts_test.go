package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// The pages in docs/contracts/ state each contract's rules as a table, and a
// rule is only as good as the check that holds it. This test reads every
// page and holds each row to a check that exists: a Go test that a _test.go
// file of the module declares, a word of the gate script (.yardr/test)
// written gate:<word>, or the word unchecked, which keeps a gap in the open
// where a blank cell would hide it. It holds that the named check exists,
// not that the check holds the rule, which the page's author and its
// reviewer read. The form of a page is the Rules section of
// docs/contracts/core.md, which names this test as the check of its fourth
// rule. It reads text only and starts no process.
func TestContractPagesNameTheirChecks(t *testing.T) {
	t.Parallel()
	// The test runs in the repository root, the root package's directory.
	pages, err := filepath.Glob("docs/contracts/*.md")
	if err != nil {
		t.Fatal(err)
	}
	// Not for want of looking: the core's own page is there.
	if len(pages) == 0 {
		t.Fatal("docs/contracts/ holds no page")
	}
	gate, err := os.ReadFile(".yardr/test")
	if err != nil {
		t.Fatal(err)
	}
	tests, err := declaredTests(".")
	if err != nil {
		t.Fatalf("reading the module's test files: %v", err)
	}
	// Not for want of looking: the module declares thousands.
	if len(tests) < 100 {
		t.Fatalf("%d tests declared: is the test running in the repository root?", len(tests))
	}
	for _, path := range pages {
		page, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range contractProblems(string(page), tests, string(gate)) {
			t.Errorf("%s: %s", filepath.ToSlash(path), problem)
		}
	}
}

// declaredTests is the name of every test the module at root declares, read
// from the _test.go files that go test would run: the walk leaves out what
// ./... leaves out, which is testdata and any directory whose name starts
// with . or _ (.git among them), and it leaves out a directory with a go.mod
// of its own, which is a module of its own, as a checkout nested in the tree
// is. Declarations are read with go/parser, as arch_test.go reads imports.
func declaredTests(root string) (map[string]bool, error) {
	tests := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return fs.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && isTestFunc(fn) {
				tests[fn.Name.Name] = true
			}
		}
		return nil
	})
	return tests, err
}

// isTestFunc reports whether go test runs fn as a test: a function with no
// receiver, named Test and then anything but a lower-case letter, with one
// parameter, a *testing.T. TestMain takes a *testing.M and is no test.
func isTestFunc(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Type.TypeParams != nil {
		return false
	}
	rest, ok := strings.CutPrefix(fn.Name.Name, "Test")
	if !ok {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(rest); rest != "" && unicode.IsLower(r) {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return false
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// contractProblems reads a page's text and says what is wrong with its Rules
// section, one sentence for each fault, the sentence naming the rule it is
// about. The rules are the rows of the table under the ## Rules heading,
// numbered in order, and each has a check cell that checkProblems accepts.
func contractProblems(page string, tests map[string]bool, gate string) []string {
	rows, problem := rulesTable(page)
	if problem != "" {
		return []string{problem}
	}
	var problems []string
	for i, cells := range rows {
		n := strconv.Itoa(i + 1)
		row := "rule " + n + ": "
		if len(cells) != 3 {
			problems = append(problems, row+"the row has "+strconv.Itoa(len(cells))+" cells, want 3: # | rule | check")
			continue
		}
		if cells[0] != n {
			problems = append(problems, row+"the row is numbered "+strconv.Quote(cells[0])+", and the rows run 1, 2, ... in order")
		}
		if cells[1] == "" {
			problems = append(problems, row+"the rule is blank")
		}
		for _, p := range checkProblems(cells[2], tests, gate) {
			problems = append(problems, row+p)
		}
	}
	return problems
}

// rulesTable returns the cells of each row of a page's Rules table, or the
// one problem that keeps it from reading them: the page has no ## Rules
// section or has two, or the section does not hold exactly one table with the
// header | # | rule | check | and a row.
func rulesTable(page string) (rows [][]string, problem string) {
	return sectionTable(page, "Rules", []string{"#", "rule", "check"})
}

// sectionTable returns the cells of each row of the table in a page's
// ## <heading> section, or the one problem that keeps it from reading them:
// the page has no such section or has two, or the section does not hold
// exactly one table with the header cols and a row. The section runs to the
// next heading of level one or two, and what stands in a code fence, a
// heading or a table shown as an example, is no part of the page's structure.
// The Rules tables and core.md's own tables (Core types, Line ceilings, Names
// the core may not name, Files the name scan does not read) are read with it,
// so that a page's tables are one form.
func sectionTable(page, heading string, cols []string) (rows [][]string, problem string) {
	var section []string
	var sections int
	fenced, in := false, false
	for line := range strings.Lines(page) {
		line = strings.TrimRight(line, " \t\r\n")
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			in = line == "## "+heading
			if in {
				sections++
			}
			continue
		}
		if in {
			section = append(section, line)
		}
	}
	switch {
	case sections == 0:
		return nil, "has no ## " + heading + " section"
	case sections > 1:
		return nil, "has " + strconv.Itoa(sections) + " ## " + heading + " sections, want one"
	}

	// The table is the run of lines that start with a pipe: its header, its
	// separator, then its rows. A pipe line after the run is a second table.
	const (
		before = iota
		header
		body
		after
	)
	state := before
	for _, line := range section {
		if !strings.HasPrefix(line, "|") {
			if state == body {
				state = after
			}
			continue
		}
		switch state {
		case before:
			if !slices.Equal(tableCells(line), cols) {
				return nil, "the " + heading + " table's header is " + line + ", want | " + strings.Join(cols, " | ") + " |"
			}
			state = header
		case header:
			if !separatorRow.MatchString(line) {
				return nil, "the " + heading + " table's header is not followed by a separator row of dashes"
			}
			state = body
		case body:
			rows = append(rows, tableCells(line))
		case after:
			return nil, "## " + heading + " holds a second table, want one"
		}
	}
	if len(rows) == 0 {
		return nil, "## " + heading + " holds no table row"
	}
	return rows, ""
}

// separatorRow is the line under a table's header: cells of dashes, with the
// colons that align them.
var separatorRow = regexp.MustCompile(`^\|(\s*:?-+:?\s*\|)+\s*$`)

// tableCells splits one table row into its trimmed cells. A pipe written \|
// stays in its cell.
func tableCells(row string) []string {
	row = strings.ReplaceAll(strings.TrimSpace(row), `\|`, "\x00")
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	cells := strings.Split(row, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(strings.ReplaceAll(c, "\x00", "|"))
	}
	return cells
}

// checkNames is a check cell that names its checks: names in backticks, each
// without a space, separated by ", ".
var checkNames = regexp.MustCompile("^`[^` ]+`(, `[^` ]+`)*$")

// checkProblems holds one check cell to the Rules section's form and says
// what is wrong with it, naming the check it could not find. The cell is the
// word unchecked, or names in backticks: a test that tests declares, or
// gate:<word> for a word of the gate script. A word of the script is whole
// when no letter, digit or underscore stands against it, so a part of a name
// (gate:file for filesizes) finds no step.
func checkProblems(cell string, tests map[string]bool, gate string) []string {
	switch {
	case cell == "":
		return []string{"the check cell is blank: write unchecked, or name the test or gate step that holds the rule"}
	case cell == "unchecked":
		return nil
	case !checkNames.MatchString(cell):
		return []string{"the check cell " + strconv.Quote(cell) + " is neither unchecked nor names in backticks separated by \", \""}
	}
	var problems []string
	for name := range strings.SplitSeq(strings.Trim(cell, "`"), "`, `") {
		word, isGate := strings.CutPrefix(name, "gate:")
		switch {
		case isGate && word == "":
			problems = append(problems, "`gate:` names no step: write gate:<word>")
		case isGate:
			whole := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_])`)
			if !whole.MatchString(gate) {
				problems = append(problems, "`"+name+"`: .yardr/test has no word "+word)
			}
		case !tests[name]:
			problems = append(problems, "`"+name+"`: no func "+name+"(t *testing.T) is declared in a _test.go file of the module")
		}
	}
	return problems
}

// The refusals fire. Each page below is right or wrong in one way, and the
// problem contractProblems gives for it must name the rule and the check it
// is about. The pages are written here because the real ones have no fault
// to show. The rest of the test holds what a declared name is: the functions
// isTestFunc accepts, and the files declaredTests reads them from.
func TestContractChecksRefuse(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{"TestDeclared": true}
	gate := "filesizebudget() {\n  filesizes='\n"
	// rules is a page whose Rules table holds the rows given, followed by a
	// section whose table has a blank check, which no one reads.
	rules := func(rows ...string) string {
		return "# A contract\n\nText.\n\n## Rules\n\n| # | rule | check |\n|---|---|---|\n" +
			strings.Join(rows, "\n") + "\n\n## After\n\n| 1 | not a rule | |\n"
	}
	for _, tc := range []struct {
		name string
		page string
		want []string // parts of the first problem; none means the page is right
	}{
		{"unchecked", rules("| 1 | a rule | unchecked |"), nil},
		{"a declared name", rules("| 1 | a rule | `TestDeclared` |"), nil},
		{"a gate word", rules("| 1 | a rule | `gate:filesizes` |"), nil},
		{"two names", rules("| 1 | a rule | `TestDeclared`, `gate:filesizes` |"), nil},
		{"a pipe in a rule", rules(`| 1 | a \| b | unchecked |`), nil},
		{"a table in a fence", "# A contract\n\n## Rules\n\n```\n| # | rule | check |\n|---|---|---|\n| 1 | an example | |\n```\n\n| # | rule | check |\n|---|---|---|\n| 1 | a rule | unchecked |\n", nil},
		{"a blank cell", rules("| 1 | a rule |  |"), []string{"rule 1: ", "check cell is blank"}},
		{"an undeclared name", rules("| 1 | a rule | `TestUndeclared` |"), []string{"rule 1: ", "`TestUndeclared`"}},
		{"a name without Test", rules("| 1 | a rule | `Declared` |"), []string{"rule 1: ", "`Declared`"}},
		{"an absent gate word", rules("| 1 | a rule | `gate:nosuchstep` |"), []string{"rule 1: ", "`gate:nosuchstep`", "nosuchstep"}},
		{"part of a gate word", rules("| 1 | a rule | `gate:filesize` |"), []string{"rule 1: ", "`gate:filesize`"}},
		{"a gate with no word", rules("| 1 | a rule | `gate:` |"), []string{"rule 1: ", "`gate:`"}},
		{"the second name", rules("| 1 | a rule | `TestDeclared`, `TestUndeclared` |"), []string{"rule 1: ", "`TestUndeclared`"}},
		{"the second row", rules("| 1 | a rule | unchecked |", "| 2 | a rule | `TestUndeclared` |"), []string{"rule 2: ", "`TestUndeclared`"}},
		{"a name without backticks", rules("| 1 | a rule | TestDeclared |"), []string{"rule 1: ", `"TestDeclared"`}},
		{"a name with one backtick", rules("| 1 | a rule | `TestDeclared |"), []string{"rule 1: "}},
		{"unchecked beside a name", rules("| 1 | a rule | unchecked, `TestDeclared` |"), []string{"rule 1: ", `"unchecked, `}},
		{"names without the comma", rules("| 1 | a rule | `TestDeclared` `TestDeclared` |"), []string{"rule 1: "}},
		{"a row out of order", rules("| 1 | a rule | unchecked |", "| 3 | a rule | unchecked |"), []string{"rule 2: ", `"3"`}},
		{"a blank rule", rules("| 1 |  | unchecked |"), []string{"rule 1: ", "rule is blank"}},
		{"a row of two cells", rules("| 1 | a rule |"), []string{"rule 1: ", "2 cells"}},
		{"no Rules section", "# A contract\n\n## Form\n\ntext\n", []string{"no ## Rules section"}},
		{"a Rules heading in a fence", "# A contract\n\n```\n## Rules\n```\n", []string{"no ## Rules section"}},
		{"two Rules sections", rules("| 1 | a rule | unchecked |") + "\n## Rules\n\ntext\n", []string{"2 ## Rules sections"}},
		{"no table row", "# A contract\n\n## Rules\n\n| # | rule | check |\n|---|---|---|\n\n## After\n", []string{"no table row"}},
		{"no table", "# A contract\n\n## Rules\n\nunchecked.\n", []string{"no table row"}},
		{"another header", "# A contract\n\n## Rules\n\n| rule | check |\n|---|---|\n| a rule | unchecked |\n", []string{"header", "| # | rule | check |"}},
		{"no separator row", "# A contract\n\n## Rules\n\n| # | rule | check |\n| 1 | a rule | unchecked |\n", []string{"separator"}},
		{"a second table", "# A contract\n\n## Rules\n\n| # | rule | check |\n|---|---|---|\n| 1 | a rule | unchecked |\n\n| # | rule | check |\n|---|---|---|\n| 1 | a rule | unchecked |\n", []string{"second table"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := contractProblems(tc.page, tests, gate)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Errorf("a right page is refused: %q", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("a page that is wrong in %q is accepted", tc.want)
			}
			for _, part := range tc.want {
				if !strings.Contains(got[0], part) {
					t.Errorf("the problem %q does not say %q", got[0], part)
				}
			}
		})
	}

	// What go test runs as a test, and so what a page may name.
	src := `package p
func TestA(t *testing.T) {}
func Test(t *testing.T) {}
func Test_a(t *testing.T) {}
func TestÄ(t *testing.T) {}
func Testa(t *testing.T) {}
func TestMain(m *testing.M) {}
func TestB(b *testing.B) {}
func TestC() {}
func TestD(t *testing.T, n int) {}
func (s S) TestE(t *testing.T) {}
func TestF[T any](t *testing.T) {}
func TestG(t *T) {}
func TestH(t testing.T) {}
func helper(t *testing.T) {}
`
	file, err := parser.ParseFile(token.NewFileSet(), "p_test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"TestA": true, "Test": true, "Test_a": true, "TestÄ": true}
	var seen int
	for _, decl := range file.Decls {
		fn := decl.(*ast.FuncDecl)
		seen++
		if got := isTestFunc(fn); got != want[fn.Name.Name] {
			t.Errorf("isTestFunc(%s) = %v, want %v", fn.Name.Name, got, want[fn.Name.Name])
		}
	}
	if declared := strings.Count(src, "\nfunc "); seen != declared {
		t.Errorf("%d functions read, the source declares %d", seen, declared)
	}

	// And where go test finds them: the walk leaves out testdata, directories
	// that start with . or _, and a module of its own, and reads only
	// _test.go files.
	root := t.TempDir()
	for path, name := range map[string]string{
		"a_test.go":              "TestA",
		"sub/b_test.go":          "TestB",
		"sub/b.go":               "TestNotInATestFile",
		"sub/testdata/c_test.go": "TestInTestdata",
		".hidden/d_test.go":      "TestInADotDirectory",
		"_ignored/e_test.go":     "TestInAnUnderscoreDirectory",
		"nested/go.mod":          "",
		"nested/f_test.go":       "TestInANestedModule",
	} {
		body := "module nested\n"
		if name != "" {
			body = "package p\n\nfunc " + name + "(t *testing.T) {}\n"
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := declaredTests(root)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, map[string]bool{"TestA": true, "TestB": true}) {
		t.Errorf("the walk declares %v, want TestA and TestB alone", slices.Sorted(maps.Keys(got)))
	}
}
