package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// nilResults records which results a function returns as a literal nil on some
// path where its error result, if it has one, is also nil. Results lists the
// indexes that can be nil unconditionally. NilWhenParamNil additionally records
// an index whose only nil-yielding returns are all dominated by a check that a
// specific parameter is nil — a guarded mapper such as:
//
//	func toDto(e *entity) *Dto {
//		if e == nil {
//			return nil
//		}
//		return &Dto{...}
//	}
//
// A result appears in at most one of the two: an index moves out of Results
// and into NilWhenParamNil only when every one of its nil-yielding returns is
// guarded by a nil check on the very same parameter.
type nilResults struct {
	Results         []int
	NilWhenParamNil map[int]int
}

func (*nilResults) AFact() {}

func (f *nilResults) String() string {
	parts := make([]string, 0, len(f.Results)+len(f.NilWhenParamNil))

	for _, r := range f.Results {
		parts = append(parts, "may return nil result "+strconv.Itoa(r))
	}

	condIndexes := make([]int, 0, len(f.NilWhenParamNil))
	for idx := range f.NilWhenParamNil {
		condIndexes = append(condIndexes, idx)
	}

	slices.Sort(condIndexes)

	for _, idx := range condIndexes {
		parts = append(parts, "may return nil result "+strconv.Itoa(idx)+
			" when param "+strconv.Itoa(f.NilWhenParamNil[idx])+" is nil")
	}

	return strings.Join(parts, ", ")
}

// exportNilResultsFact records, for fd, the zero-based result indexes it returns
// as a literal nil on some return statement where its error result (if it has
// one) is also nil, splitting them into the unconditional and the
// parameter-guarded case. It returns whether it exported a fact that was not
// already recorded.
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

	fact, ok := c.computeNilResultsFact(fd, fn)
	if !ok {
		return false
	}

	c.pass.ExportObjectFact(fn, fact)

	return true
}

// computeNilResultsFact builds the nilResults fact for fd/fn from its body
// alone, with no dependency on the pass's fact store, which is what lets it be
// exercised directly from a unit test. It reports false when fd returns no
// literal nil for any result.
func (c *checker) computeNilResultsFact(fd *ast.FuncDecl, fn *types.Func) (*nilResults, bool) {
	returnsByIndex := c.collectNilYieldingReturns(fd, fn)
	if len(returnsByIndex) == 0 {
		return nil, false
	}

	params := paramIndexByName(fn.Signature().Params())

	var indexes []int

	condParam := map[int]int{}

	for idx, rets := range returnsByIndex {
		if p, ok := c.soleGuardingParam(fd.Body, rets, params); ok {
			condParam[idx] = p

			continue
		}

		indexes = append(indexes, idx)
	}

	slices.Sort(indexes)

	fact := &nilResults{Results: indexes}
	if len(condParam) > 0 {
		fact.NilWhenParamNil = condParam
	}

	return fact, true
}

// collectNilYieldingReturns groups, by result index, every return statement in
// fd's body that yields a literal nil for that index per collectNilResultIndexes.
func (c *checker) collectNilYieldingReturns(fd *ast.FuncDecl, fn *types.Func) map[int][]*ast.ReturnStmt {
	results := fn.Signature().Results()
	errIdx := lastErrorResultIndex(results)

	returnsByIndex := map[int][]*ast.ReturnStmt{}

	inspectSkippingFuncLits(fd.Body, func(n ast.Node) {
		ret, isReturn := n.(*ast.ReturnStmt)
		if !isReturn {
			return
		}

		marked := map[int]bool{}
		c.collectNilResultIndexes(ret, results, errIdx, marked)

		for idx := range marked {
			returnsByIndex[idx] = append(returnsByIndex[idx], ret)
		}
	})

	return returnsByIndex
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

// paramIndexByName maps each named parameter in params to its zero-based
// index, which is what lets a guard's compared identifier be matched back to
// the call-site argument occupying the same position.
func paramIndexByName(params *types.Tuple) map[string]int {
	out := make(map[string]int, params.Len())

	for i := 0; i < params.Len(); i++ {
		if name := params.At(i).Name(); name != "" {
			out[name] = i
		}
	}

	return out
}

// soleGuardingParam reports the single parameter index that dominates every
// return in rets with a nil check, when one exists. It requires every return to
// be located inside body and to share exactly one common guarding parameter;
// any return missing a guard, or a mismatch between the returns' guards,
// leaves the result unconditional.
func (c *checker) soleGuardingParam(body *ast.BlockStmt, rets []*ast.ReturnStmt, params map[string]int) (int, bool) {
	if len(params) == 0 {
		return 0, false
	}

	var common map[int]bool

	for i, ret := range rets {
		guarded, found := c.guardedParamsIn(body.List, ret, map[int]bool{}, params)
		if !found {
			return 0, false
		}

		if i == 0 {
			common = guarded

			continue
		}

		common = intersectInts(common, guarded)
	}

	if len(common) != 1 {
		return 0, false
	}

	for idx := range common {
		return idx, true
	}

	return 0, false
}

// guardedParamsIn searches stmts for target and, when found, reports the set of
// parameter indices proven nil by every if statement that dominates it — active
// carries the guards an enclosing if has already established.
func (c *checker) guardedParamsIn(stmts []ast.Stmt, target *ast.ReturnStmt, active map[int]bool, params map[string]int) (map[int]bool, bool) {
	for _, stmt := range stmts {
		if found, ok := c.guardedParamsInStmt(stmt, target, active, params); ok {
			return found, true
		}
	}

	return nil, false
}

func (c *checker) guardedParamsInStmt(stmt ast.Stmt, target *ast.ReturnStmt, active map[int]bool, params map[string]int) (map[int]bool, bool) {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		if s == target {
			return active, true
		}

		return nil, false
	case *ast.BlockStmt:
		return c.guardedParamsIn(s.List, target, active, params)
	case *ast.LabeledStmt:
		return c.guardedParamsInStmt(s.Stmt, target, active, params)
	case *ast.IfStmt:
		return c.guardedParamsInIf(s, target, active, params)
	default:
		return nil, false
	}
}

// guardedParamsInIf handles one if statement: the condition's nil-when-true
// parameters are added to the active set for the then-branch only, since that
// is the one branch where the condition is known to have been true.
func (c *checker) guardedParamsInIf(s *ast.IfStmt, target *ast.ReturnStmt, active map[int]bool, params map[string]int) (map[int]bool, bool) {
	inner := unionInts(active, c.paramNilOnTrue(s.Cond, params))

	if found, ok := c.guardedParamsIn(s.Body.List, target, inner, params); ok {
		return found, true
	}

	switch e := s.Else.(type) {
	case *ast.BlockStmt:
		return c.guardedParamsIn(e.List, target, active, params)
	case *ast.IfStmt:
		return c.guardedParamsInIf(e, target, active, params)
	default:
		return nil, false
	}
}

// paramNilOnTrue reports the parameters among params guaranteed nil whenever
// cond evaluates true, decomposed the same way nilGuards decomposes a
// condition: parens, a leading !, and && chains, bottoming out on the same
// nilComparisonPath nilGuards itself uses to recognise `x == nil` / `x != nil`.
func (c *checker) paramNilOnTrue(cond ast.Expr, params map[string]int) map[int]bool {
	switch e := ast.Unparen(cond).(type) {
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return nil
		}

		return c.paramNilOnFalse(e.X, params)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND:
			return unionInts(c.paramNilOnTrue(e.X, params), c.paramNilOnTrue(e.Y, params))
		case token.EQL:
			return c.paramFromComparison(e, params)
		default:
			return nil
		}
	default:
		return nil
	}
}

// paramNilOnFalse is paramNilOnTrue's mirror for the branch taken when cond
// evaluates false — the shape a `!` or a De Morgan-flipped `||` reduces to.
func (c *checker) paramNilOnFalse(cond ast.Expr, params map[string]int) map[int]bool {
	switch e := ast.Unparen(cond).(type) {
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return nil
		}

		return c.paramNilOnTrue(e.X, params)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LOR:
			return unionInts(c.paramNilOnFalse(e.X, params), c.paramNilOnFalse(e.Y, params))
		case token.NEQ:
			return c.paramFromComparison(e, params)
		default:
			return nil
		}
	default:
		return nil
	}
}

// paramFromComparison reports the single named parameter e compares against
// the nil literal, when there is one.
func (c *checker) paramFromComparison(e *ast.BinaryExpr, params map[string]int) map[int]bool {
	path, ok := c.nilComparisonPath(e, nil)
	if !ok {
		return nil
	}

	idx, ok := params[path]
	if !ok {
		return nil
	}

	return map[int]bool{idx: true}
}

func unionInts(a, b map[int]bool) map[int]bool {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	out := make(map[int]bool, len(a)+len(b))

	for idx := range a {
		out[idx] = true
	}

	for idx := range b {
		out[idx] = true
	}

	return out
}

func intersectInts(a, b map[int]bool) map[int]bool {
	out := map[int]bool{}

	for idx := range a {
		if b[idx] {
			out[idx] = true
		}
	}

	return out
}

// calleeNilResultsFact resolves call's callee to a declared function or method
// and reports its nilResults fact, when it carries one. It reports false for a
// conversion, a builtin, or a call through a func value rather than a declared
// function or method.
func (c *checker) calleeNilResultsFact(call *ast.CallExpr) (nilResults, bool) {
	var ident *ast.Ident

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nilResults{}, false
	}

	fn, isFunc := c.resolve(ident).(*types.Func)
	if !isFunc {
		return nilResults{}, false
	}

	var fact nilResults
	if !c.pass.ImportObjectFact(fn, &fact) {
		return nilResults{}, false
	}

	return fact, true
}

// nilableResultIndexes reports the result indexes call's callee may return as a
// literal nil at this call site: every unconditionally nil-able index from its
// nilResults fact, plus every parameter-guarded index whose guarding
// parameter's argument is not provably non-nil here.
func (c *checker) nilableResultIndexes(call *ast.CallExpr, sc scope) []int {
	fact, ok := c.calleeNilResultsFact(call)
	if !ok {
		return nil
	}

	out := slices.Clone(fact.Results)

	for idx, paramIdx := range fact.NilWhenParamNil {
		if c.callArgDefinitelyNonNil(call, paramIdx, sc) {
			continue
		}

		out = append(out, idx)
	}

	return out
}

// callArgDefinitelyNonNil reports whether call's paramIdx-th argument is
// provably non-nil at this call site: an address-of or new(T) expression
// (isDefinitelyNonNil), or an argument whose own canonical path is already
// proven non-nil in sc.
func (c *checker) callArgDefinitelyNonNil(call *ast.CallExpr, paramIdx int, sc scope) bool {
	if paramIdx < 0 || paramIdx >= len(call.Args) {
		return false
	}

	arg := call.Args[paramIdx]

	if c.isDefinitelyNonNil(arg) {
		return true
	}

	path, ok := canonicalPath(arg, sc.alias)

	return ok && sc.proven[path]
}

// markNilResultsFromCall marks maybe-nil, in sc, the names among lhs that
// receive a result rhs's callee may return as a literal nil beside a nil
// error. Used for both `v, err := f()` and `var v, err = f()`.
func (c *checker) markNilResultsFromCall(lhs []ast.Expr, rhs ast.Expr, sc scope) {
	call, isCall := ast.Unparen(rhs).(*ast.CallExpr)
	if !isCall {
		return
	}

	for _, j := range c.nilableResultIndexes(call, sc) {
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
