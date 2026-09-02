package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"
)

// universeResolver stands in for pass.TypesInfo.ObjectOf in tests that build
// synthetic ASTs with no real type-checking pass behind them: it resolves an
// identifier to the predeclared universe object of the same name, if any.
func universeResolver(id *ast.Ident) types.Object {
	return types.Universe.Lookup(id.Name)
}

func TestNilGuards(t *testing.T) {
	c := &checker{resolve: universeResolver}

	notNil := func(path string) *ast.BinaryExpr {
		return &ast.BinaryExpr{X: ast.NewIdent(path), Op: token.NEQ, Y: ast.NewIdent("nil")}
	}

	isNil := func(path string) *ast.BinaryExpr {
		return &ast.BinaryExpr{X: ast.NewIdent(path), Op: token.EQL, Y: ast.NewIdent("nil")}
	}

	t.Run("x != nil proves x when true", func(t *testing.T) {
		whenTrue, whenFalse := c.nilGuards(notNil("p"), newScope())
		if len(whenTrue) != 1 || whenTrue[0] != "p" || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want [p], nil", whenTrue, whenFalse)
		}
	})

	t.Run("x == nil proves x when false", func(t *testing.T) {
		whenTrue, whenFalse := c.nilGuards(isNil("p"), newScope())
		if whenTrue != nil || len(whenFalse) != 1 || whenFalse[0] != "p" {
			t.Fatalf("nilGuards() = %v, %v, want nil, [p]", whenTrue, whenFalse)
		}
	})

	t.Run("conjunction proves both operands when true", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: notNil("p"), Op: token.LAND, Y: notNil("q")}

		whenTrue, whenFalse := c.nilGuards(cond, newScope())
		if len(whenTrue) != 2 || whenTrue[0] != "p" || whenTrue[1] != "q" || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want [p q], nil", whenTrue, whenFalse)
		}
	})

	t.Run("disjunction of nil checks proves both when false", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: isNil("p"), Op: token.LOR, Y: isNil("q")}

		whenTrue, whenFalse := c.nilGuards(cond, newScope())
		if whenTrue != nil || len(whenFalse) != 2 {
			t.Fatalf("nilGuards() = %v, %v, want nil, [p q]", whenTrue, whenFalse)
		}
	})

	t.Run("negation swaps the two sides", func(t *testing.T) {
		cond := &ast.UnaryExpr{Op: token.NOT, X: isNil("p")}

		whenTrue, whenFalse := c.nilGuards(cond, newScope())
		if len(whenTrue) != 1 || whenTrue[0] != "p" || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want [p], nil", whenTrue, whenFalse)
		}
	})

	t.Run("a comparison against something other than nil proves nothing", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: ast.NewIdent("p"), Op: token.NEQ, Y: ast.NewIdent("q")}

		whenTrue, whenFalse := c.nilGuards(cond, newScope())
		if whenTrue != nil || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want nil, nil", whenTrue, whenFalse)
		}
	})

	t.Run("an ordering comparison proves nothing", func(t *testing.T) {
		cond := &ast.BinaryExpr{X: ast.NewIdent("n"), Op: token.GTR, Y: ast.NewIdent("m")}

		whenTrue, whenFalse := c.nilGuards(cond, newScope())
		if whenTrue != nil || whenFalse != nil {
			t.Fatalf("nilGuards() = %v, %v, want nil, nil", whenTrue, whenFalse)
		}
	})
}

func TestClauseGuards(t *testing.T) {
	c := &checker{resolve: universeResolver}

	notNil := func(path string) *ast.BinaryExpr {
		return &ast.BinaryExpr{X: ast.NewIdent(path), Op: token.NEQ, Y: ast.NewIdent("nil")}
	}

	isNil := func(path string) *ast.BinaryExpr {
		return &ast.BinaryExpr{X: ast.NewIdent(path), Op: token.EQL, Y: ast.NewIdent("nil")}
	}

	t.Run("tagless x != nil proves x when true", func(t *testing.T) {
		whenTrue, whenFalse := c.clauseGuards(nil, notNil("x"), newScope())
		if len(whenTrue) != 1 || whenTrue[0] != "x" || whenFalse != nil {
			t.Fatalf("clauseGuards() = %v, %v, want [x], nil", whenTrue, whenFalse)
		}
	})

	t.Run("tagless x == nil proves x when false", func(t *testing.T) {
		whenTrue, whenFalse := c.clauseGuards(nil, isNil("x"), newScope())
		if whenTrue != nil || len(whenFalse) != 1 || whenFalse[0] != "x" {
			t.Fatalf("clauseGuards() = %v, %v, want nil, [x]", whenTrue, whenFalse)
		}
	})

	t.Run("tagged nil case proves the tag when the clause does not match", func(t *testing.T) {
		whenTrue, whenFalse := c.clauseGuards(ast.NewIdent("p"), ast.NewIdent("nil"), newScope())
		if whenTrue != nil || len(whenFalse) != 1 || whenFalse[0] != "p" {
			t.Fatalf("clauseGuards() = %v, %v, want nil, [p]", whenTrue, whenFalse)
		}
	})

	t.Run("tagged non-nil case proves nothing", func(t *testing.T) {
		whenTrue, whenFalse := c.clauseGuards(ast.NewIdent("p"), ast.NewIdent("other"), newScope())
		if whenTrue != nil || whenFalse != nil {
			t.Fatalf("clauseGuards() = %v, %v, want nil, nil", whenTrue, whenFalse)
		}
	})
}

func TestBlockExits(t *testing.T) {
	c := &checker{resolve: universeResolver}

	t.Run("nil block does not exit", func(t *testing.T) {
		if c.blockExits(nil) {
			t.Fatal("blockExits(nil) = true, want false")
		}
	})

	t.Run("empty block does not exit", func(t *testing.T) {
		if c.blockExits(&ast.BlockStmt{}) {
			t.Fatal("blockExits(empty) = true, want false")
		}
	})

	t.Run("trailing return exits", func(t *testing.T) {
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}}
		if !c.blockExits(block) {
			t.Fatal("blockExits(return) = false, want true")
		}
	})

	t.Run("trailing continue exits", func(t *testing.T) {
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.CONTINUE}}}
		if !c.blockExits(block) {
			t.Fatal("blockExits(continue) = false, want true")
		}
	})

	t.Run("trailing panic exits", func(t *testing.T) {
		call := &ast.CallExpr{Fun: ast.NewIdent("panic")}
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call}}}

		if !c.blockExits(block) {
			t.Fatal("blockExits(panic) = false, want true")
		}
	})

	t.Run("trailing ordinary call does not exit", func(t *testing.T) {
		call := &ast.CallExpr{Fun: ast.NewIdent("cleanup")}
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call}}}

		if c.blockExits(block) {
			t.Fatal("blockExits(call) = true, want false")
		}
	})

	t.Run("trailing fallthrough does not exit", func(t *testing.T) {
		block := &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.FALLTHROUGH}}}
		if c.blockExits(block) {
			t.Fatal("blockExits(fallthrough) = true, want false")
		}
	})
}

func TestIsDefinitelyNonNil(t *testing.T) {
	c := &checker{resolve: universeResolver}

	t.Run("address of a composite literal is non-nil", func(t *testing.T) {
		expr := &ast.UnaryExpr{Op: token.AND, X: &ast.CompositeLit{}}
		if !c.isDefinitelyNonNil(expr) {
			t.Fatal("isDefinitelyNonNil(&T{}) = false, want true")
		}
	})

	t.Run("new returns non-nil", func(t *testing.T) {
		expr := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{ast.NewIdent("T")}}
		if !c.isDefinitelyNonNil(expr) {
			t.Fatal("isDefinitelyNonNil(new(T)) = false, want true")
		}
	})

	t.Run("an arbitrary call says nothing", func(t *testing.T) {
		expr := &ast.CallExpr{Fun: ast.NewIdent("load")}
		if c.isDefinitelyNonNil(expr) {
			t.Fatal("isDefinitelyNonNil(load()) = true, want false")
		}
	})

	t.Run("a plain identifier says nothing", func(t *testing.T) {
		if c.isDefinitelyNonNil(ast.NewIdent("other")) {
			t.Fatal("isDefinitelyNonNil(other) = true, want false")
		}
	})
}
