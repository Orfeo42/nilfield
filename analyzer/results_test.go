package analyzer

import (
	"go/ast"
	"go/types"
	"testing"
)

// collectSnippetNilResultIndexes type-checks src, which must declare a single
// function whose body's only statement is the return under test, and runs
// collectNilResultIndexes on it.
func collectSnippetNilResultIndexes(t *testing.T, src string) map[int]bool {
	t.Helper()

	fd, info := parseAndCheck(t, src)
	c := newSnippetChecker(info)

	fn, isFunc := info.Defs[fd.Name].(*types.Func)
	if !isFunc {
		t.Fatal("snippet function has no *types.Func object")
	}

	results := fn.Signature().Results()
	errIdx := lastErrorResultIndex(results)

	ret, isReturn := fd.Body.List[0].(*ast.ReturnStmt)
	if !isReturn {
		t.Fatal("snippet function's first statement is not a return")
	}

	out := map[int]bool{}
	c.collectNilResultIndexes(ret, results, errIdx, out)

	return out
}

func TestCollectNilResultIndexes(t *testing.T) {
	t.Run("single pointer result returning nil", func(t *testing.T) {
		out := collectSnippetNilResultIndexes(t, `package p

type T struct{}

func f() *T { return nil }
`)

		if len(out) != 1 || !out[0] {
			t.Fatalf("collectNilResultIndexes() = %v, want {0: true}", out)
		}
	})

	t.Run("(*T, error) returning nil, nil", func(t *testing.T) {
		out := collectSnippetNilResultIndexes(t, `package p

type T struct{}

func f() (*T, error) { return nil, nil }
`)

		if len(out) != 1 || !out[0] {
			t.Fatalf("collectNilResultIndexes() = %v, want {0: true}", out)
		}
	})

	t.Run("(*T, error) returning nil, err", func(t *testing.T) {
		out := collectSnippetNilResultIndexes(t, `package p

type T struct{}

func f(err error) (*T, error) { return nil, err }
`)

		if len(out) != 0 {
			t.Fatalf("collectNilResultIndexes() = %v, want none", out)
		}
	})

	t.Run("[]int nil", func(t *testing.T) {
		out := collectSnippetNilResultIndexes(t, `package p

func f() []int { return nil }
`)

		if len(out) != 0 {
			t.Fatalf("collectNilResultIndexes() = %v, want none", out)
		}
	})

	t.Run("error nil", func(t *testing.T) {
		out := collectSnippetNilResultIndexes(t, `package p

func f() error { return nil }
`)

		if len(out) != 0 {
			t.Fatalf("collectNilResultIndexes() = %v, want none", out)
		}
	})
}

// computeSnippetNilResultsFact type-checks src, which must declare a single
// function, and runs computeNilResultsFact on it.
func computeSnippetNilResultsFact(t *testing.T, src string) (*nilResults, bool) {
	t.Helper()

	fd, info := parseAndCheck(t, src)
	c := newSnippetChecker(info)

	fn, isFunc := info.Defs[fd.Name].(*types.Func)
	if !isFunc {
		t.Fatal("snippet function has no *types.Func object")
	}

	return c.computeNilResultsFact(fd, fn)
}

func TestComputeNilResultsFact(t *testing.T) {
	t.Run("nil-yielding return dominated by a nil-parameter guard is conditional", func(t *testing.T) {
		fact, ok := computeSnippetNilResultsFact(t, `package p

type T struct{}

func f(e *T) *T {
	if e == nil {
		return nil
	}

	return e
}
`)
		if !ok {
			t.Fatal("computeNilResultsFact() ok = false, want true")
		}

		if len(fact.Results) != 0 {
			t.Fatalf("fact.Results = %v, want none", fact.Results)
		}

		if fact.NilWhenParamNil[0] != 0 {
			t.Fatalf("fact.NilWhenParamNil = %v, want {0: 0}", fact.NilWhenParamNil)
		}
	})

	t.Run("nil-yielding return unrelated to any parameter stays unconditional", func(t *testing.T) {
		fact, ok := computeSnippetNilResultsFact(t, `package p

type T struct{}

func f(ok bool) *T {
	if !ok {
		return nil
	}

	return &T{}
}
`)
		if !ok {
			t.Fatal("computeNilResultsFact() ok = false, want true")
		}

		if len(fact.Results) != 1 || fact.Results[0] != 0 {
			t.Fatalf("fact.Results = %v, want [0]", fact.Results)
		}

		if len(fact.NilWhenParamNil) != 0 {
			t.Fatalf("fact.NilWhenParamNil = %v, want none", fact.NilWhenParamNil)
		}
	})

	t.Run("one of two nil-yielding returns lacks a parameter guard, stays unconditional", func(t *testing.T) {
		fact, ok := computeSnippetNilResultsFact(t, `package p

type T struct{}

func f(e *T, ok bool) *T {
	if e == nil {
		return nil
	}

	if !ok {
		return nil
	}

	return e
}
`)
		if !ok {
			t.Fatal("computeNilResultsFact() ok = false, want true")
		}

		if len(fact.Results) != 1 || fact.Results[0] != 0 {
			t.Fatalf("fact.Results = %v, want [0]", fact.Results)
		}

		if len(fact.NilWhenParamNil) != 0 {
			t.Fatalf("fact.NilWhenParamNil = %v, want none", fact.NilWhenParamNil)
		}
	})

	t.Run("guard on a different parameter than the caller expects", func(t *testing.T) {
		fact, ok := computeSnippetNilResultsFact(t, `package p

type T struct{}

func f(e *T, other *T) *T {
	if other == nil {
		return nil
	}

	return e
}
`)
		if !ok {
			t.Fatal("computeNilResultsFact() ok = false, want true")
		}

		if len(fact.Results) != 0 {
			t.Fatalf("fact.Results = %v, want none", fact.Results)
		}

		if fact.NilWhenParamNil[0] != 1 {
			t.Fatalf("fact.NilWhenParamNil = %v, want {0: 1}", fact.NilWhenParamNil)
		}
	})
}
