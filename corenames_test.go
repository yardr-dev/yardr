package main

import (
	"cmp"
	"fmt"
	"go/scanner"
	"go/token"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The core must not learn what is outside it, and a name is where that shows.
// docs/contracts/core.md lists, under Names the core may not name, the harness
// kinds, the providers and the herdr command words that the core's code may
// not write, and this test holds the non-test Go of each package in its Line
// ceilings table to the list, less the files the page puts on the
// how-and-what side by path. A name matches a whole string literal and a
// whole part of an identifier, comments skipped. Today's hits are
// coreNamesAllowed below, rows that only fall, so that a new hit fails from the
// day the list lands and a hit that has gone cannot come back. The page names
// this test as the check of its sixth rule. It reads files and starts no
// process.
func TestCoreNamesNothingOnTheList(t *testing.T) {
	t.Parallel()
	// The test runs in the repository root, the root package's directory.
	page, err := os.ReadFile("docs/contracts/core.md")
	if err != nil {
		t.Fatal(err)
	}
	files, err := loadRepoGoFiles()
	if err != nil {
		t.Fatal(err)
	}
	problems, read := coreNameProblems(string(page), files, coreNamesAllowed)
	for _, problem := range problems {
		t.Error(problem)
	}
	// Not for want of looking: the ceilings table names the core's packages,
	// which hold hundreds of files.
	if len(problems) == 0 && read < 100 {
		t.Fatalf("%d Go files read of the packages in Line ceilings", read)
	}
}

// A new kind of harness is a file in harnesses/, and the core may not name the
// kind: so the kind joins the list in the commit that adds its file, and this
// test fails until it does. The kind of a file is its name, as agents.Load
// reads it (claude.yaml is claude). The page names this test as the check of
// its seventh rule.
func TestHarnessFilesNameOnlyListedKinds(t *testing.T) {
	t.Parallel()
	page, err := os.ReadFile("docs/contracts/core.md")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("harnesses")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			files = append(files, entry.Name())
		}
	}
	for _, problem := range harnessKindProblems(string(page), files) {
		t.Error(problem)
	}
	// Not for want of looking: the directory holds a file for each kind.
	if n := countSuffix(files, ".yaml"); n < 5 {
		t.Fatalf("%d harness files found in harnesses/: is the test running in the repository root?", n)
	}
}

// harnessKindsGroup is the row of the list that the harness files are held to.
const harnessKindsGroup = "harness kinds"

// nameAllowance is the hits a file may keep of one listed name: a row of
// coreNamesAllowed. It is held in this file, with the check, so that a row
// falls in the commit that moves a hit out and is seen when one is added.
type nameAllowance struct {
	file string
	name string
	hits int
}

// nameKey is a file and a name, what a row of the allow-list is for.
type nameKey struct{ file, name string }

var (
	nameListCols = []string{"group", "names"}
	skippedCols  = []string{"file", "listed as"}
	// listedName is a name of the list: one lowercase word of letters and
	// digits, so that a whole literal or whole parts of an identifier can
	// equal it.
	listedName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	// skippedFile is a file cell of Files the name scan does not read: one
	// non-test Go file under internal/ in backticks.
	skippedFile = regexp.MustCompile("^`(internal/[a-z][a-z0-9_]*(/[a-z][a-z0-9_]*)*/[a-z][a-z0-9_]*\\.go)`$")
)

// listedNames reads the table under Names the core may not name into the group
// of each listed name. A row that cannot be read, a name that is no word, and a
// name or a group twice are problems, each naming the row, and the result is
// then not to be trusted.
func listedNames(page string) (groups map[string]string, problems []string) {
	const heading = "Names the core may not name"
	rows, problem := sectionTable(page, heading, nameListCols)
	if problem != "" {
		return nil, []string{problem}
	}
	groups = map[string]string{}
	seen := map[string]bool{}
	for i, cells := range rows {
		row := heading + " row " + strconv.Itoa(i+1) + ": "
		if len(cells) != len(nameListCols) {
			problems = append(problems, row+"the row has "+strconv.Itoa(len(cells))+" cells, want 2: group | names")
			continue
		}
		group := cells[0]
		switch {
		case group == "":
			problems = append(problems, row+"the group is blank")
			continue
		case seen[group]:
			problems = append(problems, row+"the group "+strconv.Quote(group)+" has two rows, want one")
			continue
		}
		seen[group] = true
		// The names are written as a check cell's are: in backticks, with
		// no space inside one, separated by ", ".
		if !checkNames.MatchString(cells[1]) {
			problems = append(problems, row+"the names cell "+strconv.Quote(cells[1])+" is not names in backticks separated by \", \"")
			continue
		}
		for name := range strings.SplitSeq(strings.Trim(cells[1], "`"), "`, `") {
			switch {
			case !listedName.MatchString(name):
				problems = append(problems, row+"the name "+strconv.Quote(name)+" is not one lowercase word of letters and digits")
			case groups[name] != "":
				problems = append(problems, row+"the name "+strconv.Quote(name)+" is already listed under "+strconv.Quote(groups[name]))
			default:
				groups[name] = group
			}
		}
	}
	return groups, problems
}

// ceilingPackages is the set of packages in the Line ceilings table: the
// directories whose own files the name scan reads. A row whose package cell
// cannot be read is for ceilingProblems to say, and is left out here.
func ceilingPackages(page string) (pkgs map[string]bool, problem string) {
	rows, problem := sectionTable(page, "Line ceilings", ceilingCols)
	if problem != "" {
		return nil, problem
	}
	pkgs = map[string]bool{}
	for _, cells := range rows {
		if m := ceilingPackage.FindStringSubmatch(cells[0]); m != nil {
			pkgs[m[1]] = true
		}
	}
	return pkgs, ""
}

// unreadFiles is the set of files in the table under Files the name scan does
// not read. Each row must say where the page lists the file, and name a Go file
// that is there and stands in a package of the ceilings table: a row for any
// other file is dead, and would go on calling a file how-and-what after it has
// moved. have is the set of non-test Go files of the repository.
func unreadFiles(page string, pkgs, have map[string]bool) (skip map[string]bool, problems []string) {
	const heading = "Files the name scan does not read"
	rows, problem := sectionTable(page, heading, skippedCols)
	if problem != "" {
		return nil, []string{problem}
	}
	skip = map[string]bool{}
	for i, cells := range rows {
		row := heading + " row " + strconv.Itoa(i+1) + ": "
		if len(cells) != len(skippedCols) {
			problems = append(problems, row+"the row has "+strconv.Itoa(len(cells))+" cells, want 2: file | listed as")
			continue
		}
		m := skippedFile.FindStringSubmatch(cells[0])
		if m == nil || strings.HasSuffix(m[1], "_test.go") {
			problems = append(problems, row+"the file cell "+strconv.Quote(cells[0])+" is not one non-test Go file under internal/ in backticks")
			continue
		}
		file := m[1]
		switch {
		case cells[1] == "":
			problems = append(problems, file+" has a blank listed as cell in "+heading+": write the entry of the how-and-what list in What is core that names it")
		case skip[file]:
			problems = append(problems, file+" has two rows in "+heading+", want one")
		case !have[file]:
			problems = append(problems, file+" has a row in "+heading+" and no such file is there (moved or removed?): change the row with it")
		// A ceilings table that cannot be read has said so, and names no
		// package that a row could stand in.
		case pkgs != nil && !pkgs[path.Dir(file)]:
			problems = append(problems, file+" has a row in "+heading+" and stands in no package of Line ceilings, so the scan never reads it: delete the row")
		}
		skip[file] = true
	}
	return skip, problems
}

// coreNameProblems says what is wrong with the core's names, one sentence for
// each fault: a table of the page that cannot be read, a row of the allow-list
// that is no row, a hit beyond what the allow-list holds (naming its file, its
// line and the name) and a row that holds more than its file has. files are
// the non-test Go files of the repository, of which it reads those directly in
// a package of Line ceilings, less the files the page does not have read; read
// says how many that is. It is a function of its arguments so that the
// refusals can be tried against made-up pages and files.
func coreNameProblems(page string, files []repoGoFile, allowed []nameAllowance) (problems []string, read int) {
	const where = "docs/contracts/core.md: "
	names, more := listedNames(page)
	for _, p := range more {
		problems = append(problems, where+p)
	}
	pkgs, problem := ceilingPackages(page)
	if problem != "" {
		problems = append(problems, where+problem)
	}
	have := make(map[string]bool, len(files))
	for _, f := range files {
		have[f.path] = true
	}
	skip, more := unreadFiles(page, pkgs, have)
	for _, p := range more {
		problems = append(problems, where+p)
	}
	// A page that cannot be read says so and no more: what a scan found by it
	// would be a guess.
	if len(problems) > 0 {
		return problems, 0
	}

	allow := map[nameKey]int{}
	for _, a := range allowed {
		key := nameKey{a.file, a.name}
		switch {
		case a.hits < 1:
			problems = append(problems, a.file+" has a row for "+strconv.Quote(a.name)+" in coreNamesAllowed that allows "+strconv.Itoa(a.hits)+
				" hits, want one or more: delete a row that allows none")
		case names[a.name] == "":
			problems = append(problems, a.file+" has a row in coreNamesAllowed for "+strconv.Quote(a.name)+
				", which is not on the list under Names the core may not name: delete the row")
		case allow[key] != 0:
			problems = append(problems, a.file+" has two rows in coreNamesAllowed for "+strconv.Quote(a.name)+", want one")
		default:
			allow[key] = a.hits
		}
	}

	lines := map[nameKey][]int{}
	for _, f := range files {
		if !pkgs[path.Dir(f.path)] || skip[f.path] {
			continue
		}
		read++
		// The walk split the file at its newlines, which the join restores.
		for _, h := range nameHits([]byte(strings.Join(f.lines, "\n")), names) {
			key := nameKey{f.path, h.name}
			lines[key] = append(lines[key], h.line)
		}
	}
	keys := slices.Collect(maps.Keys(lines))
	for key := range allow {
		if _, ok := lines[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b nameKey) int {
		return cmp.Or(strings.Compare(a.file, b.file), strings.Compare(a.name, b.name))
	})
	for _, key := range keys {
		got, n := lines[key], allow[key]
		switch {
		case len(got) > n:
			problems = append(problems, fmt.Sprintf("%s:%d names %q (%s): the file holds %s of it, on %s, and coreNamesAllowed allows %d; "+
				"the core may not name it, so keep the code that does outside the packages of Line ceilings "+
				"(docs/contracts/core.md, Names the core may not name)",
				key.file, got[n], key.name, names[key.name], hitsOf(len(got)), lineList(got), n))
		case len(got) == 0 && n > 0:
			problems = append(problems, fmt.Sprintf("%s holds no hit of %q and its row in coreNamesAllowed allows %d: delete the row, "+
				"so that a hit that has gone cannot come back", key.file, key.name, n))
		case len(got) < n:
			problems = append(problems, fmt.Sprintf("%s holds %s of %q and its row in coreNamesAllowed allows %d: lower the row to %d, "+
				"so that a hit that has gone cannot come back", key.file, hitsOf(len(got)), key.name, n, len(got)))
		}
	}
	return problems, read
}

// hitsOf and lineList put a count and a list of lines into a sentence.
func hitsOf(n int) string {
	if n == 1 {
		return "1 hit"
	}
	return strconv.Itoa(n) + " hits"
}

func lineList(lines []int) string {
	// A line can hold two hits (a literal and an identifier of one name), and
	// is listed once.
	var words []string
	for i, l := range lines {
		if i == 0 || l != lines[i-1] {
			words = append(words, strconv.Itoa(l))
		}
	}
	if len(words) == 1 {
		return "line " + words[0]
	}
	return "lines " + strings.Join(words, ", ")
}

// countSuffix is how many of names end in suffix.
func countSuffix(names []string, suffix string) int {
	var n int
	for _, name := range names {
		if strings.HasSuffix(name, suffix) {
			n++
		}
	}
	return n
}

// harnessKindProblems says which harness file is named for a kind that is not
// in the harness kinds row of the list. files are the names in harnesses/; a
// file that is no .yaml file names no kind.
func harnessKindProblems(page string, files []string) []string {
	const where = "docs/contracts/core.md: "
	names, problems := listedNames(page)
	for i, p := range problems {
		problems[i] = where + p
	}
	if len(problems) > 0 {
		return problems
	}
	kinds := map[string]bool{}
	for name, group := range names {
		if group == harnessKindsGroup {
			kinds[name] = true
		}
	}
	if len(kinds) == 0 {
		return []string{where + "the list under Names the core may not name has no row for " + strconv.Quote(harnessKindsGroup) +
			", which the harness files are held to"}
	}
	for _, file := range files {
		kind, ok := strings.CutSuffix(file, ".yaml")
		if ok && !kinds[kind] {
			problems = append(problems, "harnesses/"+file+" names the kind "+strconv.Quote(kind)+", which is not on the list of "+harnessKindsGroup+
				" under Names the core may not name in docs/contracts/core.md: add it there in the commit that adds the file")
		}
	}
	return problems
}

// nameHit is one place that a listed name is written: the line of the token
// (from 1) and the name.
type nameHit struct {
	line int
	name string
}

// nameHits finds the listed names in one Go source file, token by token, so
// that a comment holds none. names maps each name to its group. A string
// literal is a hit when its whole value is a name. An identifier is a hit for
// each name that one or more of its parts, in a row, spell (identParts): kindPi
// and Pi and "pi" are hits for pi, OpenCode is one for opencode, and pick and
// PID are none. The case of a name is ignored. A fault in the source is not
// this test's to report.
func nameHits(src []byte, names map[string]string) []nameHit {
	longest := 0
	for name := range names {
		longest = max(longest, len(name))
	}
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	var hits []nameHit
	for {
		pos, tok, lit := s.Scan()
		switch tok {
		case token.EOF:
			return hits
		case token.STRING:
			if v, err := strconv.Unquote(lit); err == nil {
				if v = strings.ToLower(v); names[v] != "" {
					hits = append(hits, nameHit{file.Line(pos), v})
				}
			}
		case token.IDENT:
			parts := identParts(lit)
			for i := range parts {
				spelled := ""
				for _, part := range parts[i:] {
					spelled += part
					if names[spelled] != "" {
						hits = append(hits, nameHit{file.Line(pos), spelled})
					}
					if len(spelled) >= longest {
						break
					}
				}
			}
		}
	}
}

// identParts splits an identifier into its words in lower case: at an
// underscore, around a run of digits, between a lower-case letter and an
// upper-case one, and before the last capital of a run of capitals that a
// lower-case letter follows (HTTPServer is HTTP and Server).
func identParts(id string) []string {
	var parts []string
	runes := []rune(id)
	start := -1
	flush := func(end int) {
		if start >= 0 {
			parts = append(parts, strings.ToLower(string(runes[start:end])))
			start = -1
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || unicode.IsDigit(r):
			flush(i)
		case start < 0:
			start = i
		case unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1])):
			flush(i)
			start = i
		}
	}
	flush(len(runes))
	return parts
}

// The refusals fire. Each page below is right or wrong in one way, and the
// problem coreNameProblems gives for it must name the file and the fault, and
// be the only one. The pages and files are made up here because the real page
// has no fault to show. What a hit is, how an identifier is split and which
// harness file is refused are held on small sources after them.
func TestCoreNamesRefuse(t *testing.T) {
	t.Parallel()
	const (
		list = "| harness kinds | `claude`, `opencode` |\n| providers | `google` |\n"
		// internal/a and internal/b are packages of the ceilings table, and
		// internal/a has a file that the page does not have read.
		skipped  = "| `internal/a/skip.go` | the daemon's passes |\n"
		ceilings = "| `internal/a` | 100 | 5 lines at abc1234 |\n| `internal/b` | 100 | 5 lines at abc1234 |\n"
		hit      = "package a\n\nvar kind = \"claude\"\n"
	)
	// build is a made-up core.md: the Line ceilings table, the list and the
	// files given, followed by a section whose table is none of theirs, which
	// no one reads.
	build := func(rowsOfCeilings, rowsOfList, rowsOfFiles string) string {
		return "# The core\n\n## Line ceilings\n\nText.\n\n| package | ceiling | measured |\n|---|---:|---|\n" + rowsOfCeilings +
			"\n## Names the core may not name\n\nText.\n\n| group | names |\n|---|---|\n" + rowsOfList +
			"\n## Files the name scan does not read\n\nText.\n\n| file | listed as |\n|---|---|\n" + rowsOfFiles +
			"\n## After\n\n| 1 | not a list | |\n"
	}
	page := build(ceilings, list, skipped)
	repo := map[string]string{
		"internal/a/x.go":    "package a\n",
		"internal/a/skip.go": "package a\n",
		"internal/b/y.go":    "package b\n",
		"internal/c/z.go":    "package c\n",
	}
	// with is the repo and one file more, or a file with other lines.
	with := func(file, src string) map[string]string {
		m := maps.Clone(repo)
		m[file] = src
		return m
	}
	row := func(file, name string, hits int) nameAllowance { return nameAllowance{file, name, hits} }
	for _, tc := range []struct {
		name    string
		page    string
		repo    map[string]string
		allowed []nameAllowance
		want    []string // parts of the one problem; none means the page is right
	}{
		{"no hit", page, repo, nil, nil},
		{"a hit the row holds", page, with("internal/a/x.go", hit), []nameAllowance{row("internal/a/x.go", "claude", 1)}, nil},
		{"a hit with no row", page, with("internal/a/x.go", hit), nil,
			[]string{"internal/a/x.go:3 ", `"claude"`, "(harness kinds)", "1 hit", "line 3", "allows 0", "outside the packages of Line ceilings"}},
		{"a hit of a name that is not the row's", page, with("internal/a/x.go", "package a\n\nvar a, b = \"claude\", \"google\"\n"),
			[]nameAllowance{row("internal/a/x.go", "claude", 1)},
			[]string{"internal/a/x.go:3 ", `"google"`, "(providers)", "allows 0"}},
		{"a second hit beyond the row", page, with("internal/a/x.go", hit+"var more = \"claude\"\n"),
			[]nameAllowance{row("internal/a/x.go", "claude", 1)},
			[]string{"internal/a/x.go:4 ", `"claude"`, "2 hits", "lines 3, 4", "allows 1"}},
		{"two hits on one line", page, with("internal/a/x.go", "package a\n\nvar _ = claude(\"claude\")\n"), nil,
			[]string{"internal/a/x.go:3 ", "2 hits of it, on line 3, and"}},
		{"a row that holds more than the file", page, with("internal/a/x.go", hit), []nameAllowance{row("internal/a/x.go", "claude", 3)},
			[]string{"internal/a/x.go ", "1 hit", `"claude"`, "allows 3", "lower the row to 1"}},
		{"a row for a file with no hit", page, repo, []nameAllowance{row("internal/a/x.go", "claude", 2)},
			[]string{"internal/a/x.go ", "no hit", `"claude"`, "allows 2", "delete the row"}},
		{"a row for a file that is gone", page, repo, []nameAllowance{row("internal/a/gone.go", "claude", 1)},
			[]string{"internal/a/gone.go ", "no hit", "delete the row"}},
		{"a row for a name not on the list", page, with("internal/a/x.go", hit),
			[]nameAllowance{row("internal/a/x.go", "slack", 1), row("internal/a/x.go", "claude", 1)},
			[]string{"internal/a/x.go ", `"slack"`, "not on the list"}},
		{"two rows for a file and a name", page, with("internal/a/x.go", hit),
			[]nameAllowance{row("internal/a/x.go", "claude", 1), row("internal/a/x.go", "claude", 1)},
			[]string{"internal/a/x.go ", "two rows", `"claude"`}},
		{"a row that allows none", page, repo, []nameAllowance{row("internal/a/x.go", "claude", 0)},
			[]string{"internal/a/x.go ", "allows 0 hits", "one or more"}},
		{"a hit in a file the page does not have read", page, with("internal/a/skip.go", hit), nil, nil},
		{"a hit in a package with no row", page, with("internal/c/z.go", hit), nil, nil},
		{"a hit below a package's directory", page, with("internal/a/sub/x.go", hit), nil, nil},
		{"a hit in a comment", page, with("internal/a/x.go", "package a\n\n// claude is a kind\n/* google */\nvar x = 1 // opencode\n"), nil, nil},
		{"a name inside a longer literal", page, with("internal/a/x.go", "package a\n\nvar x = \"claude code\"\nvar y = `google/x`\n"), nil, nil},
		{"a name as a part of an identifier", page, with("internal/a/x.go", "package a\n\nvar kindClaude = 1\n"), nil,
			[]string{"internal/a/x.go:3 ", `"claude"`}},
		{"a skipped file that is gone", build(ceilings, list, skipped+"| `internal/a/gone.go` | the daemon's passes |\n"), repo, nil,
			[]string{"internal/a/gone.go", "no such file is there", "moved or removed"}},
		{"a skipped file in no ceilings package", build(ceilings, list, skipped+"| `internal/c/z.go` | the daemon's passes |\n"), repo, nil,
			[]string{"internal/c/z.go", "no package of Line ceilings"}},
		{"a skipped file twice", build(ceilings, list, skipped+skipped), repo, nil, []string{"internal/a/skip.go", "two rows"}},
		{"a skipped test file", build(ceilings, list, skipped+"| `internal/a/x_test.go` | tests |\n"), repo, nil,
			[]string{"row 2", "file cell", "non-test Go file"}},
		{"a skipped file without backticks", build(ceilings, list, "| internal/a/skip.go | the daemon's passes |\n"), repo, nil,
			[]string{"row 1", "file cell"}},
		{"a skipped file listed as nothing", build(ceilings, list, "| `internal/a/skip.go` |  |\n"), repo, nil,
			[]string{"internal/a/skip.go", "blank listed as cell"}},
		{"a skipped row of three cells", build(ceilings, list, skipped+"| `internal/a/skip.go` | a | b |\n"), repo, nil,
			[]string{"row 2", "3 cells"}},
		{"a name in capitals", build(ceilings, "| harness kinds | `Claude` |\n", skipped), repo, nil,
			[]string{"row 1", `"Claude"`, "lowercase word"}},
		{"a name with a hyphen", build(ceilings, "| harness kinds | `kiro-cli` |\n", skipped), repo, nil,
			[]string{"row 1", `"kiro-cli"`, "lowercase word"}},
		{"a name listed twice", build(ceilings, list+"| herdr's command words | `claude` |\n", skipped), repo, nil,
			[]string{"row 3", `"claude"`, `already listed under "harness kinds"`}},
		{"a group of two rows", build(ceilings, list+"| providers | `slack` |\n", skipped), repo, nil,
			[]string{"row 3", `"providers"`, "two rows"}},
		{"a group with no name", build(ceilings, "|  | `claude` |\n", skipped), repo, nil, []string{"row 1", "group is blank"}},
		{"names without backticks", build(ceilings, "| harness kinds | claude, opencode |\n", skipped), repo, nil,
			[]string{"row 1", "names cell", "claude, opencode"}},
		{"a list row of three cells", build(ceilings, list+"| a | `b` | c |\n", skipped), repo, nil, []string{"row 3", "3 cells"}},
		{"no list section", strings.Replace(page, "## Names the core may not name", "## Names", 1), repo, nil,
			[]string{"no ## Names the core may not name section"}},
		{"another list header", strings.Replace(page, "| group | names |", "| group | words |", 1), repo, nil,
			[]string{"header", "| group | names |"}},
		{"no files section", strings.Replace(page, "## Files the name scan does not read", "## Files", 1), repo, nil,
			[]string{"no ## Files the name scan does not read section"}},
		{"no ceilings section", strings.Replace(page, "## Line ceilings", "## Ceilings", 1), repo, nil,
			[]string{"no ## Line ceilings section"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := coreNameProblems(tc.page, goFilesOf(tc.repo), tc.allowed)
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
	// The files the scan reads are counted: those directly in a package of the
	// table, less the page's own.
	if _, read := coreNameProblems(page, goFilesOf(repo), nil); read != 2 {
		t.Errorf("%d files read of a repo with two to read", read)
	}

	// What a hit is: a whole literal, or whole parts of an identifier in a
	// row, on its line, and never a comment, the inside of a literal or the
	// inside of a part.
	names := map[string]string{"pi": "kinds", "opencode": "kinds", "google": "providers", "claude": "kinds"}
	for _, tc := range []struct {
		name string
		src  string
		want []string // line and name of each hit, in order
	}{
		{"a whole literal", "package p\nvar a = \"pi\"\n", []string{"2 pi"}},
		{"a literal in capitals", "package p\nvar a = \"Google\"\n", []string{"2 google"}},
		{"a raw literal", "package p\nvar a = `claude`\n", []string{"2 claude"}},
		{"a literal that holds more", "package p\nvar a, b, c = \"pi \", \"a pi\", \"pie\"\n", nil},
		{"a raw literal that holds a line more", "package p\nvar a = `claude\n`\n", nil},
		{"the identifier itself", "package p\nvar pi = 1\nvar _ = math.Pi\n", []string{"2 pi", "3 pi"}},
		{"a part after a lower-case letter", "package p\nvar kindPi = 1\n", []string{"2 pi"}},
		{"a part before an upper-case letter", "package p\nvar piKind = 1\n", []string{"2 pi"}},
		{"a part between underscores", "package p\nvar kind_pi_x = 1\n", []string{"2 pi"}},
		{"a part before a digit", "package p\nvar pi2 = 1\nvar claude3x = 2\n", []string{"2 pi", "3 claude"}},
		{"a method", "package p\nvar _ = x.Claude()\nvar _ = x.google()\n", []string{"2 claude", "3 google"}},
		{"the inside of a part", "package p\nvar pick, pid, PID, piano, spider, Pie, googles = 1, 2, 3, 4, 5, 6, 7\n", nil},
		{"several parts in a row", "package p\nvar OpenCode, openCode, OPENCODE, isOpencode = 1, 2, 3, 4\n", []string{"2 opencode", "2 opencode", "2 opencode", "2 opencode"}},
		{"parts that run on past the name", "package p\nvar OpenCoder, OpenCodex, Open, Code = 1, 2, 3, 4\n", nil},
		{"two hits on a line", "package p\nvar a, b = \"pi\", \"google\"\n", []string{"2 pi", "2 google"}},
		{"hits on two lines", "package p\nvar a = \"pi\"\n\nvar b = pi\n", []string{"2 pi", "4 pi"}},
		{"a line comment", "package p\n// pi claude\nvar a = 1 // google\n", nil},
		{"a block comment", "package p\n/* pi\nclaude */\nvar a = 1\n", nil},
		{"a struct tag", "package p\ntype T struct {\n\tA int `json:\"claude\"`\n}\n", nil},
		{"an import path", "package p\nimport \"example.com/claude\"\n", nil},
		{"a rune", "package p\nvar a = 'p'\n", nil},
		{"a keyword and a blank", "package p\nvar _ = func() { for range 1 {} }\n", nil},
	} {
		var got []string
		for _, h := range nameHits([]byte(tc.src), names) {
			got = append(got, strconv.Itoa(h.line)+" "+h.name)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: hits %q, want %q", tc.name, got, tc.want)
		}
	}

	// How an identifier is split into the parts a name must equal whole.
	for id, want := range map[string][]string{
		"kindPi":        {"kind", "pi"},
		"Pi":            {"pi"},
		"HTTPServer":    {"http", "server"},
		"bSessPane":     {"b", "sess", "pane"},
		"herdr_session": {"herdr", "session"},
		"claude3x":      {"claude", "x"},
		"PID":           {"pid"},
		"xID":           {"x", "id"},
		"OpenCode":      {"open", "code"},
		"_":             nil,
		"":              nil,
	} {
		if got := identParts(id); !slices.Equal(got, want) {
			t.Errorf("identParts(%q) = %q, want %q", id, got, want)
		}
	}

	// Which harness file is refused: one named for a kind that is not in the
	// harness kinds row, whatever else is on the list.
	for _, tc := range []struct {
		name  string
		page  string
		files []string
		want  []string // parts of the one problem; none means the files are right
	}{
		{"every kind listed", page, []string{"claude.yaml", "opencode.yaml", "harnesses.go", "README.md"}, nil},
		{"no file", page, nil, nil},
		{"a kind not on the list", page, []string{"claude.yaml", "newkind.yaml"},
			[]string{"harnesses/newkind.yaml", `"newkind"`, "harness kinds", "in the commit that adds the file"}},
		{"a name of another group", page, []string{"google.yaml"}, []string{"harnesses/google.yaml", `"google"`}},
		{"a kind in capitals", page, []string{"Claude.yaml"}, []string{"harnesses/Claude.yaml", `"Claude"`}},
		{"a kind with a dot", page, []string{"claude.v2.yaml"}, []string{`"claude.v2"`}},
		{"no harness kinds row", build(ceilings, "| providers | `google` |\n", skipped), []string{"claude.yaml"},
			[]string{"no row for", `"harness kinds"`}},
		{"a list that cannot be read", strings.Replace(page, "## Names the core may not name", "## Names", 1), []string{"claude.yaml"},
			[]string{"no ## Names the core may not name section"}},
	} {
		got := harnessKindProblems(tc.page, tc.files)
		if len(tc.want) == 0 {
			if len(got) != 0 {
				t.Errorf("%s: right files are refused: %q", tc.name, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Errorf("%s: %d problems, want one: %q", tc.name, len(got), got)
			continue
		}
		for _, part := range tc.want {
			if !strings.Contains(got[0], part) {
				t.Errorf("%s: the problem %q does not say %q", tc.name, got[0], part)
			}
		}
	}
}

// goFilesOf is made-up Go files as the shared walk holds them: a path and its
// lines.
func goFilesOf(src map[string]string) []repoGoFile {
	var out []repoGoFile
	for _, file := range slices.Sorted(maps.Keys(src)) {
		out = append(out, repoGoFile{path: file, lines: strings.Split(src[file], "\n")})
	}
	return out
}

// coreNamesAllowed is today's hits of the listed names: one row for a file and
// a name, with the number of hits the file may keep of it, by file and then
// name. A count only falls, and a row goes with its last hit, in the commit
// that moves the code out. A row is added only in the commit that puts its
// name on the list, at the hits that name has that day, and a file that moves
// or splits takes its rows to its new paths with no more hits in all. The list
// is in docs/contracts/core.md, under Names the core may not name.
var coreNamesAllowed = []nameAllowance{
	{"internal/dispatch/cleanup.go", "workspace", 1},
	{"internal/dispatch/crew.go", "tab", 1},
	{"internal/dispatch/crewstarts.go", "tab", 1},
	{"internal/dispatch/homesessions.go", "tab", 5},
	{"internal/dispatch/starts.go", "tab", 1},
	{"internal/rpc/client_gen.go", "workspace", 1},
	{"internal/rpc/user.go", "google", 3},
	{"internal/rpc/userclear.go", "google", 2},
	{"internal/rpc/usersecret.go", "google", 6},
	{"internal/runner/crewleft.go", "workspace", 9},
	{"internal/runner/herdr.go", "codex", 3},
	{"internal/runner/herdr.go", "tab", 9},
	{"internal/runner/herdr.go", "workspace", 12},
	{"internal/runner/herdr_brief.go", "claude", 1},
	{"internal/runner/herdr_brief.go", "kimi", 1},
	{"internal/runner/herdr_crew.go", "tab", 2},
	{"internal/runner/herdr_crew.go", "workspace", 2},
	{"internal/runner/herdr_dir.go", "tab", 6},
	{"internal/runner/herdr_dir.go", "workspace", 14},
	{"internal/runner/herdr_labels.go", "tab", 1},
	{"internal/runner/herdr_labels.go", "workspace", 1},
	{"internal/runner/herdr_status.go", "tab", 5},
	{"internal/runner/herdr_status.go", "workspace", 5},
	{"internal/runner/model.go", "workspace", 5},
	{"internal/runner/trust.go", "claude", 9},
	{"internal/runner/usage.go", "workspace", 1},
	{"internal/server/carry_userlogin.go", "claude", 3},
	{"internal/server/hostedmint.go", "google", 3},
	{"internal/store/api.go", "workspace", 1},
	{"internal/store/googlecred.go", "google", 20},
	{"internal/store/harness.go", "workspace", 19},
	{"internal/store/harness_effort.go", "agy", 1},
	{"internal/store/harness_effort.go", "claude", 2},
	{"internal/store/harness_effort.go", "codex", 1},
	{"internal/store/harness_effort.go", "muse", 1},
	{"internal/store/harness_kinds.go", "agy", 3},
	{"internal/store/harness_kinds.go", "claude", 5},
	{"internal/store/harness_kinds.go", "codex", 5},
	{"internal/store/harness_kinds.go", "grok", 4},
	{"internal/store/harness_kinds.go", "kimi", 5},
	{"internal/store/harness_kinds.go", "kiro", 6},
	{"internal/store/harness_kinds.go", "muse", 4},
	{"internal/store/harness_kinds.go", "opencode", 4},
	{"internal/store/harness_kinds.go", "pi", 4},
	{"internal/store/harness_model.go", "claude", 1},
	{"internal/store/harness_model.go", "opencode", 2},
	{"internal/store/loginsteps.go", "google", 1},
	{"internal/store/migrate_settings.go", "claude", 1},
	{"internal/store/routing.go", "claude", 1},
	{"internal/store/routing.go", "workspace", 4},
	{"internal/store/runask_userlogin.go", "claude", 3},
	{"internal/store/secretask.go", "google", 29},
	{"internal/store/secretgive.go", "google", 5},
	{"internal/store/secretreach.go", "google", 3},
	{"internal/store/settings.go", "claude", 1},
	{"internal/store/sourcecalls.go", "google", 3},
	{"internal/store/sourcehosted.go", "google", 2},
	{"internal/store/sourcekeyed.go", "google", 10},
	{"internal/store/userlogin.go", "claude", 3},
	{"internal/store/weight.go", "google", 2},
	{"internal/store/yardrsource.go", "google", 4},
}
