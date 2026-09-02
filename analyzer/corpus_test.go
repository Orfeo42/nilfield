package analyzer

import (
	"go/ast"
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
// it.
//
// The suite is therefore red until the analyzer covers the whole corpus. That is
// the point: the failing subtests are the work list, and each one goes green on
// its own as the case is implemented.
const (
	hazardMarker  = "//nilhazard:"
	safeMarker    = "//nilsafe:"
	fixtureMarker = "//nilfixture:"
)

// corpusCase is one marked function: the hazard sites it holds, and the
// diagnostics the analyzer raised inside it.
type corpusCase struct {
	id       string
	hazard   bool
	sites    int
	reported int
}

func TestCorpus(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), New(Config{}), "nilcases")
	if len(results) != 1 {
		t.Fatalf("analysistest.Run returned %d results, want 1", len(results))
	}

	cases, unmarked := collectCorpusCases(t, results[0])

	if len(unmarked) > 0 {
		t.Fatalf("functions carrying no nilhazard/nilsafe/nilfixture marker: %v", unmarked)
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
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
		})
	}

	t.Run("coverage", func(t *testing.T) {
		var hazards, covered, sites, reported int

		for _, c := range cases {
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

		if covered < hazards {
			t.Fatalf("%d hazard cases are not covered", hazards-covered)
		}
	})
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

			id, kind := parseMarker(fn)

			if kind == markerNone {
				unmarked = append(unmarked, fn.Name.Name)

				continue
			}

			if kind == markerFixture {
				continue
			}

			if seen[id] {
				t.Fatalf("duplicate case id %q", id)
			}

			seen[id] = true

			cases = append(cases, corpusCase{
				id:       id,
				hazard:   kind == markerHazard,
				sites:    markerSites(t, fn),
				reported: countDiagnosticsIn(fn, result.Diagnostics),
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
)

// parseMarker reads the marker off a function's doc comment, returning the case
// id for hazard and safe cases.
func parseMarker(fn *ast.FuncDecl) (string, markerKind) {
	if fn.Doc == nil {
		return "", markerNone
	}

	for _, comment := range fn.Doc.List {
		text := comment.Text

		if strings.HasPrefix(text, fixtureMarker) {
			return "", markerFixture
		}

		if rest, found := strings.CutPrefix(text, safeMarker); found {
			return strings.TrimSpace(rest), markerSafe
		}

		if rest, found := strings.CutPrefix(text, hazardMarker); found {
			id, _, _ := strings.Cut(strings.TrimSpace(rest), " ")

			return id, markerHazard
		}
	}

	return "", markerNone
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
