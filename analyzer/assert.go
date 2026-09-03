package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
)

// assertedArg reports the index of call's argument that is proven true when call
// does not panic, if call resolves to a function carrying the assertHelper fact.
func (c *checker) assertedArg(call *ast.CallExpr) (int, bool) {
	fn, isFunc := c.calleeFunc(call)
	if !isFunc {
		return 0, false
	}

	var fact assertHelper
	if !c.pass.ImportObjectFact(fn, &fact) {
		return 0, false
	}

	if fact.Arg >= len(call.Args) {
		return 0, false
	}

	return fact.Arg, true
}

// exportAssertHelperFact records, for fd, that its first bool parameter proven
// asserted by its body is the argument callers must hold true to avoid a panic.
// It returns whether it exported a fact that was not already recorded.
func (c *checker) exportAssertHelperFact(fd *ast.FuncDecl) bool {
	if fd.Body == nil || fd.Type.Params == nil {
		return false
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func)
	if !isFunc {
		return false
	}

	var scratch assertHelper
	if c.pass.ImportObjectFact(fn, &scratch) {
		return false
	}

	index := 0

	for _, field := range fd.Type.Params.List {
		for _, name := range field.Names {
			obj := c.pass.TypesInfo.ObjectOf(name)

			if isBoolParam(obj) && c.paramIsAsserted(fd.Body, obj) {
				c.pass.ExportObjectFact(fn, &assertHelper{Arg: index})

				return true
			}

			index++
		}
	}

	return false
}

func isBoolParam(obj types.Object) bool {
	v, isVar := obj.(*types.Var)

	return isVar && types.Identical(v.Type(), types.Typ[types.Bool])
}

// paramIsAsserted scans body's top-level statements for one of the recognised
// forms that proves body panics or never returns normally unless paramObj holds.
func (c *checker) paramIsAsserted(body *ast.BlockStmt, paramObj types.Object) bool {
	stmts := body.List

	for i := 0; i < len(stmts); i++ {
		stmt := stmts[i]

		if c.assertFormIf(stmt, paramObj) {
			return true
		}

		if c.assertFormIfReturnThenPanic(stmts, i, paramObj) {
			return true
		}

		if c.assertFormDelegation(stmt, paramObj) {
			return true
		}

		switch stmt.(type) {
		case *ast.ReturnStmt, *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.RangeStmt, *ast.SelectStmt:
			return false
		}
	}

	return false
}

// assertFormIf recognises `if <cond> { <block ending in a call that never
// returns> }` where cond proves paramObj true.
func (c *checker) assertFormIf(stmt ast.Stmt, paramObj types.Object) bool {
	ifStmt, isIf := stmt.(*ast.IfStmt)
	if !isIf || ifStmt.Init != nil || ifStmt.Else != nil {
		return false
	}

	if !c.condAssertsParam(ifStmt.Cond, paramObj) {
		return false
	}

	return c.blockEndsInNeverReturn(ifStmt.Body)
}

// assertFormIfReturnThenPanic recognises `if p { return }` followed by a tail
// that never completes normally.
func (c *checker) assertFormIfReturnThenPanic(stmts []ast.Stmt, i int, paramObj types.Object) bool {
	ifStmt, isIf := stmts[i].(*ast.IfStmt)
	if !isIf || ifStmt.Init != nil || ifStmt.Else != nil {
		return false
	}

	if !c.identIsParam(ifStmt.Cond, paramObj) {
		return false
	}

	if len(ifStmt.Body.List) != 1 {
		return false
	}

	ret, isReturn := ifStmt.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 0 {
		return false
	}

	return c.tailNeverReturns(stmts[i+1:])
}

// tailNeverReturns reports whether the statements after an `if p { return }`
// cannot complete normally: they end in a call that never returns and hold no
// return of their own, so work done between the guard and the panic (building
// the value to panic with) does not change what the guard proves.
func (c *checker) tailNeverReturns(stmts []ast.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}

	block := &ast.BlockStmt{List: stmts}

	return c.blockEndsInNeverReturn(block) && !containsReturn(block)
}

// assertFormDelegation recognises a call to another assert helper, passed
// paramObj at the position that helper's own fact asserts.
func (c *checker) assertFormDelegation(stmt ast.Stmt, paramObj types.Object) bool {
	exprStmt, isExprStmt := stmt.(*ast.ExprStmt)
	if !isExprStmt {
		return false
	}

	call, isCall := exprStmt.X.(*ast.CallExpr)
	if !isCall {
		return false
	}

	var ident *ast.Ident

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return false
	}

	fn, isFunc := c.resolve(ident).(*types.Func)
	if !isFunc {
		return false
	}

	var fact assertHelper
	if !c.pass.ImportObjectFact(fn, &fact) {
		return false
	}

	sig := fn.Signature()
	if sig.Variadic() && fact.Arg >= sig.Params().Len()-1 {
		return false
	}

	if fact.Arg >= len(call.Args) {
		return false
	}

	return c.identIsParam(call.Args[fact.Arg], paramObj)
}

// blockEndsInNeverReturn reports whether block's last statement is a call proven
// never to return normally.
func (c *checker) blockEndsInNeverReturn(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}

	last, isExprStmt := block.List[len(block.List)-1].(*ast.ExprStmt)
	if !isExprStmt {
		return false
	}

	call, isCall := last.X.(*ast.CallExpr)

	return isCall && c.callNeverReturns(call)
}

// condAssertsParam reports whether cond is `!p` or `p == false`/`false == p`,
// where p resolves to paramObj.
func (c *checker) condAssertsParam(cond ast.Expr, paramObj types.Object) bool {
	cond = ast.Unparen(cond)

	if unary, isUnary := cond.(*ast.UnaryExpr); isUnary && unary.Op == token.NOT {
		return c.identIsParam(unary.X, paramObj)
	}

	binary, isBinary := cond.(*ast.BinaryExpr)
	if !isBinary || binary.Op != token.EQL {
		return false
	}

	if c.identIsParam(binary.X, paramObj) && c.isUniverseFalse(binary.Y) {
		return true
	}

	return c.identIsParam(binary.Y, paramObj) && c.isUniverseFalse(binary.X)
}

func (c *checker) isUniverseFalse(expr ast.Expr) bool {
	id, isIdent := ast.Unparen(expr).(*ast.Ident)

	return isIdent && isUniverse(c.resolve(id), "false")
}

func (c *checker) identIsParam(expr ast.Expr, paramObj types.Object) bool {
	id, isIdent := ast.Unparen(expr).(*ast.Ident)

	return isIdent && c.pass.TypesInfo.ObjectOf(id) == paramObj
}

// exportNeverReturnsFact records that fd's body cannot complete normally. It
// returns whether it exported a fact that was not already recorded.
//
// The trailing statement proves the fall-through path never returns, but an
// earlier guard elsewhere in the body can still return normally, so a body
// containing any return statement is disqualified.
func (c *checker) exportNeverReturnsFact(fd *ast.FuncDecl) bool {
	if fd.Body == nil || !c.blockEndsInNeverReturn(fd.Body) || containsReturn(fd.Body) {
		return false
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func)
	if !isFunc {
		return false
	}

	var scratch neverReturns
	if c.pass.ImportObjectFact(fn, &scratch) {
		return false
	}

	c.pass.ExportObjectFact(fn, &neverReturns{})

	return true
}

// containsReturn reports whether a return statement appears anywhere in body,
// nested func literals excluded.
func containsReturn(body *ast.BlockStmt) bool {
	found := false

	inspectSkippingFuncLits(body, func(n ast.Node) {
		if _, isReturn := n.(*ast.ReturnStmt); isReturn {
			found = true
		}
	})

	return found
}
