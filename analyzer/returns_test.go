package analyzer

import (
	"go/ast"
	"testing"
)

// ifCondOf type-checks src, which must declare a single function whose body's
// first statement is the if statement under test, and returns its condition
// alongside a checker built from the same type info.
func ifCondOf(t *testing.T, src string) (*checker, ast.Expr) {
	t.Helper()

	fd, info := parseAndCheck(t, src)
	c := newSnippetChecker(info)

	ifStmt, isIf := fd.Body.List[0].(*ast.IfStmt)
	if !isIf {
		t.Fatal("snippet function's first statement is not an if")
	}

	return c, ifStmt.Cond
}

func TestErrorGuards(t *testing.T) {
	t.Run("err != nil", func(t *testing.T) {
		c, cond := ifCondOf(t, `package p

func f(err error) int {
	if err != nil {
		return 1
	}

	return 0
}
`)

		whenTrue, whenFalse := c.errorGuards(cond, newScope())

		if len(whenTrue) != 1 || whenTrue[0] != "err" {
			t.Fatalf("errorGuards() whenTrue = %v, want [err]", whenTrue)
		}

		if len(whenFalse) != 0 {
			t.Fatalf("errorGuards() whenFalse = %v, want none", whenFalse)
		}
	})

	t.Run("x != nil with pointer x", func(t *testing.T) {
		c, cond := ifCondOf(t, `package p

type T struct{}

func f(x *T) int {
	if x != nil {
		return 1
	}

	return 0
}
`)

		whenTrue, whenFalse := c.errorGuards(cond, newScope())

		if len(whenTrue) != 0 || len(whenFalse) != 0 {
			t.Fatalf("errorGuards() = (%v, %v), want (none, none)", whenTrue, whenFalse)
		}
	})

	t.Run("err != nil && p != nil", func(t *testing.T) {
		c, cond := ifCondOf(t, `package p

type T struct{}

func f(err error, p *T) int {
	if err != nil && p != nil {
		return 1
	}

	return 0
}
`)

		whenTrue, whenFalse := c.errorGuards(cond, newScope())

		if len(whenTrue) != 1 || whenTrue[0] != "err" {
			t.Fatalf("errorGuards() whenTrue = %v, want [err]", whenTrue)
		}

		if len(whenFalse) != 0 {
			t.Fatalf("errorGuards() whenFalse = %v, want none", whenFalse)
		}
	})
}
