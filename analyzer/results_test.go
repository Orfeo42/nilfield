package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
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

// parseSnippetFuncs parses src as a standalone Go file and type-checks it
// with no importer, returning every top-level function declaration
// alongside the real type info backing them - the multi-function
// counterpart of parseAndCheck, needed to exercise transitivity between two
// declared functions in one snippet.
func parseSnippetFuncs(t *testing.T, src string) ([]*ast.FuncDecl, *types.Info) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "snippet.go", src, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
	}

	conf := types.Config{}
	if _, err := conf.Check("snippet", fset, []*ast.File{file}, info); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	var decls []*ast.FuncDecl

	for _, decl := range file.Decls {
		if fd, isFunc := decl.(*ast.FuncDecl); isFunc {
			decls = append(decls, fd)
		}
	}

	if len(decls) == 0 {
		t.Fatal("snippet declares no function")
	}

	return decls, info
}

// funcDeclNamed finds the declaration named name among decls.
func funcDeclNamed(t *testing.T, decls []*ast.FuncDecl, name string) *ast.FuncDecl {
	t.Helper()

	for _, fd := range decls {
		if fd.Name.Name == name {
			return fd
		}
	}

	t.Fatalf("snippet declares no function named %q", name)

	return nil
}

// newFactPass builds an *analysis.Pass whose ExportObjectFact and
// ImportObjectFact are backed by a simple in-memory map, which is what
// proving transitivity through a callee's own nonNilResults fact needs from
// a snippet unit test.
func newFactPass(info *types.Info) *analysis.Pass {
	store := map[types.Object]*nonNilResults{}

	pass := &analysis.Pass{TypesInfo: info}

	pass.ExportObjectFact = func(obj types.Object, fact analysis.Fact) {
		nn, isNonNilResults := fact.(*nonNilResults)
		if !isNonNilResults {
			return
		}

		store[obj] = nn
	}

	pass.ImportObjectFact = func(obj types.Object, fact analysis.Fact) bool {
		target, isNonNilResults := fact.(*nonNilResults)
		if !isNonNilResults {
			return false
		}

		source, found := store[obj]
		if !found {
			return false
		}

		*target = *source

		return true
	}

	return pass
}

func TestComputeNonNilResultsFact(t *testing.T) {
	t.Run("returning only an address qualifies", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type T struct{}

func f() *T {
	return &T{}
}
`)

		fn, isFunc := info.Defs[fd.Name].(*types.Func)
		if !isFunc {
			t.Fatal("snippet function has no *types.Func object")
		}

		c := newSnippetChecker(info)

		fact, ok := c.computeNonNilResultsFact(fd, fn)
		if !ok {
			t.Fatal("computeNonNilResultsFact() ok = false, want true")
		}

		if len(fact.Results) != 1 || fact.Results[0] != 0 {
			t.Fatalf("fact.Results = %v, want [0]", fact.Results)
		}
	})

	t.Run("a nil-yielding return disqualifies the function", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type T struct{}

func f(ok bool) *T {
	if ok {
		return &T{}
	}

	return nil
}
`)

		fn, isFunc := info.Defs[fd.Name].(*types.Func)
		if !isFunc {
			t.Fatal("snippet function has no *types.Func object")
		}

		c := newSnippetChecker(info)

		_, ok := c.computeNonNilResultsFact(fd, fn)
		if ok {
			t.Fatal("computeNonNilResultsFact() ok = true, want false for a function with a nil-yielding return")
		}
	})

	t.Run("a named result disqualifies the function", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type T struct{}

func f() (result *T) {
	return &T{}
}
`)

		fn, isFunc := info.Defs[fd.Name].(*types.Func)
		if !isFunc {
			t.Fatal("snippet function has no *types.Func object")
		}

		c := newSnippetChecker(info)

		_, ok := c.computeNonNilResultsFact(fd, fn)
		if ok {
			t.Fatal("computeNonNilResultsFact() ok = true, want false for a function with a named result")
		}
	})

	t.Run("transitive through a proven callee's own fact", func(t *testing.T) {
		decls, info := parseSnippetFuncs(t, `package p

type T struct{}

func getter() *T {
	return &T{}
}

func wrapper() *T {
	return getter()
}
`)

		getterDecl := funcDeclNamed(t, decls, "getter")
		wrapperDecl := funcDeclNamed(t, decls, "wrapper")

		getterFn, isFunc := info.Defs[getterDecl.Name].(*types.Func)
		if !isFunc {
			t.Fatal("getter has no *types.Func object")
		}

		wrapperFn, isFunc := info.Defs[wrapperDecl.Name].(*types.Func)
		if !isFunc {
			t.Fatal("wrapper has no *types.Func object")
		}

		pass := newFactPass(info)
		c := &checker{pass: pass, resolve: info.ObjectOf}

		getterFact, ok := c.computeNonNilResultsFact(getterDecl, getterFn)
		if !ok {
			t.Fatal("computeNonNilResultsFact(getter) ok = false, want true")
		}

		pass.ExportObjectFact(getterFn, getterFact)

		wrapperFact, ok := c.computeNonNilResultsFact(wrapperDecl, wrapperFn)
		if !ok {
			t.Fatal("computeNonNilResultsFact(wrapper) ok = false, want true")
		}

		if len(wrapperFact.Results) != 1 || wrapperFact.Results[0] != 0 {
			t.Fatalf("wrapperFact.Results = %v, want [0]", wrapperFact.Results)
		}
	})
}
