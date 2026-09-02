package analyzer

import (
	"go/ast"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// nilResults records which results a function returns as a literal nil on some
// path where its error result, if it has one, is also nil.
type nilResults struct{ Results []int }

func (*nilResults) AFact() {}

func (f *nilResults) String() string {
	parts := make([]string, len(f.Results))
	for i, r := range f.Results {
		parts[i] = strconv.Itoa(r)
	}

	return "may return nil result " + strings.Join(parts, ", ")
}

// exportNilResultsFact records, for fd, the zero-based result indexes it returns
// as a literal nil on some return statement where its error result (if it has
// one) is also nil. It returns whether it exported a fact that was not already
// recorded.
func (c *checker) exportNilResultsFact(fd *ast.FuncDecl) bool {
	if fd.Body == nil {
		return false
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func)
	if !isFunc {
		return false
	}

	var scratch nilResults
	if c.pass.ImportObjectFact(fn, &scratch) {
		return false
	}

	results := fn.Signature().Results()
	errIdx := lastErrorResultIndex(results)

	marked := map[int]bool{}

	inspectSkippingFuncLits(fd.Body, func(n ast.Node) {
		ret, isReturn := n.(*ast.ReturnStmt)
		if !isReturn {
			return
		}

		c.collectNilResultIndexes(ret, results, errIdx, marked)
	})

	if len(marked) == 0 {
		return false
	}

	indexes := make([]int, 0, len(marked))
	for idx := range marked {
		indexes = append(indexes, idx)
	}

	slices.Sort(indexes)

	c.pass.ExportObjectFact(fn, &nilResults{Results: indexes})

	return true
}

// collectNilResultIndexes marks, in out, the result indexes ret returns as a
// literal nil. A return is skipped entirely when its shape does not match the
// signature (a bare return, or a `return f()` expansion), and when its own
// error result is not itself the nil identifier: a nil value beside a real
// error is the normal failure shape, not the hazard this fact is about.
func (c *checker) collectNilResultIndexes(ret *ast.ReturnStmt, results *types.Tuple, errIdx int, out map[int]bool) {
	if len(ret.Results) != results.Len() {
		return
	}

	if errIdx >= 0 && !c.isNilIdent(ast.Unparen(ret.Results[errIdx])) {
		return
	}

	for j, expr := range ret.Results {
		if j == errIdx {
			continue
		}

		rt := results.At(j).Type()
		if isErrorType(rt) {
			continue
		}

		if c.isNilIdent(ast.Unparen(expr)) && isNilResultKind(rt) {
			out[j] = true
		}
	}
}

// isNilResultKind reports whether t is one of the kinds where returning a
// literal nil is a hazard rather than an ordinary empty value. A slice is
// excluded: a nil slice behaves like an empty one, not a missing value.
func isNilResultKind(t types.Type) bool {
	switch types.Unalias(t).Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Map, *types.Chan, *types.Signature:
		return true
	default:
		return false
	}
}

// lastErrorResultIndex is the index of the last result whose type is exactly
// the universe error type, or -1 when the signature carries none.
func lastErrorResultIndex(results *types.Tuple) int {
	errIdx := -1

	for i := 0; i < results.Len(); i++ {
		if isErrorType(results.At(i).Type()) {
			errIdx = i
		}
	}

	return errIdx
}

// calleeNilResults reports the result indexes call's callee may return as a
// literal nil beside a nil error, per the nilResults fact. It reports nil for a
// conversion, a builtin, or a call through a func value rather than a declared
// function or method.
func (c *checker) calleeNilResults(call *ast.CallExpr) []int {
	var ident *ast.Ident

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nil
	}

	fn, isFunc := c.resolve(ident).(*types.Func)
	if !isFunc {
		return nil
	}

	var fact nilResults
	if !c.pass.ImportObjectFact(fn, &fact) {
		return nil
	}

	return fact.Results
}

// markNilResultsFromCall marks maybe-nil, in sc, the names among lhs that
// receive a result rhs's callee may return as a literal nil beside a nil
// error. Used for both `v, err := f()` and `var v, err = f()`.
func (c *checker) markNilResultsFromCall(lhs []ast.Expr, rhs ast.Expr, sc scope) {
	call, isCall := ast.Unparen(rhs).(*ast.CallExpr)
	if !isCall {
		return
	}

	for _, j := range c.calleeNilResults(call) {
		if j < 0 || j >= len(lhs) {
			continue
		}

		id, isIdent := lhs[j].(*ast.Ident)
		if !isIdent || id.Name == "_" {
			continue
		}

		sc.markNil(id.Name, maybeNil)
	}
}
