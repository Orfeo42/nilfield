package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// parseAndCheck parses src as a standalone Go file and type-checks it with no
// importer, which is enough for snippets that import nothing, and returns the
// file's single top-level function declaration alongside the real type info
// backing it.
func parseAndCheck(t *testing.T, src string) (*ast.FuncDecl, *types.Info) {
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

	var fd *ast.FuncDecl

	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok {
			fd = f

			break
		}
	}

	if fd == nil {
		t.Fatal("snippet declares no function")
	}

	return fd, info
}

// paramObject finds the *types.Object for the parameter named name in fd.
func paramObject(t *testing.T, fd *ast.FuncDecl, info *types.Info, name string) types.Object {
	t.Helper()

	for _, field := range fd.Type.Params.List {
		for _, id := range field.Names {
			if id.Name == name {
				return info.ObjectOf(id)
			}
		}
	}

	t.Fatalf("snippet declares no parameter named %q", name)

	return nil
}

func newSnippetChecker(info *types.Info) *checker {
	return &checker{
		pass:    &analysis.Pass{TypesInfo: info},
		resolve: info.ObjectOf,
	}
}

func TestAssertedParam(t *testing.T) {
	t.Run("if p { return } then work then panic", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(ok bool, msg string) {
	if ok {
		return
	}

	trace := msg + "!"
	panic(trace)
}
`)

		c := newSnippetChecker(info)
		if !c.paramIsAsserted(fd.Body, paramObject(t, fd, info, "ok")) {
			t.Fatal("paramIsAsserted() = false, want true for a panic after an assignment")
		}
	})

	t.Run("if !p { panic }", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(ok bool) {
	if !ok {
		panic("x")
	}
}
`)

		c := newSnippetChecker(info)
		ok := paramObject(t, fd, info, "ok")

		if !c.paramIsAsserted(fd.Body, ok) {
			t.Fatal("paramIsAsserted() = false, want true")
		}
	})

	t.Run("if p { return }; panic", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(ok bool) {
	if ok {
		return
	}

	panic("x")
}
`)

		c := newSnippetChecker(info)
		ok := paramObject(t, fd, info, "ok")

		if !c.paramIsAsserted(fd.Body, ok) {
			t.Fatal("paramIsAsserted() = false, want true")
		}
	})

	t.Run("second bool parameter", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(x int, ok bool) {
	if !ok {
		panic("x")
	}
}
`)

		c := newSnippetChecker(info)
		ok := paramObject(t, fd, info, "ok")
		x := paramObject(t, fd, info, "x")

		if !c.paramIsAsserted(fd.Body, ok) {
			t.Fatal("paramIsAsserted(ok) = false, want true")
		}

		if c.paramIsAsserted(fd.Body, x) {
			t.Fatal("paramIsAsserted(x) = true, want false")
		}
	})

	t.Run("if p { return } with no trailing panic", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(ok bool) {
	if !ok {
		return
	}
}
`)

		c := newSnippetChecker(info)
		ok := paramObject(t, fd, info, "ok")

		if c.paramIsAsserted(fd.Body, ok) {
			t.Fatal("paramIsAsserted() = true, want false")
		}
	})

	t.Run("body without any if", func(t *testing.T) {
		fd, info := parseAndCheck(t, `package p

func f(ok bool) {
	_ = ok
}
`)

		c := newSnippetChecker(info)
		ok := paramObject(t, fd, info, "ok")

		if c.paramIsAsserted(fd.Body, ok) {
			t.Fatal("paramIsAsserted() = true, want false")
		}
	})
}

func TestCallNeverReturns(t *testing.T) {
	t.Run("builtin panic call", func(t *testing.T) {
		c := &checker{resolve: universeResolver}
		call := &ast.CallExpr{Fun: ast.NewIdent("panic")}

		if !c.callNeverReturns(call) {
			t.Fatal("callNeverReturns(panic(...)) = false, want true")
		}
	})

	t.Run("ordinary call with no fact", func(t *testing.T) {
		c := &checker{resolve: universeResolver}
		call := &ast.CallExpr{Fun: ast.NewIdent("cleanup")}

		if c.callNeverReturns(call) {
			t.Fatal("callNeverReturns(cleanup(...)) = true, want false")
		}
	})
}
