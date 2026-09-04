package slogfmt

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSlogCallKVStart(t *testing.T) {
	t.Run("Debug", func(t *testing.T) {
		assertSlogCallKVStart(t, "Debug", 1)
	})

	t.Run("Info", func(t *testing.T) {
		assertSlogCallKVStart(t, "Info", 1)
	})

	t.Run("Warn", func(t *testing.T) {
		assertSlogCallKVStart(t, "Warn", 1)
	})

	t.Run("Error", func(t *testing.T) {
		assertSlogCallKVStart(t, "Error", 1)
	})

	t.Run("DebugContext", func(t *testing.T) {
		assertSlogCallKVStart(t, "DebugContext", 2)
	})

	t.Run("InfoContext", func(t *testing.T) {
		assertSlogCallKVStart(t, "InfoContext", 2)
	})

	t.Run("WarnContext", func(t *testing.T) {
		assertSlogCallKVStart(t, "WarnContext", 2)
	})

	t.Run("ErrorContext", func(t *testing.T) {
		assertSlogCallKVStart(t, "ErrorContext", 2)
	})

	t.Run("Log", func(t *testing.T) {
		assertSlogCallKVStart(t, "Log", 3)
	})

	t.Run("non-slog selector", func(t *testing.T) {
		call := mustParseCallExpr(t, `logger.Info("msg", "k", 1)`)

		if _, ok := slogCallKVStart(call); ok {
			t.Fatalf("slogCallKVStart() ok = true, want false")
		}
	})

	t.Run("bare ident call", func(t *testing.T) {
		call := mustParseCallExpr(t, `println("x")`)

		if _, ok := slogCallKVStart(call); ok {
			t.Fatalf("slogCallKVStart() ok = true, want false")
		}
	})
}

func assertSlogCallKVStart(t *testing.T, name string, want int) {
	t.Helper()

	call := mustParseCallExpr(t, "slog."+name+`(a, b, c)`)

	got, ok := slogCallKVStart(call)
	if !ok {
		t.Fatalf("slogCallKVStart() ok = false, want true")
	}

	if got != want {
		t.Fatalf("slogCallKVStart() = %d, want %d", got, want)
	}
}

func mustParseCallExpr(t *testing.T, src string) *ast.CallExpr {
	t.Helper()

	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", src, err)
	}

	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("ParseExpr(%q) = %T, want *ast.CallExpr", src, expr)
	}

	return call
}

func TestRewriteCall(t *testing.T) {
	t.Run("single pair", func(t *testing.T) {
		call, src, fset := parseSlogCall(t, "\tslog.Info(\"msg\", \"k1\", 1)")

		got, err := rewriteCall(src, call, fset)
		if err != nil {
			t.Fatalf("rewriteCall() error = %v", err)
		}

		want := "slog.Info(\"msg\",\n\t\t\"k1\", 1,\n\t)"
		if string(got) != want {
			t.Fatalf("rewriteCall() = %q, want %q", got, want)
		}
	})

	t.Run("several pairs", func(t *testing.T) {
		call, src, fset := parseSlogCall(t, "\tslog.Error(\"msg\", \"k1\", 1, \"k2\", 2, \"k3\", 3)")

		got, err := rewriteCall(src, call, fset)
		if err != nil {
			t.Fatalf("rewriteCall() error = %v", err)
		}

		want := "slog.Error(\"msg\",\n\t\t\"k1\", 1,\n\t\t\"k2\", 2,\n\t\t\"k3\", 3,\n\t)"
		if string(got) != want {
			t.Fatalf("rewriteCall() = %q, want %q", got, want)
		}
	})

	t.Run("Context variant keeps ctx on the first line", func(t *testing.T) {
		call, src, fset := parseSlogCall(t, "\tslog.ErrorContext(ctx, \"msg\", \"k1\", 1, \"k2\", 2)")

		got, err := rewriteCall(src, call, fset)
		if err != nil {
			t.Fatalf("rewriteCall() error = %v", err)
		}

		want := "slog.ErrorContext(ctx, \"msg\",\n\t\t\"k1\", 1,\n\t\t\"k2\", 2,\n\t)"
		if string(got) != want {
			t.Fatalf("rewriteCall() = %q, want %q", got, want)
		}
	})

	t.Run("odd trailing arg", func(t *testing.T) {
		call, src, fset := parseSlogCall(t, "\tslog.Info(\"msg\", \"k1\", 1, \"k2\")")

		got, err := rewriteCall(src, call, fset)
		if err != nil {
			t.Fatalf("rewriteCall() error = %v", err)
		}

		want := "slog.Info(\"msg\",\n\t\t\"k1\", 1,\n\t\t\"k2\",\n\t)"
		if string(got) != want {
			t.Fatalf("rewriteCall() = %q, want %q", got, want)
		}
	})

	t.Run("call already spread across lines", func(t *testing.T) {
		call, src, fset := parseSlogCall(t, "\tslog.Warn(\"msg\",\n\t\t\"k1\", 1, \"k2\", 2)")

		got, err := rewriteCall(src, call, fset)
		if err != nil {
			t.Fatalf("rewriteCall() error = %v", err)
		}

		want := "slog.Warn(\"msg\",\n\t\t\"k1\", 1,\n\t\t\"k2\", 2,\n\t)"
		if string(got) != want {
			t.Fatalf("rewriteCall() = %q, want %q", got, want)
		}
	})
}

// parseSlogCall wraps body inside a minimal, fully parseable Go file (so
// fset offsets line up exactly with the returned source bytes, which
// rewriteCall requires) and returns the first slog call found in it.
func parseSlogCall(t *testing.T, body string) (*ast.CallExpr, []byte, *token.FileSet) {
	t.Helper()

	src := "package p\n\nimport (\n\t\"context\"\n\t\"log/slog\"\n)\n\nfunc f(ctx context.Context) {\n" + body + "\n}\n"

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	var call *ast.CallExpr

	ast.Inspect(file, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		if _, isSlog := slogCallKVStart(c); isSlog {
			call = c
		}

		return true
	})

	if call == nil {
		t.Fatalf("no slog call found in:\n%s", src)
	}

	return call, []byte(src), fset
}

func TestIsExcluded(t *testing.T) {
	t.Run("filename contains an excluded fragment", func(t *testing.T) {
		if !isExcluded("src/generated/foo.go", []string{"src/generated/"}) {
			t.Fatalf("isExcluded() = false, want true")
		}
	})

	t.Run("filename matches no fragment", func(t *testing.T) {
		if isExcluded("src/domain/foo.go", []string{"src/generated/"}) {
			t.Fatalf("isExcluded() = true, want false")
		}
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
				t.Fatalf("splitFragments() = %v, want %v", got, want)
			}
		}
	})
}

func TestAnalyzer(t *testing.T) {
	t.Run("one-line call with two pairs gets a suggested fix", func(t *testing.T) {
		analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), New(Config{}), "twopairs")
	})

	t.Run("context variant keeps ctx on the first line", func(t *testing.T) {
		analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), New(Config{}), "contextpair")
	})

	t.Run("a call already split reports nothing", func(t *testing.T) {
		analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), New(Config{}), "alreadysplit")
	})

	t.Run("attribute arguments already one per line report nothing", func(t *testing.T) {
		analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), New(Config{}), "fullysplit")
	})

	t.Run("a call with no kv pairs reports nothing", func(t *testing.T) {
		analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), New(Config{}), "nokvpairs")
	})

	t.Run("excluded path reports nothing", func(t *testing.T) {
		analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), New(Config{ExcludePaths: "excludedpath/"}), "excludedpath")
	})
}
