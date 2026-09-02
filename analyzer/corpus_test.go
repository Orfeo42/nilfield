package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// The corpus in testdata/src/nilcases enumerates every way a nil value reaches a
// dereference in Go. Every function there carries a marker, and TestCorpus turns
// each one into a subtest: a hazard case fails until the analyzer reports every
// site it holds, a safe case fails as soon as the analyzer reports anything in
// it, and an out-of-scope case — a real hazard that falls outside what this
// analyzer covers by design — fails as soon as the analyzer reports anything in
// it too, which is what keeps a re-widened rule from passing silently.
//
// The suite is therefore red until the analyzer covers the whole corpus. That is
// the point: the failing subtests are the work list, and each one goes green on
// its own as the case is implemented.
const (
	hazardMarker     = "//nilhazard:"
	safeMarker       = "//nilsafe:"
	fixtureMarker    = "//nilfixture:"
	outOfScopeMarker = "//niloutofscope:"
	whyInfix         = " why="
)

// corpusCase is one marked function: the hazard sites it holds, and the
// diagnostics the analyzer raised inside it. An out-of-scope case carries why
// instead of a hazard site count: it is a real hazard outside what this
// analyzer covers by design.
type corpusCase struct {
	id         string
	hazard     bool
	outOfScope bool
	why        string
	sites      int
	reported   int
}

func TestCorpus(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), New(Config{}), "nilcases")
	if len(results) != 1 {
		t.Fatalf("analysistest.Run returned %d results, want 1", len(results))
	}

	cases, unmarked := collectCorpusCases(t, results[0])

	if len(unmarked) > 0 {
		t.Fatalf("functions carrying no nilhazard/nilsafe/nilfixture/niloutofscope marker: %v", unmarked)
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			checkCorpusCase(t, c)
		})
	}

	t.Run("coverage", func(t *testing.T) {
		logCorpusCoverage(t, cases)
	})
}

// checkCorpusCase asserts the one property each marker kind promises: a safe or
// out-of-scope case must draw zero diagnostics, a hazard case must draw exactly
// one per site it holds.
func checkCorpusCase(t *testing.T, c corpusCase) {
	t.Helper()

	if c.outOfScope {
		if c.reported > 0 {
			t.Fatalf("out of scope (%s): %d diagnostics reported for a hazard this analyzer does not cover", c.why, c.reported)
		}

		return
	}

	if !c.hazard {
		if c.reported > 0 {
			t.Fatalf("false positive: %d diagnostics in a case that is correct", c.reported)
		}

		return
	}

	if c.reported < c.sites {
		t.Fatalf("not covered: %d of %d hazard sites reported", c.reported, c.sites)
	}

	if c.reported > c.sites {
		t.Fatalf("over-reported: %d diagnostics for %d hazard sites", c.reported, c.sites)
	}
}

// logCorpusCoverage logs the in-scope hazard coverage and the out-of-scope-case
// breakdown by why token, then fails if any in-scope hazard case is not fully
// covered.
func logCorpusCoverage(t *testing.T, cases []corpusCase) {
	t.Helper()

	var hazards, covered, sites, reported int

	byWhy := map[string]int{}

	for _, c := range cases {
		if c.outOfScope {
			byWhy[c.why]++

			continue
		}

		if !c.hazard {
			continue
		}

		hazards++
		sites += c.sites
		reported += c.reported

		if c.reported >= c.sites {
			covered++
		}
	}

	t.Logf("nilfield fully covers %d of %d hazard cases (%d of %d hazard sites)",
		covered, hazards, reported, sites)

	if len(byWhy) > 0 {
		t.Logf("out of scope: %s", formatOutOfScopeBreakdown(byWhy))
	}

	if covered < hazards {
		t.Fatalf("%d hazard cases are not covered", hazards-covered)
	}
}

// formatOutOfScopeBreakdown renders the out-of-scope-case counts grouped by why
// token, the most common reason first.
func formatOutOfScopeBreakdown(byWhy map[string]int) string {
	whys := make([]string, 0, len(byWhy))
	for why := range byWhy {
		whys = append(whys, why)
	}

	slices.SortFunc(whys, func(a, b string) int {
		if byWhy[a] != byWhy[b] {
			return byWhy[b] - byWhy[a]
		}

		return strings.Compare(a, b)
	})

	parts := make([]string, len(whys))
	for i, why := range whys {
		parts[i] = strconv.Itoa(byWhy[why]) + " " + why
	}

	return strings.Join(parts, ", ")
}

// collectCorpusCases pairs every marked function in the corpus with the number
// of diagnostics the analyzer raised inside its body, and names the functions
// carrying no marker at all so a case cannot be added without being inventoried.
func collectCorpusCases(t *testing.T, result *analysistest.Result) ([]corpusCase, []string) {
	t.Helper()

	var (
		cases    []corpusCase
		unmarked []string
	)

	seen := map[string]bool{}

	for _, file := range result.Pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			m, kind := parseMarker(fn)

			if kind == markerNone {
				unmarked = append(unmarked, fn.Name.Name)

				continue
			}

			if kind == markerFixture {
				continue
			}

			if seen[m.id] {
				t.Fatalf("duplicate case id %q", m.id)
			}

			seen[m.id] = true

			cases = append(cases, corpusCase{
				id:         m.id,
				hazard:     kind == markerHazard,
				outOfScope: kind == markerOutOfScope,
				why:        m.why,
				sites:      markerSites(t, fn),
				reported:   countDiagnosticsIn(fn, result.Diagnostics),
			})
		}
	}

	if len(cases) == 0 {
		t.Fatal("no marked cases found in the corpus")
	}

	slices.Sort(unmarked)

	return cases, unmarked
}

type markerKind int

const (
	markerNone markerKind = iota
	markerHazard
	markerSafe
	markerFixture
	markerOutOfScope
)

// marker is the parsed content of one corpus marker: the case id every kind
// carries, and the why token an out-of-scope marker additionally carries.
type marker struct {
	id  string
	why string
}

// parseMarker reads the marker off a function's doc comment, returning the
// parsed marker and its kind.
func parseMarker(fn *ast.FuncDecl) (marker, markerKind) {
	if fn.Doc == nil {
		return marker{}, markerNone
	}

	for _, comment := range fn.Doc.List {
		text := comment.Text

		if strings.HasPrefix(text, fixtureMarker) {
			return marker{}, markerFixture
		}

		if rest, found := strings.CutPrefix(text, safeMarker); found {
			return marker{id: strings.TrimSpace(rest)}, markerSafe
		}

		if rest, found := strings.CutPrefix(text, outOfScopeMarker); found {
			id, why := parseOutOfScopeMarker(rest)

			return marker{id: id, why: why}, markerOutOfScope
		}

		if rest, found := strings.CutPrefix(text, hazardMarker); found {
			id, _, _ := strings.Cut(strings.TrimSpace(rest), " ")

			return marker{id: id}, markerHazard
		}
	}

	return marker{}, markerNone
}

// parseOutOfScopeMarker splits a niloutofscope marker's remainder
// ("<id> why=<token>") into the case id and the why token it names.
func parseOutOfScopeMarker(rest string) (string, string) {
	trimmed := strings.TrimSpace(rest)

	id, why, found := strings.Cut(trimmed, whyInfix)
	if !found {
		return strings.TrimSpace(trimmed), ""
	}

	return strings.TrimSpace(id), strings.TrimSpace(why)
}

// markerSites reads the sites= count off a hazard marker.
func markerSites(t *testing.T, fn *ast.FuncDecl) int {
	t.Helper()

	for _, comment := range fn.Doc.List {
		rest, found := strings.CutPrefix(comment.Text, hazardMarker)
		if !found {
			continue
		}

		_, count, found := strings.Cut(strings.TrimSpace(rest), " sites=")
		if !found {
			t.Fatalf("hazard marker on %s carries no sites= count", fn.Name.Name)
		}

		sites, err := strconv.Atoi(count)
		if err != nil {
			t.Fatalf("hazard marker on %s: %v", fn.Name.Name, err)
		}

		return sites
	}

	return 0
}

func countDiagnosticsIn(fn *ast.FuncDecl, diagnostics []analysis.Diagnostic) int {
	count := 0

	for _, d := range diagnostics {
		if d.Pos >= fn.Pos() && d.Pos <= fn.End() {
			count++
		}
	}

	return count
}

// parseFuncDeclForMarker parses src, which must declare a single function, and
// returns its *ast.FuncDecl with doc comments attached, for feeding to
// parseMarker.
func parseFuncDeclForMarker(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "snippet.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	for _, decl := range file.Decls {
		if fd, isFunc := decl.(*ast.FuncDecl); isFunc {
			return fd
		}
	}

	t.Fatal("snippet declares no function")

	return nil
}

func TestParseMarker(t *testing.T) {
	t.Run("hazard marker carries the case id", func(t *testing.T) {
		fd := parseFuncDeclForMarker(t, "package p\n\n//nilhazard:some-case sites=2\nfunc f() {}\n")

		m, kind := parseMarker(fd)

		if kind != markerHazard {
			t.Fatalf("parseMarker() kind = %v, want markerHazard", kind)
		}

		if m.id != "some-case" {
			t.Fatalf("parseMarker() id = %q, want %q", m.id, "some-case")
		}
	})

	t.Run("safe marker carries the case id", func(t *testing.T) {
		fd := parseFuncDeclForMarker(t, "package p\n\n//nilsafe:some-case\nfunc f() {}\n")

		m, kind := parseMarker(fd)

		if kind != markerSafe {
			t.Fatalf("parseMarker() kind = %v, want markerSafe", kind)
		}

		if m.id != "some-case" {
			t.Fatalf("parseMarker() id = %q, want %q", m.id, "some-case")
		}
	})

	t.Run("fixture marker carries nothing", func(t *testing.T) {
		fd := parseFuncDeclForMarker(t, "package p\n\n//nilfixture:shared\nfunc f() {}\n")

		_, kind := parseMarker(fd)

		if kind != markerFixture {
			t.Fatalf("parseMarker() kind = %v, want markerFixture", kind)
		}
	})

	t.Run("out-of-scope marker carries the case id and the why token", func(t *testing.T) {
		fd := parseFuncDeclForMarker(t, "package p\n\n//niloutofscope:some-case why=not-a-dereference\nfunc f() {}\n")

		m, kind := parseMarker(fd)

		if kind != markerOutOfScope {
			t.Fatalf("parseMarker() kind = %v, want markerOutOfScope", kind)
		}

		if m.id != "some-case" {
			t.Fatalf("parseMarker() id = %q, want %q", m.id, "some-case")
		}

		if m.why != "not-a-dereference" {
			t.Fatalf("parseMarker() why = %q, want %q", m.why, "not-a-dereference")
		}
	})

	t.Run("no doc comment yields markerNone", func(t *testing.T) {
		fd := parseFuncDeclForMarker(t, "package p\n\nfunc f() {}\n")

		_, kind := parseMarker(fd)

		if kind != markerNone {
			t.Fatalf("parseMarker() kind = %v, want markerNone", kind)
		}
	})
}

func TestParseOutOfScopeMarker(t *testing.T) {
	t.Run("id and why are split on the why= infix", func(t *testing.T) {
		id, why := parseOutOfScopeMarker(" some-case why=construction-site")

		if id != "some-case" {
			t.Fatalf("parseOutOfScopeMarker() id = %q, want %q", id, "some-case")
		}

		if why != "construction-site" {
			t.Fatalf("parseOutOfScopeMarker() why = %q, want %q", why, "construction-site")
		}
	})

	t.Run("missing why infix yields an empty why", func(t *testing.T) {
		id, why := parseOutOfScopeMarker(" some-case")

		if id != "some-case" {
			t.Fatalf("parseOutOfScopeMarker() id = %q, want %q", id, "some-case")
		}

		if why != "" {
			t.Fatalf("parseOutOfScopeMarker() why = %q, want empty", why)
		}
	})
}
