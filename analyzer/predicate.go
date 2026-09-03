package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// nilPredicate marks a function returning a single bool that answers true
// whenever its Arg-th argument is nil, so the caller's branch where the call is
// false has the argument proven non-nil.
//
// Only that one direction is derived, and deliberately so. A helper like
// gf's `g.IsNil` decides the rest of its answer by reflection, which no AST
// rule can read; what it does state syntactically is a leading
// `if value == nil { return true }`, and that alone is what the false branch
// needs. Nothing here claims a true result means the argument IS nil.
type nilPredicate struct{ Arg int }

func (*nilPredicate) AFact() {}

func (f *nilPredicate) String() string { return "reports nil argument " + strconv.Itoa(f.Arg) }

// exportNilPredicateFact records, for fd, the parameter its body answers true
// for when that parameter is nil. It returns whether it exported a fact that
// was not already recorded, so run's fixpoint loop can iterate: the delegation
// form below reads the fact of the callee it forwards to, which an earlier
// iteration may not have exported yet.
func (c *checker) exportNilPredicateFact(fd *ast.FuncDecl) bool {
	if fd.Body == nil || fd.Type.Params == nil {
		return false
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func)
	if !isFunc || !returnsSingleBool(fn) {
		return false
	}

	var scratch nilPredicate
	if c.pass.ImportObjectFact(fn, &scratch) {
		return false
	}

	arg, ok := c.nilAnsweredParam(fd)
	if !ok {
		return false
	}

	c.pass.ExportObjectFact(fn, &nilPredicate{Arg: arg})

	return true
}

func returnsSingleBool(fn *types.Func) bool {
	sig, isSignature := fn.Type().(*types.Signature)
	if !isSignature || sig.Results().Len() != 1 {
		return false
	}

	return types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool])
}

// nilAnsweredParam reports the index of the first parameter of fd whose nil
// value the body answers true for, in either of the two shapes this derives:
// a leading `if p == nil { return true }` guard, or a sole `return g(..., p, ...)`
// forwarding to a callee that already carries the fact for that same argument.
func (c *checker) nilAnsweredParam(fd *ast.FuncDecl) (int, bool) {
	for idx, name := range paramNames(fd.Type.Params) {
		obj := c.pass.TypesInfo.ObjectOf(name)
		if obj == nil || !isNillableKind(obj.Type()) {
			continue
		}

		if c.guardsNilParam(fd.Body, name.Name) || c.delegatesNilParam(fd.Body, name.Name) {
			return idx, true
		}
	}

	return 0, false
}

// paramNames flattens a parameter list into one identifier per argument
// position, so an index into it is the index a call site's argument list uses.
// An unnamed or blank parameter has no identifier to match and is skipped.
func paramNames(params *ast.FieldList) []*ast.Ident {
	var out []*ast.Ident

	for _, field := range params.List {
		if len(field.Names) == 0 {
			out = append(out, nil)

			continue
		}

		for _, name := range field.Names {
			if name.Name == "_" {
				out = append(out, nil)

				continue
			}

			out = append(out, name)
		}
	}

	return out
}

// guardsNilParam recognises `if name == nil { return true }` as body's very
// first statement. It has to be the first: a return anywhere before it would
// answer for a nil argument without ever reaching the guard.
func (c *checker) guardsNilParam(body *ast.BlockStmt, name string) bool {
	if len(body.List) == 0 {
		return false
	}

	ifStmt, isIf := body.List[0].(*ast.IfStmt)
	if !isIf || ifStmt.Init != nil {
		return false
	}

	be, isBinary := ifStmt.Cond.(*ast.BinaryExpr)
	if !isBinary || be.Op != token.EQL {
		return false
	}

	path, ok := c.nilComparisonPath(be, nil)
	if !ok || path != name {
		return false
	}

	return c.returnsTrue(ifStmt.Body)
}

// returnsTrue reports whether block is exactly `return true`.
func (c *checker) returnsTrue(block *ast.BlockStmt) bool {
	if len(block.List) != 1 {
		return false
	}

	ret, isReturn := block.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return false
	}

	id, isIdent := ret.Results[0].(*ast.Ident)

	return isIdent && isUniverse(c.resolve(id), "true")
}

// delegatesNilParam recognises a body that is nothing but
// `return g(..., name, ...)`, where g already carries the fact for the
// position name is passed in, which is how a package's public wrapper inherits
// the answer of the internal helper it forwards to.
func (c *checker) delegatesNilParam(body *ast.BlockStmt, name string) bool {
	if len(body.List) != 1 {
		return false
	}

	ret, isReturn := body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return false
	}

	call, isCall := ast.Unparen(ret.Results[0]).(*ast.CallExpr)
	if !isCall {
		return false
	}

	arg, ok := c.nilPredicateArg(call)
	if !ok {
		return false
	}

	id, isIdent := ast.Unparen(call.Args[arg]).(*ast.Ident)

	return isIdent && id.Name == name
}

// nilPredicateArg reports the index of call's argument the callee's
// nilPredicate fact answers for, when the callee carries one and the call
// actually passes that argument.
func (c *checker) nilPredicateArg(call *ast.CallExpr) (int, bool) {
	fn, isFunc := c.calleeFunc(call)
	if !isFunc {
		return 0, false
	}

	var fact nilPredicate
	if !c.pass.ImportObjectFact(fn, &fact) {
		return 0, false
	}

	if fact.Arg < 0 || fact.Arg >= len(call.Args) {
		return 0, false
	}

	return fact.Arg, true
}

// callNilGuards reports the paths a call used as a condition proves non-nil: a
// callee carrying the nilPredicate fact answers true whenever its argument is
// nil, so the branch where the call is FALSE is the one with the argument
// proven, and the true branch proves nothing.
func (c *checker) callNilGuards(call *ast.CallExpr, sc scope) ([]string, []string) {
	arg, ok := c.nilPredicateArg(call)
	if !ok {
		return nil, nil
	}

	path, isPath := canonicalPath(call.Args[arg], sc.alias)
	if !isPath {
		return nil, nil
	}

	return nil, []string{path}
}

// calleeFunc resolves call's callee to the declared function or method it
// names, which is the object every fact is keyed on. A conversion, a builtin,
// or a call through a func value names no such object.
func (c *checker) calleeFunc(call *ast.CallExpr) (*types.Func, bool) {
	var ident *ast.Ident

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nil, false
	}

	fn, isFunc := c.resolve(ident).(*types.Func)

	return fn, isFunc
}
