package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// checkReturn reports the two return-time hazards a function's own body can
// prove about itself: returning a nil value alongside a nil error, which
// leaves the caller unable to tell success from a not-found or empty result,
// and returning after an error the body itself checked was never propagated.
func (c *checker) checkReturn(ret *ast.ReturnStmt, sc scope) {
	if c.sig == nil {
		return
	}

	results := c.sig.Results()
	if len(ret.Results) != results.Len() {
		return
	}

	errIdx := lastErrorResultIndex(results)
	if errIdx < 0 {
		return
	}

	if !c.isNilIdent(ast.Unparen(ret.Results[errIdx])) {
		return
	}

	c.checkNilValueWithNilError(ret, results, errIdx)
	c.checkSwallowedError(ret, sc)
}

// checkNilValueWithNilError reports a return whose error result is nil while
// another result is a literal nil of a kind that is not an ordinary empty
// value (see isNilResultKind). One diagnostic is emitted per return, however
// many such results it carries.
func (c *checker) checkNilValueWithNilError(ret *ast.ReturnStmt, results *types.Tuple, errIdx int) {
	for j, expr := range ret.Results {
		if j == errIdx {
			continue
		}

		rt := results.At(j).Type()
		if isErrorType(rt) {
			continue
		}

		if !c.isNilIdent(ast.Unparen(expr)) {
			continue
		}

		if isNilResultKind(rt) {
			c.report(ret.Pos(), "nil value returned with a nil error")

			return
		}
	}
}

// checkSwallowedError reports a return, reached only through a branch that
// already checked an error (see errorGuards), that lets the checked error
// vanish instead of propagating it.
func (c *checker) checkSwallowedError(ret *ast.ReturnStmt, sc scope) {
	if len(sc.checkedErrors) == 0 {
		return
	}

	names := make([]string, 0, len(sc.checkedErrors))
	for name := range sc.checkedErrors {
		names = append(names, name)
	}

	slices.Sort(names)

	c.report(ret.Pos(), "%s is discarded", strings.Join(names, ", "))
}

// errorGuards reports the paths compared against nil in cond whose operand is
// error-typed, decomposing parens, a leading `!`, and `&&`/`||` chains the same
// way nilGuards does. The `if err := f(); err != nil { ... }` init form works
// because walkIf walks Init in the same scope before calling this.
func (c *checker) errorGuards(cond ast.Expr, sc scope) ([]string, []string) {
	switch e := cond.(type) {
	case *ast.ParenExpr:
		return c.errorGuards(e.X, sc)
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return nil, nil
		}

		inverted, straight := c.errorGuards(e.X, sc)

		return straight, inverted
	case *ast.BinaryExpr:
		return c.binaryErrorGuards(e, sc)
	default:
		return nil, nil
	}
}

func (c *checker) binaryErrorGuards(e *ast.BinaryExpr, sc scope) ([]string, []string) {
	switch e.Op {
	case token.LAND:
		leftTrue, _ := c.errorGuards(e.X, sc)
		rightTrue, _ := c.errorGuards(e.Y, sc)

		return append(leftTrue, rightTrue...), nil
	case token.LOR:
		_, leftFalse := c.errorGuards(e.X, sc)
		_, rightFalse := c.errorGuards(e.Y, sc)

		return nil, append(leftFalse, rightFalse...)
	case token.NEQ, token.EQL:
		path, ok := c.errorNilComparisonPath(e, sc.alias)
		if !ok {
			return nil, nil
		}

		if e.Op == token.NEQ {
			return []string{path}, nil
		}

		return nil, []string{path}
	default:
		return nil, nil
	}
}

// errorNilComparisonPath is nilComparisonPath narrowed to an error-typed
// operand, which is what keeps a `p != nil` pointer guard from being read as an
// error check.
func (c *checker) errorNilComparisonPath(be *ast.BinaryExpr, alias map[string]string) (string, bool) {
	var target ast.Expr

	switch {
	case c.isNilIdent(be.Y):
		target = be.X
	case c.isNilIdent(be.X):
		target = be.Y
	default:
		return "", false
	}

	t := c.pass.TypesInfo.TypeOf(target)
	if t == nil || !isErrorType(t) {
		return "", false
	}

	return canonicalPath(target, alias)
}

// declSignature is fd's own declared signature, used to seed c.sig before
// walking its body so its return statements can be checked against it.
func (c *checker) declSignature(fd *ast.FuncDecl) *types.Signature {
	fn, isFunc := c.pass.TypesInfo.Defs[fd.Name].(*types.Func)
	if !isFunc {
		return nil
	}

	return fn.Signature()
}
