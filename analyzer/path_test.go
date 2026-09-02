package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"
)

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

	t.Run("star expression wraps its operand", func(t *testing.T) {
		expr := &ast.StarExpr{X: ast.NewIdent("pp")}

		path, ok := canonicalPath(expr, map[string]string{})
		if !ok || path != "(*pp)" {
			t.Fatalf("canonicalPath() = %q, %v, want \"(*pp)\", true", path, ok)
		}
	})

	t.Run("a selector on a star expression joins with a dot", func(t *testing.T) {
		expr := &ast.SelectorExpr{X: &ast.StarExpr{X: ast.NewIdent("pp")}, Sel: ast.NewIdent("n")}

		path, ok := canonicalPath(expr, map[string]string{})
		if !ok || path != "(*pp).n" {
			t.Fatalf("canonicalPath() = %q, %v, want \"(*pp).n\", true", path, ok)
		}
	})
}

func TestIsFieldPath(t *testing.T) {
	t.Run("a selector path contains a dot", func(t *testing.T) {
		if !isFieldPath("o.p") {
			t.Fatal("isFieldPath(\"o.p\") = false, want true")
		}
	})

	t.Run("a bare identifier has no dot", func(t *testing.T) {
		if isFieldPath("o") {
			t.Fatal("isFieldPath(\"o\") = true, want false")
		}
	})
}

func TestIsStarPath(t *testing.T) {
	t.Run("a star path starts with the star marker", func(t *testing.T) {
		if !isStarPath("(*pp)") {
			t.Fatal("isStarPath(\"(*pp)\") = false, want true")
		}
	})

	t.Run("a bare identifier is not a star path", func(t *testing.T) {
		if isStarPath("pp") {
			t.Fatal("isStarPath(\"pp\") = true, want false")
		}
	})
}

func TestIsErrorType(t *testing.T) {
	t.Run("the universe error type is an error type", func(t *testing.T) {
		if !isErrorType(errorType) {
			t.Fatal("isErrorType(errorType) = false, want true")
		}
	})

	t.Run("int is not an error type", func(t *testing.T) {
		if isErrorType(types.Typ[types.Int]) {
			t.Fatal("isErrorType(int) = true, want false")
		}
	})
}

func TestIsNillableKind(t *testing.T) {
	t.Run("pointer is nillable", func(t *testing.T) {
		if !isNillableKind(types.NewPointer(types.Typ[types.Int])) {
			t.Fatal("isNillableKind(pointer) = false, want true")
		}
	})

	t.Run("interface is nillable", func(t *testing.T) {
		if !isNillableKind(types.NewInterfaceType(nil, nil)) {
			t.Fatal("isNillableKind(interface) = false, want true")
		}
	})

	t.Run("map is nillable", func(t *testing.T) {
		if !isNillableKind(types.NewMap(types.Typ[types.String], types.Typ[types.Int])) {
			t.Fatal("isNillableKind(map) = false, want true")
		}
	})

	t.Run("slice is nillable", func(t *testing.T) {
		if !isNillableKind(types.NewSlice(types.Typ[types.Int])) {
			t.Fatal("isNillableKind(slice) = false, want true")
		}
	})

	t.Run("chan is nillable", func(t *testing.T) {
		if !isNillableKind(types.NewChan(types.SendRecv, types.Typ[types.Int])) {
			t.Fatal("isNillableKind(chan) = false, want true")
		}
	})

	t.Run("func is nillable", func(t *testing.T) {
		sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)

		if !isNillableKind(sig) {
			t.Fatal("isNillableKind(func) = false, want true")
		}
	})

	t.Run("int is not nillable", func(t *testing.T) {
		if isNillableKind(types.Typ[types.Int]) {
			t.Fatal("isNillableKind(int) = true, want false")
		}
	})

	t.Run("struct is not nillable", func(t *testing.T) {
		if isNillableKind(types.NewStruct(nil, nil)) {
			t.Fatal("isNillableKind(struct) = true, want false")
		}
	})
}

func TestAddressTakenNames(t *testing.T) {
	t.Run("address of a local is listed", func(t *testing.T) {
		call := &ast.CallExpr{Fun: ast.NewIdent("f"), Args: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: ast.NewIdent("x")}}}
		got := addressTakenNames(&ast.ExprStmt{X: call})

		if len(got) != 1 || got[0] != "x" {
			t.Fatalf("addressTakenNames() = %v, want [x]", got)
		}
	})

	t.Run("address of a field is not a local", func(t *testing.T) {
		sel := &ast.SelectorExpr{X: ast.NewIdent("o"), Sel: ast.NewIdent("p")}
		got := addressTakenNames(&ast.ExprStmt{X: &ast.UnaryExpr{Op: token.AND, X: sel}})

		if len(got) != 0 {
			t.Fatalf("addressTakenNames() = %v, want none", got)
		}
	})

	t.Run("a plain use is not listed", func(t *testing.T) {
		got := addressTakenNames(&ast.ExprStmt{X: ast.NewIdent("x")})

		if len(got) != 0 {
			t.Fatalf("addressTakenNames() = %v, want none", got)
		}
	})
}
