package analyzer

import (
	"go/ast"
	"go/types"
	"testing"
)

func TestIsWiringStruct(t *testing.T) {
	t.Run("all pointer fields qualifies", func(t *testing.T) {
		unitStruct := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "n", types.Typ[types.Int], false),
		}, nil)
		unitPtr := types.NewPointer(unitStruct)

		st := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "a", unitPtr, false),
			types.NewField(0, nil, "b", unitPtr, false),
		}, nil)

		got, ok := isWiringStruct(st)
		if !ok {
			t.Fatal("isWiringStruct() ok = false, want true")
		}

		if got.NumFields() != 2 {
			t.Fatalf("isWiringStruct() struct has %d fields, want 2", got.NumFields())
		}
	})

	t.Run("a named struct wrapping an all-pointer struct qualifies", func(t *testing.T) {
		unitStruct := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "n", types.Typ[types.Int], false),
		}, nil)
		unitPtr := types.NewPointer(unitStruct)

		st := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "a", unitPtr, false),
		}, nil)

		pkg := types.NewPackage("wiringpkg", "wiringpkg")
		obj := types.NewTypeName(0, pkg, "wiring", nil)
		named := types.NewNamed(obj, st, nil)

		if _, ok := isWiringStruct(named); !ok {
			t.Fatal("isWiringStruct(named) ok = false, want true")
		}
	})

	t.Run("a non-pointer non-interface field disqualifies", func(t *testing.T) {
		st := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "n", types.Typ[types.Int], false),
		}, nil)

		if _, ok := isWiringStruct(st); ok {
			t.Fatal("isWiringStruct() ok = true, want false")
		}
	})

	t.Run("empty struct disqualifies", func(t *testing.T) {
		st := types.NewStruct(nil, nil)

		if _, ok := isWiringStruct(st); ok {
			t.Fatal("isWiringStruct() ok = true, want false")
		}
	})

	t.Run("pointer to a wiring struct qualifies", func(t *testing.T) {
		unitStruct := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "n", types.Typ[types.Int], false),
		}, nil)
		unitPtr := types.NewPointer(unitStruct)

		st := types.NewStruct([]*types.Var{
			types.NewField(0, nil, "a", unitPtr, false),
		}, nil)
		ptrToSt := types.NewPointer(st)

		if _, ok := isWiringStruct(ptrToSt); !ok {
			t.Fatal("isWiringStruct(pointer) ok = false, want true")
		}
	})
}

// findCompositeLit returns the single composite literal of the named type
// found in fd's body.
func findCompositeLit(t *testing.T, fd *ast.FuncDecl, typeName string) *ast.CompositeLit {
	t.Helper()

	var found *ast.CompositeLit

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		lit, isLit := n.(*ast.CompositeLit)
		if !isLit {
			return true
		}

		id, isIdent := lit.Type.(*ast.Ident)
		if isIdent && id.Name == typeName {
			found = lit
		}

		return true
	})

	if found == nil {
		t.Fatalf("snippet contains no composite literal of type %q", typeName)
	}

	return found
}

func assertMissingFields(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("missingWiringFields() = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missingWiringFields() = %v, want %v", got, want)
		}
	}
}

func TestMissingWiringFields(t *testing.T) {
	t.Run("omitted key is missing", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type dep interface{ Do() }

type unit struct{ n int }

type wiring struct {
	d dep
	a *unit
	b *unit
}

func f() *wiring {
	return &wiring{a: &unit{}}
}
`)

		lit := findCompositeLit(t, fd, "wiring")
		c := newSnippetChecker(info)

		st, ok := isWiringStruct(info.TypeOf(lit))
		if !ok {
			t.Fatal("isWiringStruct() ok = false, want true")
		}

		assertMissingFields(t, c.missingWiringFields(st, lit), []string{"d", "b"})
	})

	t.Run("explicit nil value is missing", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type dep interface{ Do() }

type unit struct{ n int }

type wiring struct {
	d dep
	a *unit
	b *unit
}

func f() wiring {
	return wiring{d: nil, a: &unit{}, b: &unit{}}
}
`)

		lit := findCompositeLit(t, fd, "wiring")
		c := newSnippetChecker(info)

		st, ok := isWiringStruct(info.TypeOf(lit))
		if !ok {
			t.Fatal("isWiringStruct() ok = false, want true")
		}

		assertMissingFields(t, c.missingWiringFields(st, lit), []string{"d"})
	})

	t.Run("full literal has nothing missing", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

type dep interface{ Do() }

type someDep struct{}

type unit struct{ n int }

type wiring struct {
	d dep
	a *unit
	b *unit
}

func f() *wiring {
	return &wiring{d: someDep{}, a: &unit{}, b: &unit{}}
}

func (someDep) Do() {}
`)

		lit := findCompositeLit(t, fd, "wiring")
		c := newSnippetChecker(info)

		st, ok := isWiringStruct(info.TypeOf(lit))
		if !ok {
			t.Fatal("isWiringStruct() ok = false, want true")
		}

		assertMissingFields(t, c.missingWiringFields(st, lit), nil)
	})
}
