package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
)

func (c *checker) blockExits(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}

	switch last := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return last.Tok == token.BREAK || last.Tok == token.CONTINUE || last.Tok == token.GOTO
	case *ast.ExprStmt:
		call, isCall := last.X.(*ast.CallExpr)
		if !isCall {
			return false
		}

		return c.callNeverReturns(call)
	default:
		return false
	}
}

// callNeverReturns reports whether call is a call to the builtin panic, or to a
// function proven never to return normally via the neverReturns fact.
func (c *checker) callNeverReturns(call *ast.CallExpr) bool {
	var ident *ast.Ident

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return false
	}

	obj := c.resolve(ident)

	if _, isBuiltin := obj.(*types.Builtin); isBuiltin {
		return isUniverse(obj, "panic")
	}

	fn, isFunc := obj.(*types.Func)
	if !isFunc || c.pass == nil {
		return false
	}

	var fact neverReturns

	return c.pass.ImportObjectFact(fn, &fact)
}

// nilGuards decomposes a condition into the paths proven non-nil when it is true and
// the ones proven non-nil when it is false.
func (c *checker) nilGuards(cond ast.Expr, sc scope) ([]string, []string) {
	switch e := cond.(type) {
	case *ast.ParenExpr:
		return c.nilGuards(e.X, sc)
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return nil, nil
		}

		inverted, straight := c.nilGuards(e.X, sc)

		return straight, inverted
	case *ast.BinaryExpr:
		return c.binaryNilGuards(e, sc)
	default:
		return nil, nil
	}
}

func (c *checker) binaryNilGuards(e *ast.BinaryExpr, sc scope) ([]string, []string) {
	switch e.Op {
	case token.LAND:
		leftTrue, _ := c.nilGuards(e.X, sc)
		rightTrue, _ := c.nilGuards(e.Y, sc)

		return append(leftTrue, rightTrue...), nil
	case token.LOR:
		_, leftFalse := c.nilGuards(e.X, sc)
		_, rightFalse := c.nilGuards(e.Y, sc)

		return nil, append(leftFalse, rightFalse...)
	case token.NEQ, token.EQL:
		path, ok := c.nilComparisonPath(e, sc.alias)
		if !ok {
			return nil, nil
		}

		// A nil error variable carries the postcondition of the validator call it
		// came from, so the branch that survives `err != nil` inherits its proofs.
		implied := slices.Clone(sc.errProof[path])

		if e.Op == token.NEQ {
			return []string{path}, implied
		}

		return implied, []string{path}
	default:
		return nil, nil
	}
}

func (c *checker) nilComparisonPath(be *ast.BinaryExpr, alias map[string]string) (string, bool) {
	var target ast.Expr

	switch {
	case c.isNilIdent(be.Y):
		target = be.X
	case c.isNilIdent(be.X):
		target = be.Y
	default:
		return "", false
	}

	return canonicalPath(target, alias)
}

func (c *checker) isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)

	return ok && isUniverse(c.resolve(id), "nil")
}

// clauseGuards reports the paths a single switch case expression proves non-nil. A
// tagless switch clause is itself a boolean condition, so it defers to nilGuards. A
// tagged switch clause can only prove something about the tag: matching `case nil`
// proves the tag path is nil, i.e. it proves nothing when true and proves the tag
// path only in the branch where this clause did NOT match.
func (c *checker) clauseGuards(tag ast.Expr, expr ast.Expr, sc scope) ([]string, []string) {
	if tag == nil {
		return c.nilGuards(expr, sc)
	}

	if !c.isNilIdent(expr) {
		return nil, nil
	}

	tagPath, ok := canonicalPath(tag, sc.alias)
	if !ok {
		return nil, nil
	}

	return nil, []string{tagPath}
}

// clauseListGuards combines the guards of a case clause's expression list. A single
// expression's own guards apply directly. A comma-separated list is an OR: none of
// them matching is what proves each expression's whenFalse paths, while a match on
// any one of several alternatives proves nothing about which alternative it was.
func (c *checker) clauseListGuards(tag ast.Expr, list []ast.Expr, sc scope) ([]string, []string) {
	if len(list) == 1 {
		return c.clauseGuards(tag, list[0], sc)
	}

	var whenFalse []string

	for _, expr := range list {
		_, exprFalse := c.clauseGuards(tag, expr, sc)
		whenFalse = append(whenFalse, exprFalse...)
	}

	return nil, whenFalse
}

func (c *checker) isDefinitelyNonNil(expr ast.Expr) bool {
	unary, isUnary := expr.(*ast.UnaryExpr)
	if isUnary && unary.Op == token.AND {
		return true
	}

	call, isCall := expr.(*ast.CallExpr)
	if !isCall {
		return false
	}

	id, isIdent := call.Fun.(*ast.Ident)

	return isIdent && isUniverse(c.resolve(id), "new")
}

// provenFromLiteral extends the proof for path to the fields a composite literal sets
// directly, recursing into nested literals so `&T{F: &U{G: &V{}}}` also proves path.F
// and path.F.G.
func (c *checker) provenFromLiteral(path string, expr ast.Expr) []string {
	lit, ok := unwrapCompositeLit(expr)
	if !ok {
		return nil
	}

	var paths []string

	for _, elt := range lit.Elts {
		kv, isKeyValue := elt.(*ast.KeyValueExpr)
		if !isKeyValue {
			continue
		}

		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent || !c.isDefinitelyNonNil(kv.Value) {
			continue
		}

		fieldPath := path + "." + key.Name
		paths = append(paths, fieldPath)
		paths = append(paths, c.provenFromLiteral(fieldPath, kv.Value)...)
	}

	return paths
}

func unwrapCompositeLit(expr ast.Expr) (*ast.CompositeLit, bool) {
	if lit, ok := expr.(*ast.CompositeLit); ok {
		return lit, true
	}

	unary, isUnary := expr.(*ast.UnaryExpr)
	if !isUnary || unary.Op != token.AND {
		return nil, false
	}

	lit, ok := unary.X.(*ast.CompositeLit)

	return lit, ok
}
