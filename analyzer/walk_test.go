package analyzer

import (
	"go/ast"
	"testing"
)

// returnExpr returns the single expression in fd's first return statement, which
// is enough to reach the map/slice-index or type-assert expression a snippet in
// this file exists to hold.
func returnExpr(t *testing.T, fd *ast.FuncDecl) ast.Expr {
	t.Helper()

	for _, stmt := range fd.Body.List {
		ret, isReturn := stmt.(*ast.ReturnStmt)
		if !isReturn || len(ret.Results) != 1 {
			continue
		}

		return ret.Results[0]
	}

	t.Fatal("snippet declares no single-result return statement")

	return nil
}

func TestIsNilOrigin(t *testing.T) {
	t.Run("map index of a pointer element is a nil origin", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type T struct{}

func f(m map[string]*T) *T {
	return m["k"]
}
`)

		c := newSnippetChecker(info)

		if !c.isNilOrigin(returnExpr(t, fd), newScope()) {
			t.Fatal("isNilOrigin(map[string]*T index) = false, want true")
		}
	})

	t.Run("slice index of a pointer element is a nil origin", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type T struct{}

func f(s []*T) *T {
	return s[0]
}
`)

		c := newSnippetChecker(info)

		if !c.isNilOrigin(returnExpr(t, fd), newScope()) {
			t.Fatal("isNilOrigin([]*T index) = false, want true")
		}
	})

	t.Run("map index of an int element is not a nil origin", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(m map[string]int) int {
	return m["k"]
}
`)

		c := newSnippetChecker(info)

		if c.isNilOrigin(returnExpr(t, fd), newScope()) {
			t.Fatal("isNilOrigin(map[string]int index) = true, want false")
		}
	})

	t.Run("single-form type assertion to a pointer is a nil origin", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type T struct{}

func f(v any) *T {
	return v.(*T)
}
`)

		c := newSnippetChecker(info)

		if !c.isNilOrigin(returnExpr(t, fd), newScope()) {
			t.Fatal("isNilOrigin(v.(*T)) = false, want true")
		}
	})

	t.Run("single-form type assertion to an int is not a nil origin", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(v any) int {
	return v.(int)
}
`)

		c := newSnippetChecker(info)

		if c.isNilOrigin(returnExpr(t, fd), newScope()) {
			t.Fatal("isNilOrigin(v.(int)) = true, want false")
		}
	})
}
