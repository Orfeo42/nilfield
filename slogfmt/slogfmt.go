// Package slogfmt reports slog calls whose key-value argument pairs do not
// each sit on their own line, and attaches a suggested fix that rewrites the
// call so they do — satisfying golangci-lint's sloglint args-on-sep-lines
// rule.
package slogfmt

import (
	"bytes"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Config holds the flags New wires onto the analyzer it builds.
type Config struct {
	ExcludePaths string
}

// New builds a slogfmt analyzer configured with cfg, whose fields are also
// exposed as the -exclude-paths flag so a driver like singlechecker can still
// override them from the command line.
func New(cfg Config) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name:     "slogfmt",
		Doc:      "reports slog calls whose key-value arguments are not each on their own line, with a suggested fix",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}

	a.Flags.StringVar(&cfg.ExcludePaths, "exclude-paths", cfg.ExcludePaths,
		"comma-separated path fragments; a file whose slash-normalized path contains one is never reported")

	a.Run = func(pass *analysis.Pass) (any, error) {
		return run(pass, cfg)
	}

	return a
}

// Analyzer is the default slogfmt analyzer, with no excluded paths beyond
// what flags set at runtime.
var Analyzer = New(Config{})

// run reports every slog call in pass whose key-value arguments need to be
// spread across separate lines.
func run(pass *analysis.Pass, cfg Config) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	excludePaths := splitFragments(cfg.ExcludePaths)
	sources := map[string][]byte{}

	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		checkCall(pass, n.(*ast.CallExpr), excludePaths, sources)
	})

	return nil, nil
}

// checkCall reports a diagnostic with a suggested fix when call is a slog
// call whose key-value arguments need to be spread across separate lines.
func checkCall(pass *analysis.Pass, call *ast.CallExpr, excludePaths []string, sources map[string][]byte) {
	kvStart, isSlog := slogCallKVStart(call)
	if !isSlog {
		return
	}

	if len(call.Args) <= kvStart+1 {
		return
	}

	filename := pass.Fset.Position(call.Pos()).Filename
	if isExcluded(filename, excludePaths) {
		return
	}

	if !sharesLine(pass.Fset.File(call.Pos()), call.Args, kvStart) {
		return
	}

	src, err := readSource(pass, filename, sources)
	if err != nil {
		return
	}

	rewritten, err := rewriteCall(src, call, pass.Fset)
	if err != nil {
		return
	}

	if bytes.Equal(rewritten, callSource(src, pass.Fset, call)) {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: "slog key-value arguments must each be on their own line",
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "put each key-value pair on its own line",
			TextEdits: []analysis.TextEdit{{
				Pos:     call.Pos(),
				End:     call.End(),
				NewText: rewritten,
			}},
		}},
	})
}

// readSource returns the source bytes for filename, reading them through
// pass.ReadFile once per run and caching the result in sources.
func readSource(pass *analysis.Pass, filename string, sources map[string][]byte) ([]byte, error) {
	if src, ok := sources[filename]; ok {
		return src, nil
	}

	src, err := pass.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	sources[filename] = src

	return src, nil
}

// sharesLine reports whether any two consecutive arguments from kvStart on
// share a source line. A call whose arguments already sit one per line is left
// alone whatever they are: attribute-style arguments (slog.String(...) and
// friends) are single values, not key-value pairs, and pairing them up two to a
// line would be wrong.
func sharesLine(tf *token.File, args []ast.Expr, kvStart int) bool {
	for i := kvStart + 1; i < len(args); i++ {
		if tf.Line(args[i-1].Pos()) == tf.Line(args[i].Pos()) {
			return true
		}
	}

	return false
}

// callSource returns the source bytes call currently occupies, which is what
// the rewritten form is compared against: a call already written one pair per
// line rewrites to itself, and reporting it would leave a finding that its own
// fix cannot clear.
func callSource(src []byte, fset *token.FileSet, call *ast.CallExpr) []byte {
	tf := fset.File(call.Pos())

	return src[tf.Offset(call.Pos()):tf.Offset(call.End())]
}

// isExcluded reports whether filename's slash-normalized path contains any
// of fragments.
func isExcluded(filename string, fragments []string) bool {
	normalized := filepath.ToSlash(filename)

	for _, fragment := range fragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}

// splitFragments splits raw on commas, trims whitespace, drops empty
// fragments and slash-normalizes each surviving fragment.
func splitFragments(raw string) []string {
	var out []string

	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		out = append(out, filepath.ToSlash(trimmed))
	}

	return out
}
