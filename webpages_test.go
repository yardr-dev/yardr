package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The Pages table of docs/web.md is the list of what the web view answers, and
// step 6 of "Adding a page" asks for a row in it, so that a route with no row
// is a page the docs never tell of. This test holds the view to it: it reads
// the GET routes that internal/web/server.go registers on the view's main mux
// and fails each that has no row, but for webRoutesUnlisted. It does not hold
// the table to the routes the other way: a row's path can carry a query
// (/?before=<seq>, /beads?all=1), so that telling a row with no route from a
// row for a variant of one is a matching problem of its own. It reads files
// and starts no process.
func TestWebPagesListEveryRoute(t *testing.T) {
	t.Parallel()
	// The test runs in the repository root, the root package's directory.
	src, err := os.ReadFile(webServerFile)
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile("docs/web.md")
	if err != nil {
		t.Fatal(err)
	}
	problems, read := webPagesProblems(string(src), string(page), webRoutesUnlisted)
	for _, problem := range problems {
		t.Error(problem)
	}
	// Not for want of looking: the view registers a few dozen GET routes.
	if len(problems) == 0 && read < 20 {
		t.Fatalf("%d GET routes read from %s", read, webServerFile)
	}
}

// webServerFile is the file that registers the view's routes: step 4 of
// "Adding a page" puts a page's route in its routes function.
const webServerFile = "internal/web/server.go"

var (
	// webPagesCols is the header of the Pages table.
	webPagesCols = []string{"path", "page", "JSON"}
	// webPathCell is a path cell of the Pages table: one span of backticks.
	webPathCell = regexp.MustCompile("^`([^`]+)`$")
	// muxWildcard is a wildcard of a ServeMux pattern: {id}, {rest...} or {$}.
	muxWildcard = regexp.MustCompile(`\{[^}]*\}`)
)

// webRoute is a GET route of the view's main mux: its path as the Pages table
// writes one, and the line of the source that registers it.
type webRoute struct {
	path string
	line int
}

// webPagesProblems says what is wrong between the routes that a Go source
// registers on the view's main mux and the Pages table of a docs/web.md page,
// one sentence for each fault, each naming its route or its line: a
// registration it cannot read, a row it cannot read, a GET route with no row,
// and an entry of unlisted that is no longer needed (its route has a row now,
// or the source no longer registers it). read says how many GET routes the
// source registers. It is a function of its arguments so that the refusals
// can be tried against made-up sources and pages.
func webPagesProblems(src, page string, unlisted []string) (problems []string, read int) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, webServerFile, src, parser.SkipObjectResolution)
	if err != nil {
		return []string{webServerFile + " cannot be read: " + err.Error()}, 0
	}
	routes, problems := webRoutes(fset, file)
	listed, more := listedWebPaths(page)
	problems = append(problems, more...)
	// A table that cannot be read says so and no more: what a comparison with
	// it found would be a guess.
	if len(more) > 0 {
		return problems, len(routes)
	}

	registered := map[string]bool{}
	skipped := map[string]bool{}
	for _, path := range unlisted {
		skipped[path] = true
	}
	for _, route := range routes {
		// A path that two patterns register is told once.
		if registered[route.path] {
			continue
		}
		registered[route.path] = true
		if !listed[route.path] && !skipped[route.path] {
			problems = append(problems, fmt.Sprintf("%s:%d registers GET %s and the Pages table of docs/web.md has no row for it: add a row to the table",
				webServerFile, route.line, route.path))
		}
	}
	for _, path := range unlisted {
		switch {
		case listed[path]:
			problems = append(problems, path+" is in webRoutesUnlisted and the Pages table of docs/web.md has a row for it now: delete the entry")
		case !registered[path]:
			problems = append(problems, path+" is in webRoutesUnlisted and "+webServerFile+" registers no GET route for it: delete the entry")
		}
	}
	return problems, len(routes)
}

// listedWebPaths reads the paths that the Pages table of a docs/web.md page
// has a row for: the first cell of each row, one span of backticks, cut at a
// ?, so that /beads and /beads?all=1 are rows for the one route. It reads the
// first table of the section only: the lists under it, in a ### subsection,
// are another. A table or a row that cannot be read is a problem, and the
// paths are then not to be trusted.
func listedWebPaths(page string) (listed map[string]bool, problems []string) {
	const where = "docs/web.md: "
	rows, _, problem := sectionFirstTable(page, "Pages", webPagesCols)
	if problem != "" {
		return nil, []string{where + problem}
	}
	listed = map[string]bool{}
	for i, cells := range rows {
		m := webPathCell.FindStringSubmatch(cells[0])
		if m == nil {
			problems = append(problems, where+"row "+strconv.Itoa(i+1)+" of the Pages table has the path cell "+strconv.Quote(cells[0])+
				", which is not one span of backticks")
			continue
		}
		path, _, _ := strings.Cut(m[1], "?")
		listed[path] = true
	}
	return listed, problems
}

// webRoutes reads the GET routes that a parsed source registers on the view's
// main mux: each call of HandleFunc or Handle on a selector named mux. The
// sandbox origin's pagesMux and the local reader's localMux are other muxes,
// and so are other doors. A pattern that names another method is no GET
// route; one that names no method matches every method, GET among them, and
// is one. The pattern is read where it is a string literal, and as
// "GET /"+name in a loop over unbuilt, which gives it once for each name
// there. A registration whose pattern is neither is a problem naming its
// line, so that a route is not missed because of how it was written.
func webRoutes(fset *token.FileSet, file *ast.File) (routes []webRoute, problems []string) {
	line := func(n ast.Node) int { return fset.Position(n.Pos()).Line }
	at := func(n ast.Node) string { return webServerFile + ":" + strconv.Itoa(line(n)) }
	unbuilt, readable := declaredStrings(file, "unbuilt")

	register := func(call *ast.CallExpr, loop string) {
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) == 0 {
			return
		}
		if name := fn.Sel.Name; name != "HandleFunc" && name != "Handle" {
			return
		}
		if mux, ok := fn.X.(*ast.SelectorExpr); !ok || mux.Sel.Name != "mux" {
			return
		}
		patterns, ok := webPatterns(call.Args[0], loop, unbuilt)
		if !ok {
			problems = append(problems, at(call)+` registers a route by a pattern that is neither a string literal nor "GET /"+name in a loop over unbuilt: `+
				"write a literal, so that the route is held to a row in the Pages table of docs/web.md")
			return
		}
		for _, pattern := range patterns {
			path := pattern
			if method, rest, found := strings.Cut(pattern, " "); found {
				if method != "GET" {
					continue
				}
				path = rest
			}
			routes = append(routes, webRoute{webPath(strings.TrimSpace(path)), line(call)})
		}
	}

	// walk visits root for registrations; loop is the variable of the loop
	// over unbuilt that root is the body of, or "".
	var walk func(root ast.Node, loop string)
	walk = func(root ast.Node, loop string) {
		ast.Inspect(root, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.RangeStmt:
				if over, ok := n.X.(*ast.Ident); ok && over.Name == "unbuilt" {
					if !readable {
						problems = append(problems, at(n)+" ranges over unbuilt, which is no var of string literals in the file, so the routes it registers cannot be read")
					}
					name := ""
					if v, ok := n.Value.(*ast.Ident); ok {
						name = v.Name
					}
					walk(n.Body, name)
					return false
				}
			case *ast.CallExpr:
				register(n, loop)
			}
			return true
		})
	}
	walk(file, "")
	return routes, problems
}

// webPatterns is the patterns that the first argument of a registration gives:
// its string where it is a literal, and where it is a literal and the
// variable of the loop over unbuilt that the call stands in (loop names it)
// the literal followed by each name in unbuilt, one pattern for each; ok is
// false for any other argument.
func webPatterns(arg ast.Expr, loop string, unbuilt []string) (patterns []string, ok bool) {
	if s, isLit := literalString(arg); isLit {
		return []string{s}, true
	}
	sum, isSum := arg.(*ast.BinaryExpr)
	if !isSum || sum.Op != token.ADD {
		return nil, false
	}
	prefix, isLit := literalString(sum.X)
	name, isName := sum.Y.(*ast.Ident)
	if !isLit || !isName || name.Name != loop {
		return nil, false
	}
	for _, each := range unbuilt {
		patterns = append(patterns, prefix+each)
	}
	return patterns, true
}

// webPath writes a path of a mux pattern as the Pages table writes one: a
// wildcard {id} is <id>, and {$}, which ends a path there and matches no more
// of it, is nothing, so that GET /{$} and the catch-all GET / are both /.
func webPath(path string) string {
	return muxWildcard.ReplaceAllStringFunc(path, func(wild string) string {
		if wild == "{$}" {
			return ""
		}
		return "<" + wild[1:len(wild)-1] + ">"
	})
}

// declaredStrings is the elements of the package-level var of a file with the
// given name, when it is declared as a list of string literals; ok is false
// when no such var is there, or one that holds anything else.
func declaredStrings(file *ast.File, name string) (list []string, ok bool) {
	for _, decl := range file.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			// The specs of a var declaration are all ValueSpecs.
			value := spec.(*ast.ValueSpec)
			for i, id := range value.Names {
				if id.Name != name || i >= len(value.Values) {
					continue
				}
				lit, isLit := value.Values[i].(*ast.CompositeLit)
				if !isLit {
					return nil, false
				}
				for _, elt := range lit.Elts {
					s, isStr := literalString(elt)
					if !isStr {
						return nil, false
					}
					list = append(list, s)
				}
				return list, true
			}
		}
	}
	return nil, false
}

// literalString is the value of a string literal, and whether the expression
// is one.
func literalString(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// The refusals fire. Each source and page below is right or wrong in one way,
// and the problem webPagesProblems gives for it must name the route or the
// line it is about, and be the only one. They are made up here because the
// real ones have no fault to show.
func TestWebPagesRefuse(t *testing.T) {
	t.Parallel()
	// server is a made-up server.go: the declaration of unbuilt and a routes
	// function that holds the lines given, the first of them on line 6.
	server := func(unbuilt string, lines ...string) string {
		return "package web\n\nvar unbuilt = []string{" + unbuilt + "}\n\nfunc (s *Server) routes() {\n" + strings.Join(lines, "\n") + "\n}\n"
	}
	// pages is a made-up docs/web.md: a Pages table of the rows given, then
	// the lists' table in a ### subsection of it, which is not the table that
	// is read, and a section after it with a table of its own.
	pages := func(rows ...string) string {
		return "# The view\n\n## Pages\n\n| path | page | JSON |\n|---|---|---|\n" + strings.Join(rows, "\n") +
			"\n\n### The lists\n\n| command | route | rows under |\n|---|---|---|\n| `yardr x` | `/x` | `x` |\n\n## After\n\n| 1 | not a row | |\n"
	}
	row := func(path string) string { return "| `" + path + "` | a page | |" }
	const (
		getA    = `s.mux.HandleFunc("GET /a", h)`
		getB    = `s.mux.HandleFunc("GET /b", h)`
		favicon = `s.mux.HandleFunc("GET /favicon.ico", h)`
		// loop is how routes registers the pages that are not built yet.
		loop = `for _, name := range unbuilt { s.mux.HandleFunc("GET /"+name, h) }`
	)
	for _, tc := range []struct {
		name     string
		src      string
		page     string
		unlisted []string
		want     []string // parts of the one problem; none means the source and the page agree
	}{
		{"a route with a row", server("", getA), pages(row("/a")), nil, nil},
		{"a row with a query", server("", getA), pages("| `/a?all=1` | a page | |", row("/a?before=<seq>")), nil, nil},
		{"a wildcard", server("", `s.mux.HandleFunc("GET /a/{id}/b/{n}", h)`), pages(row("/a/<id>/b/<n>")), nil, nil},
		{"the home page two ways", server("", `s.mux.HandleFunc("GET /{$}", h)`, `s.mux.HandleFunc("GET /", h)`), pages(row("/")), nil, nil},
		{"other methods", server("", getA, `s.mux.HandleFunc("POST /b", h)`, `s.mux.HandleFunc("HEAD /c", h)`), pages(row("/a")), nil, nil},
		{"the sandbox origin's mux", server("", getA, `s.pagesMux.HandleFunc("GET /p", h)`), pages(row("/a")), nil, nil},
		{"a route that draws no page", server("", getA, favicon), pages(row("/a")), []string{"/favicon.ico"}, nil},
		{"a row with no route", server("", getA), pages(row("/a"), row("/gone")), nil, nil},
		{"a route in unbuilt with a row", server(`"soon"`, getA, loop), pages(row("/a"), row("/soon")), nil, nil},
		{"no route in unbuilt", server("", getA, loop), pages(row("/a")), nil, nil},

		{"an unlisted route", server("", getA, getB), pages(row("/a")), nil,
			[]string{"internal/web/server.go:7 ", "GET /b", "no row", "add a row"}},
		{"an unlisted route registered twice", server("", `s.mux.HandleFunc("GET /{$}", h)`, `s.mux.HandleFunc("GET /", h)`), pages(row("/a")), nil,
			[]string{"server.go:6 ", "GET /", "add a row"}},
		{"an unlisted route by Handle", server("", getA, `s.mux.Handle("GET /h", hh)`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "GET /h", "add a row"}},
		{"an unlisted route that names no method", server("", getA, `s.mux.HandleFunc("/m", h)`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "GET /m", "add a row"}},
		{"a route in unbuilt with no row", server(`"soon"`, getA, loop), pages(row("/a")), nil,
			[]string{"server.go:7 ", "GET /soon", "add a row"}},
		{"a literal in a loop over unbuilt", server("", getA, `for range unbuilt { s.mux.HandleFunc("GET /b", h) }`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "GET /b", "add a row"}},

		{"a non-literal registration", server("", getA, `s.mux.HandleFunc(pattern, h)`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "neither a string literal", "unbuilt"}},
		{"a sum of literals", server("", getA, `s.mux.HandleFunc("GET /"+"b", h)`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "neither a string literal"}},
		{"a loop over another list", server("", getA, `for _, name := range others { s.mux.HandleFunc("GET /"+name, h) }`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "neither a string literal"}},
		{"a name that is not the loop's", server(`"soon"`, getA, `for _, name := range unbuilt { s.mux.HandleFunc("GET /"+other, h) }`), pages(row("/a")), nil,
			[]string{"server.go:7 ", "neither a string literal"}},
		{"unbuilt with a name that is no literal", server("soon", getA, loop), pages(row("/a")), nil,
			[]string{"server.go:7 ", "ranges over unbuilt", "cannot be read"}},
		{"a source that does not parse", "package web\n\nfunc (s *Server) routes() {\n", pages(row("/a")), nil,
			[]string{webServerFile, "cannot be read"}},

		{"a stale entry whose route has a row", server("", getA, favicon), pages(row("/a"), row("/favicon.ico")), []string{"/favicon.ico"},
			[]string{"/favicon.ico ", "webRoutesUnlisted", "has a row", "delete the entry"}},
		{"a stale entry whose route has gone", server("", getA), pages(row("/a")), []string{"/favicon.ico"},
			[]string{"/favicon.ico ", "webRoutesUnlisted", "registers no GET route", "delete the entry"}},
		{"a stale entry whose route takes no GET", server("", getA, `s.mux.HandleFunc("POST /f", h)`), pages(row("/a")), []string{"/f"},
			[]string{"/f ", "registers no GET route", "delete the entry"}},
		{"a stale entry beside one that holds", server("", getA, favicon), pages(row("/a")), []string{"/gone", "/favicon.ico"},
			[]string{"/gone ", "registers no GET route", "delete the entry"}},

		{"no Pages section", server("", getA), strings.Replace(pages(row("/a")), "## Pages", "## Page", 1), nil,
			[]string{"docs/web.md: ", "no ## Pages section"}},
		{"another header", server("", getA), strings.Replace(pages(row("/a")), "| path | page | JSON |", "| path | page |", 1), nil,
			[]string{"docs/web.md: ", "header", "| path | page | JSON |"}},
		{"a path cell that is no span of backticks", server("", getA), pages("| /a | a page | |"), nil,
			[]string{"docs/web.md: ", "row 1 ", `"/a"`, "span of backticks"}},
		{"a path cell of two spans", server("", getA), pages(row("/a"), "| `/a` `/b` | a page | |"), nil,
			[]string{"row 2 ", "span of backticks"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := webPagesProblems(tc.src, tc.page, tc.unlisted)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Errorf("a right source and page are refused: %q", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("a source and page that are wrong in %q give %d problems, want one: %q", tc.want, len(got), got)
			}
			for _, part := range tc.want {
				if !strings.Contains(got[0], part) {
					t.Errorf("the problem %q does not say %q", got[0], part)
				}
			}
		})
	}

	// The routes read are counted: the GET routes of the main mux, which the
	// guard of the real test holds to a number.
	src := server("", getA, getB, `s.mux.HandleFunc("POST /c", h)`, `s.pagesMux.HandleFunc("GET /d", h)`, loop)
	if _, read := webPagesProblems(src, pages(row("/a"), row("/b")), nil); read != 2 {
		t.Errorf("%d routes read of a source with two GET routes on the main mux", read)
	}
}

// webRoutesUnlisted is the GET routes of the view's main mux that the Pages
// table has no row for, because they draw no page and answer no JSON, in the
// form the test reads a route in (webPath). It only shrinks: a route that
// answers with something has its row, and an entry whose route has one, or
// that the view no longer registers, fails the test until it is deleted. An
// entry says why it has no row.
var webRoutesUnlisted = []string{
	// A browser asks for it unbidden and the view has no icon: the answer is
	// empty (204), which keeps the browser's console clear of a 404.
	"/favicon.ico",
}
