// Package analyzer reports method calls and field accesses made through a
// pointer-typed struct field that no nil guard dominates, the shape that turns
// a missing check into a nil-pointer panic at runtime.
//
// A path is proven non-nil by an `x != nil` condition (including inside `&&`/`||`
// chains and behind `!`), by an early return/panic in the opposite branch, by an
// assignment of an address or `new(...)`, by a call to an assert helper, a
// function whose body panics unless its boolean argument holds, recognised from
// its body and carried as a fact, or by a checked call to a validator method: a
// method returning a single `error` whose body rejects a nil receiver field
// before any path that can return a nil error. Writing to the field again drops
// the proof.
//
// The validator rule is derived from the callee's own guards and travels between
// packages as an analysis fact, so nothing has to be annotated at the call site.
// The one assumption it makes is that the expression a guard returns - a call, an
// address, or a package-level sentinel - is really non-nil; a guard returning a
// local variable or `nil` proves nothing. A validator that, outside a return
// statement, calls a pointer-receiver method through its receiver or hands a
// pointer, slice, map, channel, func or interface rooted at the receiver to
// another call exports no fact, since that call could clear the field.
//
// Only fields reached through a struct are reported: a bare local or parameter is
// out of scope, because the guard there is hard to miss.
//
// Every finding is reported; the gate is zero findings, there is no baseline.
//
// Install and run it from the root of the module being checked:
//
//	go install github.com/Orfeo42/nilfield/cmd/nilfield@latest
//	nilfield ./...
//	nilfield -exclude-paths=internal/dao/ ./...
package analyzer

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// generatedFileMarker is the same convention cmd/go and the rest of the Go
// ecosystem use to recognise generated source: a line matching this pattern,
// appearing before the package clause.
var generatedFileMarker = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// Config holds the flags New wires onto the analyzer it builds.
type Config struct {
	ExcludePaths string
}

// New builds a nilfield analyzer configured with cfg, whose fields are also
// exposed as the -exclude-paths flag so a driver like singlechecker can still
// override them from the command line.
func New(cfg Config) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name:     "nilfield",
		Doc:      "reports method calls and field accesses on pointer-typed struct fields with no local nil guard",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		FactTypes: []analysis.Fact{
			(*validatedFields)(nil),
			(*assertHelper)(nil),
			(*neverReturns)(nil),
			(*nilSafeReceiver)(nil),
			(*nilResults)(nil),
		},
	}

	a.Flags.StringVar(&cfg.ExcludePaths, "exclude-paths", cfg.ExcludePaths,
		"comma-separated path fragments; a file whose slash-normalized path contains one is never reported")

	a.Run = func(pass *analysis.Pass) (any, error) {
		return run(pass, cfg)
	}

	return a
}

// Analyzer is the default nilfield analyzer, with no excluded paths beyond what
// flags set at runtime.
var Analyzer = New(Config{})

func run(pass *analysis.Pass, cfg Config) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	c := &checker{
		pass:           pass,
		excludePaths:   splitFragments(cfg.ExcludePaths),
		walked:         map[*ast.FuncLit]bool{},
		resolve:        pass.TypesInfo.ObjectOf,
		generatedFiles: generatedFiles(pass),
	}

	var funcDecls []*ast.FuncDecl

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		funcDecls = append(funcDecls, n.(*ast.FuncDecl))
	})

	for _, fd := range funcDecls {
		c.exportValidatorFact(fd)
		c.exportNilResultsFact(fd)
	}

	for range funcDecls {
		exportedNew := false

		for _, fd := range funcDecls {
			if c.exportNeverReturnsFact(fd) {
				exportedNew = true
			}

			if c.exportAssertHelperFact(fd) {
				exportedNew = true
			}

			if c.exportNilSafeReceiverFact(fd) {
				exportedNew = true
			}
		}

		if !exportedNew {
			break
		}
	}

	c.wired = c.computeWiredFields()

	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		var body *ast.BlockStmt

		switch f := n.(type) {
		case *ast.FuncDecl:
			body = f.Body
		case *ast.FuncLit:
			if c.walked[f] {
				return
			}

			body = f.Body
		}

		if body == nil {
			return
		}

		c.walk(body.List, newScope())
	})

	return nil, nil
}

func splitFragments(raw string) []string {
	var out []string

	for part := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		out = append(out, filepath.ToSlash(trimmed))
	}

	return out
}

type checker struct {
	pass           *analysis.Pass
	excludePaths   []string
	walked         map[*ast.FuncLit]bool
	resolve        func(*ast.Ident) types.Object
	silent         bool
	receiver       string
	receiverDeref  bool
	wired          map[*types.Var]bool
	generatedFiles map[string]bool
}

func (c *checker) isExcluded(filename string) bool {
	normalized := filepath.ToSlash(filename)

	if strings.HasSuffix(normalized, "_test.go") {
		return true
	}

	if c.generatedFiles[normalized] {
		return true
	}

	for _, fragment := range c.excludePaths {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}

// generatedFiles returns the set of files in pass, keyed by their
// slash-normalized path, whose comments carry the Go ecosystem's generated-
// code marker before the package clause: reporting in generated code is
// unactionable, since the fix belongs in the generator, not the output. This
// is a convention, not a configurable exclusion, so it needs no flag and no
// way to turn it off.
func generatedFiles(pass *analysis.Pass) map[string]bool {
	out := map[string]bool{}

	for _, file := range pass.Files {
		if isGeneratedFile(file) {
			out[filepath.ToSlash(pass.Fset.Position(file.Package).Filename)] = true
		}
	}

	return out
}

// isGeneratedFile reports whether file carries a comment line matching
// generatedFileMarker before its package clause.
func isGeneratedFile(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.End() >= file.Package {
			continue
		}

		for _, comment := range group.List {
			if generatedFileMarker.MatchString(comment.Text) {
				return true
			}
		}
	}

	return false
}

// isPackageQualified reports whether base is a selector into another package
// (pkg.GlobalVar), which is not a struct field and out of scope the same way a bare
// local is.
func (c *checker) isPackageQualified(base ast.Expr) bool {
	sel, isSelector := base.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}

	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return false
	}

	_, isPkgName := c.pass.TypesInfo.ObjectOf(id).(*types.PkgName)

	return isPkgName
}
