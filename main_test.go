package main

import (
	"go/ast"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// withFlags points the analyzer at one configuration for the duration of a subtest.
// The flags are analyzer-global, so each subtest restores what it found.
func withFlags(t *testing.T, assertPackage string, excludePaths string) {
	t.Helper()

	previousAssert := assertPackageFlag
	previousExclude := excludePathsFlag

	assertPackageFlag = assertPackage
	excludePathsFlag = excludePaths

	t.Cleanup(func() {
		assertPackageFlag = previousAssert
		excludePathsFlag = previousExclude
	})
}

func TestAnalyzer(t *testing.T) {
	t.Run("every statement kind reaches the check", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "statements")
	})

	t.Run("nil guards are honoured and only where they hold", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "guards")
	})

	t.Run("writing to a path drops its proof", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "invalidation")
	})

	t.Run("package-qualified globals are not struct fields", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "pkgglobal")
	})

	t.Run("closures see the scope that dominates them", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "closures")
	})

	t.Run("composite literal fields are proven where they are set", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "composite")
	})

	t.Run("assert helper from the configured package proves its condition", func(t *testing.T) {
		withFlags(t, "assertpkg/utility", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "assertproof")
	})

	t.Run("assert helper proves nothing when no package is configured", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "assertdisabled")
	})

	t.Run("a checked validator call proves the fields it rejects", func(t *testing.T) {
		withFlags(t, "", "")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "validator")
	})

	t.Run("excluded path reports nothing", func(t *testing.T) {
		withFlags(t, "", "src/excluded/")
		analysistest.Run(t, analysistest.TestData(), Analyzer, "excluded")
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

func TestCanonicalPath(t *testing.T) {
	t.Run("bare identifier is its own path", func(t *testing.T) {
		path, ok := canonicalPath(ast.NewIdent("o"), map[string]string{})
		if !ok || path != "o" {
			t.Fatalf("canonicalPath() = %q, %v, want \"o\", true", path, ok)
		}
	})

	t.Run("selector chain joins with dots", func(t *testing.T) {
		expr := &ast.SelectorExpr{
			X:   &ast.SelectorExpr{X: ast.NewIdent("o"), Sel: ast.NewIdent("p")},
			Sel: ast.NewIdent("q"),
		}

		path, ok := canonicalPath(expr, map[string]string{})
		if !ok || path != "o.p.q" {
			t.Fatalf("canonicalPath() = %q, %v, want \"o.p.q\", true", path, ok)
		}
	})

	t.Run("alias resolves to the path it was read from", func(t *testing.T) {
		expr := &ast.SelectorExpr{X: ast.NewIdent("p"), Sel: ast.NewIdent("q")}

		path, ok := canonicalPath(expr, map[string]string{"p": "o.p"})
		if !ok || path != "o.p.q" {
			t.Fatalf("canonicalPath() = %q, %v, want \"o.p.q\", true", path, ok)
		}
	})

	t.Run("parentheses are transparent", func(t *testing.T) {
		expr := &ast.ParenExpr{X: &ast.SelectorExpr{X: ast.NewIdent("o"), Sel: ast.NewIdent("p")}}

		path, ok := canonicalPath(expr, map[string]string{})
		if !ok || path != "o.p" {
			t.Fatalf("canonicalPath() = %q, %v, want \"o.p\", true", path, ok)
		}
	})

	t.Run("an index expression has no canonical path", func(t *testing.T) {
		expr := &ast.IndexExpr{X: ast.NewIdent("m"), Index: ast.NewIdent("k")}

		if _, ok := canonicalPath(expr, map[string]string{}); ok {
			t.Fatal("canonicalPath() reported ok for an index expression")
		}
	})
}

func TestNilGuards(t *testing.T) {
	notNil := func(path string) *ast.BinaryExpr {
		return &ast.BinaryExpr{X: ast.NewIdent(path), Op: token.NEQ, Y: ast.NewIdent("nil")}
	}

	isNil := func(path string) *ast.BinaryExpr {
		return &ast.BinaryExpr{X: ast.NewIdent(path), Op: token.EQL, Y: ast.NewIdent("nil")}
	}

	t.Run("x != nil proves x when true", func(t *testing.T) {
		whenTrue, whenFalse := nilGuards(notNil("p"), newScope())
		if len(whenTrue) != 1 || whenTrue[0] != "p" || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want [p], nil", whenTrue, whenFalse)
		}
	})

	t.Run("x == nil proves x when false", func(t *testing.T) {
		whenTrue, whenFalse := nilGuards(isNil("p"), newScope())
		if whenTrue != nil || len(whenFalse) != 1 || whenFalse[0] != "p" {
			t.Fatalf("nilGuards() = %v, %v, want nil, [p]", whenTrue, whenFalse)
		}
	})

	t.Run("conjunction proves both operands when true", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: notNil("p"), Op: token.LAND, Y: notNil("q")}

		whenTrue, whenFalse := nilGuards(cond, newScope())
		if len(whenTrue) != 2 || whenTrue[0] != "p" || whenTrue[1] != "q" || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want [p q], nil", whenTrue, whenFalse)
		}
	})

	t.Run("disjunction of nil checks proves both when false", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: isNil("p"), Op: token.LOR, Y: isNil("q")}

		whenTrue, whenFalse := nilGuards(cond, newScope())
		if whenTrue != nil || len(whenFalse) != 2 {
			t.Fatalf("nilGuards() = %v, %v, want nil, [p q]", whenTrue, whenFalse)
		}
	})

	t.Run("negation swaps the two sides", func(t *testing.T) {
		cond := &ast.UnaryExpr{Op: token.NOT, X: isNil("p")}

		whenTrue, whenFalse := nilGuards(cond, newScope())
		if len(whenTrue) != 1 || whenTrue[0] != "p" || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want [p], nil", whenTrue, whenFalse)
		}
	})

	t.Run("a comparison against something other than nil proves nothing", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: ast.NewIdent("p"), Op: token.NEQ, Y: ast.NewIdent("q")}

		whenTrue, whenFalse := nilGuards(cond, newScope())
		if whenTrue != nil || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want nil, nil", whenTrue, whenFalse)
		}
	})

	t.Run("an ordering comparison proves nothing", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: ast.NewIdent("n"), Op: token.GTR, Y: ast.NewIdent("m")}

		whenTrue, whenFalse := nilGuards(cond, newScope())
		if whenTrue != nil || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want nil, nil", whenTrue, whenFalse)
		}
	})
}

func TestScopeInvalidate(t *testing.T) {
	t.Run("the path itself is dropped", func(t *testing.T) {
		sc := scope{proven: map[string]bool{"o.p": true}, alias: map[string]string{}}

		sc.invalidate("o.p")

		if sc.proven["o.p"] {
			t.Fatal("invalidate() kept the proof for the written path")
		}
	})

	t.Run("proofs reached through the path are dropped", func(t *testing.T) {
		sc := scope{proven: map[string]bool{"o.p": true, "o.p.q": true}, alias: map[string]string{}}

		sc.invalidate("o.p")

		if sc.proven["o.p.q"] {
			t.Fatal("invalidate() kept a proof reachable through the written path")
		}
	})

	t.Run("a sibling proof survives", func(t *testing.T) {
		sc := scope{proven: map[string]bool{"o.p": true, "o.q": true}, alias: map[string]string{}}

		sc.invalidate("o.p")

		if !sc.proven["o.q"] {
			t.Fatal("invalidate() dropped an unrelated sibling proof")
		}
	})

	t.Run("aliases of the written path are dropped", func(t *testing.T) {
		sc := scope{
			proven: map[string]bool{},
			alias:  map[string]string{"p": "o.p", "deep": "o.p.q", "other": "o.q"},
		}

		sc.invalidate("o.p")

		if _, ok := sc.alias["p"]; ok {
			t.Fatal("invalidate() kept an alias of the written path")
		}

		if _, ok := sc.alias["deep"]; ok {
			t.Fatal("invalidate() kept an alias reached through the written path")
		}

		if _, ok := sc.alias["other"]; !ok {
			t.Fatal("invalidate() dropped an unrelated alias")
		}
	})
}

func TestBlockExits(t *testing.T) {
	t.Run("nil block does not exit", func(t *testing.T) {
		if blockExits(nil) {
			t.Fatal("blockExits(nil) = true, want false")
		}
	})

	t.Run("empty block does not exit", func(t *testing.T) {
		if blockExits(&ast.BlockStmt{}) {
			t.Fatal("blockExits(empty) = true, want false")
		}
	})

	t.Run("trailing return exits", func(t *testing.T) {
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}}
		if !blockExits(block) {
			t.Fatal("blockExits(return) = false, want true")
		}
	})

	t.Run("trailing continue exits", func(t *testing.T) {
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.CONTINUE}}}
		if !blockExits(block) {
			t.Fatal("blockExits(continue) = false, want true")
		}
	})

	t.Run("trailing panic exits", func(t *testing.T) {
		call := &ast.CallExpr{Fun: ast.NewIdent("panic")}
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call}}}

		if !blockExits(block) {
			t.Fatal("blockExits(panic) = false, want true")
		}
	})

	t.Run("trailing ordinary call does not exit", func(t *testing.T) {
		call := &ast.CallExpr{Fun: ast.NewIdent("cleanup")}
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call}}}

		if blockExits(block) {
			t.Fatal("blockExits(call) = true, want false")
		}
	})

	t.Run("trailing fallthrough does not exit", func(t *testing.T) {
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.FALLTHROUGH}}}
		if blockExits(block) {
			t.Fatal("blockExits(fallthrough) = true, want false")
		}
	})
}

func TestIsDefinitelyNonNil(t *testing.T) {
	t.Run("address of a composite literal is non-nil", func(t *testing.T) {
		expr := &ast.UnaryExpr{Op: token.AND, X: &ast.CompositeLit{}}
		if !isDefinitelyNonNil(expr) {
			t.Fatal("isDefinitelyNonNil(&T{}) = false, want true")
		}
	})

	t.Run("new returns non-nil", func(t *testing.T) {
		expr := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{ast.NewIdent("T")}}
		if !isDefinitelyNonNil(expr) {
			t.Fatal("isDefinitelyNonNil(new(T)) = false, want true")
		}
	})

	t.Run("an arbitrary call says nothing", func(t *testing.T) {
		expr := &ast.CallExpr{Fun: ast.NewIdent("load")}
		if isDefinitelyNonNil(expr) {
			t.Fatal("isDefinitelyNonNil(load()) = true, want false")
		}
	})

	t.Run("a plain identifier says nothing", func(t *testing.T) {
		if isDefinitelyNonNil(ast.NewIdent("other")) {
			t.Fatal("isDefinitelyNonNil(other) = true, want false")
		}
	})
}
