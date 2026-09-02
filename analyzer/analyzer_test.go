package analyzer

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Run("every statement kind reaches the check", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "statements")
	})

	t.Run("nil guards are honoured and only where they hold", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "guards")
	})

	t.Run("writing to a path drops its proof", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "invalidation")
	})

	t.Run("package-qualified globals are not struct fields", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "pkgglobal")
	})

	t.Run("closures see the scope that dominates them, goroutines drop field proofs", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "closures")
	})

	t.Run("composite literal fields are proven where they are set", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "composite")
	})

	t.Run("assert helpers are proven from their own bodies", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "assertproof")
	})

	t.Run("a helper that does not panic proves nothing", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "assertshape")
	})

	t.Run("a checked validator call proves the fields it rejects", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "validator")
	})

	t.Run("excluded path reports nothing", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{ExcludePaths: "src/excluded/"}), "excluded")
	})

	t.Run("nil-safe receiver is transitive through a delegating method", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "nilsafereceiver")
	})

	t.Run("a field wired non-nil by every construction in the package is not reported", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "fieldwiring")
	})

	t.Run("a file generated before the package clause is skipped, others are not", func(t *testing.T) {
		analysistest.Run(t, analysistest.TestData(), New(Config{}), "generated")
	})
}

func TestSplitFragments(t *testing.T) {
	t.Run("empty string yields no fragments", func(t *testing.T) {
		if got := splitFragments(""); got != nil {
			t.Fatalf("splitFragments(\"\") = %v, want nil", got)
		}
	})

	t.Run("blank and whitespace-only entries are dropped", func(t *testing.T) {
		got := splitFragments("internal/dao/, ,  , src/gen/")

		want := []string{"internal/dao/", "src/gen/"}
		if len(got) != len(want) {
			t.Fatalf("splitFragments() = %v, want %v", got, want)
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitFragments()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		got := splitFragments("  internal/dao/  ")
		if len(got) != 1 || got[0] != "internal/dao/" {
			t.Fatalf("splitFragments() = %v, want [internal/dao/]", got)
		}
	})
}
