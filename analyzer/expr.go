package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
)

// useKind names the way an expression's base is used at a call/access site, which
// determines which nillable kinds are hazardous there: a call site cares about
// funcs, a map-write site cares about maps, and so on.
type useKind int

const (
	useSelector   useKind = iota // field or method selected through the base
	useStar                      // explicit *base
	useCall                      // base is a func VALUE being called
	useMapWrite                  // base is a map being written through an index
	useSliceIndex                // base is a slice being indexed (read)
	useChanSend
	useChanClose
)

// useSite bundles a use's position and kind with the selector expression it was
// found on, when there is one. The selector is what lets checkKnownNil tell a
// method call apart from a field access on a known-nil bare local, so it can look
// up whether the callee carries the nilSafeReceiver fact.
type useSite struct {
	pos  token.Pos
	kind useKind
	sel  *ast.SelectorExpr
}

func (c *checker) checkExpr(expr ast.Expr, sc scope) {
	if expr == nil {
		return
	}

	ast.Inspect(expr, func(n ast.Node) bool {
		if te, isExpr := n.(ast.Expr); isExpr && c.isTypeExpr(te) {
			// A type expression (e.g. the `*inner` in `v.(*inner)` or in a type
			// switch's `case *inner:`) has the same AST shape as a real pointer
			// dereference but names a type, not a value — nothing under it is a
			// hazard site.
			return false
		}

		switch e := n.(type) {
		case *ast.FuncLit:
			c.walkFuncLit(e, sc)

			return false
		case *ast.BinaryExpr:
			if e.Op == token.EQL || e.Op == token.NEQ {
				c.checkFuncNilComparison(e)
			}

			if e.Op != token.LAND && e.Op != token.LOR {
				return true
			}

			c.checkShortCircuit(e, sc)

			return false
		case *ast.SelectorExpr:
			c.checkBase(e.X, useSite{pos: e.Sel.Pos(), kind: useSelector, sel: e}, sc)
			c.checkPromotedField(e, sc)
		case *ast.StarExpr:
			c.checkBase(e.X, useSite{pos: e.Pos(), kind: useStar}, sc)
		case *ast.CallExpr:
			c.checkCall(e, sc)
		case *ast.IndexExpr:
			c.checkSliceIndex(e, sc)
		}

		return true
	})
}

// isTypeExpr reports whether e denotes a type rather than a value, which is true
// for a type used in a type assertion or a type switch case even though its AST
// shape (e.g. *ast.StarExpr, *ast.SelectorExpr) is identical to a value expression.
func (c *checker) isTypeExpr(e ast.Expr) bool {
	tv, ok := c.pass.TypesInfo.Types[e]

	return ok && tv.IsType()
}

// checkShortCircuit gives the right operand the knowledge the left one established,
// which is what makes `x != nil && x.f > 0` a guard rather than two dereferences.
func (c *checker) checkShortCircuit(e *ast.BinaryExpr, sc scope) {
	c.checkExpr(e.X, sc)

	whenTrue, whenFalse := c.nilGuards(e.X, sc)

	reached := whenTrue
	if e.Op == token.LOR {
		reached = whenFalse
	}

	c.checkExpr(e.Y, sc.with(reached))
}

// checkFuncNilComparison reports a `fn != nil` / `fn == nil` comparison whose
// non-nil operand is a declared function identifier, since a package-level func
// is never nil and the comparison always evaluates the same way.
func (c *checker) checkFuncNilComparison(e *ast.BinaryExpr) {
	var other ast.Expr

	switch {
	case c.isNilIdent(e.Y):
		other = e.X
	case c.isNilIdent(e.X):
		other = e.Y
	default:
		return
	}

	id, isIdent := ast.Unparen(other).(*ast.Ident)
	if !isIdent {
		return
	}

	if _, isFunc := c.resolve(id).(*types.Func); !isFunc {
		return
	}

	c.report(e.Pos(), "%s is never nil", id.Name)
}

// checkBase resolves base to a canonical path and hands it, together with the way
// it is being used, to checkPath. A base with no canonical path (a call result, an
// index expression, ...) is checked instead against isNilOrigin, since a map/slice
// index or a type assertion can itself be the nil value being dereferenced.
func (c *checker) checkBase(base ast.Expr, site useSite, sc scope) {
	t := c.pass.TypesInfo.TypeOf(base)
	if t == nil {
		return
	}

	if c.isPackageQualified(base) || c.isPackageLevelVar(base) {
		return
	}

	path, ok := canonicalPath(base, sc.alias)
	if !ok {
		c.checkNilOriginBase(base, site)

		return
	}

	c.checkPath(path, t, site, sc)
}

// checkNilOriginBase reports a base with no canonical path whose own expression
// shape is a nil origin (a map/slice index, a single-form type assertion), for the
// use kinds where the base itself being nil is the hazard: a field/method
// selection, an explicit dereference, or a call through it — m["k"].n, s[0].n,
// v.(*inner).n, m["k"]().
func (c *checker) checkNilOriginBase(base ast.Expr, site useSite) {
	if site.kind != useSelector && site.kind != useStar && site.kind != useCall {
		return
	}

	if !c.isNilOrigin(base) {
		return
	}

	c.report(site.pos, "%s may be nil here", types.ExprString(base))
}

// checkPath decides whether the given use of an already-proven-or-not path is
// hazardous. A field or star path is reported for the uses that dereference or
// call through it; a bare local or parameter is reported only for the two uses
// that are unambiguously the caller's own mistake (an explicit `*p` and a method
// call on an unchecked `error`) — everything else bare stays silent, because this
// analyzer is about fields reached through a struct, which is where the guard is
// easy to forget, not about auditing every parameter in the program.
func (c *checker) checkPath(path string, t types.Type, site useSite, sc scope) {
	if sc.proven[path] {
		return
	}

	if c.receiver != "" && path == c.receiver {
		c.receiverDeref = true

		return
	}

	if state, known := sc.nilable[path]; known {
		c.checkKnownNil(path, state, site)

		return
	}

	if isFieldPath(path) || isStarPath(path) {
		c.checkFieldOrStarUse(path, t, site.pos, site.kind)

		return
	}

	c.checkBarePathUse(path, t, site.pos, site.kind)
}

// checkKnownNil reports a use of a bare local whose own nil origin was recorded
// earlier in the same function, at the state it was last recorded in. The one use
// that stays silent is a method call, through a pointer receiver, on a callee that
// itself proves it tolerates a nil receiver (the nilSafeReceiver fact) — every
// other use kind, field or method alike, dereferences or blocks on the value.
func (c *checker) checkKnownNil(path string, state nilState, site useSite) {
	if site.kind == useSelector && c.methodHasNilSafeReceiver(site.sel) {
		return
	}

	c.report(site.pos, "%s %s", path, state.message())
}

// methodHasNilSafeReceiver reports whether sel resolves to a method value whose
// declaration carries the nilSafeReceiver fact.
func (c *checker) methodHasNilSafeReceiver(sel *ast.SelectorExpr) bool {
	if sel == nil {
		return false
	}

	selection := c.pass.TypesInfo.Selections[sel]
	if selection == nil || selection.Kind() != types.MethodVal {
		return false
	}

	fn, isFunc := selection.Obj().(*types.Func)
	if !isFunc {
		return false
	}

	var fact nilSafeReceiver

	return c.pass.ImportObjectFact(fn, &fact)
}

func (c *checker) checkFieldOrStarUse(path string, t types.Type, pos token.Pos, use useKind) {
	underlying := t.Underlying()

	switch use {
	case useStar:
		c.report(pos, "%s may be nil here", path)
	case useSelector:
		switch underlying.(type) {
		case *types.Pointer, *types.Interface:
			c.report(pos, "%s may be nil here", path)
		}
	case useCall:
		if _, isSignature := underlying.(*types.Signature); isSignature {
			c.report(pos, "%s may be nil here", path)
		}
	case useMapWrite:
		if _, isMap := underlying.(*types.Map); isMap {
			c.report(pos, "%s may be nil here", path)
		}
	}
}

func (c *checker) checkBarePathUse(path string, t types.Type, pos token.Pos, use useKind) {
	switch {
	case use == useStar:
		c.report(pos, "%s may be nil here", path)
	case use == useSelector && isErrorType(t):
		c.report(pos, "%s may be nil here", path)
	}
}

// report emits a diagnostic unless the checker is silent or the file is excluded.
func (c *checker) report(pos token.Pos, format string, args ...any) {
	if c.silent {
		return
	}

	if c.isExcluded(c.pass.Fset.Position(pos).Filename) {
		return
	}

	c.pass.Reportf(pos, format, args...)
}

// checkPromotedField reports the intermediate pointer-typed embedded fields a
// promoted selector reaches through, e.g. `o.e` for `outer{ *embedded }` reports
// `o.embedded` when it is not proven, since the promotion hides that indirection.
func (c *checker) checkPromotedField(e *ast.SelectorExpr, sc scope) {
	sel := c.pass.TypesInfo.Selections[e]
	if sel == nil || len(sel.Index()) <= 1 {
		return
	}

	basePath, ok := canonicalPath(e.X, sc.alias)
	if !ok || c.isPackageQualified(e.X) {
		return
	}

	t := sel.Recv()
	index := sel.Index()

	for _, idx := range index[:len(index)-1] {
		t = derefType(t)

		st, isStruct := t.Underlying().(*types.Struct)
		if !isStruct {
			return
		}

		f := st.Field(idx)
		basePath += "." + f.Name()

		if _, isPtr := types.Unalias(f.Type()).Underlying().(*types.Pointer); isPtr {
			c.checkPath(basePath, f.Type(), useSite{pos: e.Sel.Pos(), kind: useSelector}, sc)
		}

		t = f.Type()
	}
}

// derefType strips one layer of pointer indirection off t, so a struct type's own
// fields can be inspected regardless of whether it was reached by value or by
// pointer.
func derefType(t types.Type) types.Type {
	u := types.Unalias(t)

	if ptr, isPtr := u.Underlying().(*types.Pointer); isPtr {
		return ptr.Elem()
	}

	return u
}

// checkCall reports a call through a func VALUE (a variable or a struct field of
// function type), as opposed to a declared function, a method, or a conversion,
// none of which are a "use" in the sense checkPath cares about.
func (c *checker) checkCall(e *ast.CallExpr, sc scope) {
	fun := ast.Unparen(e.Fun)

	if id, isIdent := fun.(*ast.Ident); isIdent && isUniverse(c.resolve(id), "close") && len(e.Args) == 1 {
		c.checkBase(e.Args[0], useSite{pos: e.Pos(), kind: useChanClose}, sc)

		return
	}

	switch callee := fun.(type) {
	case *ast.Ident:
		if _, isVar := c.resolve(callee).(*types.Var); isVar {
			c.checkBase(fun, useSite{pos: e.Lparen, kind: useCall}, sc)
		}
	case *ast.SelectorExpr:
		c.checkSelectorCall(fun, callee, e.Lparen, sc)
	}
}

// checkSelectorCall handles the two shapes of a selector callee that is a func
// value rather than a method: a field of function type (Selections carries a
// FieldVal kind), and a package-qualified function variable (no Selection entry
// at all, since it is not a selection into a type).
func (c *checker) checkSelectorCall(fun ast.Expr, sel *ast.SelectorExpr, pos token.Pos, sc scope) {
	selection := c.pass.TypesInfo.Selections[sel]
	if selection != nil {
		if selection.Kind() == types.FieldVal {
			c.checkBase(fun, useSite{pos: pos, kind: useCall}, sc)
		}

		return
	}

	if _, isVar := c.pass.TypesInfo.ObjectOf(sel.Sel).(*types.Var); isVar {
		c.checkBase(fun, useSite{pos: pos, kind: useCall}, sc)
	}
}

func (c *checker) checkSliceIndex(e *ast.IndexExpr, sc scope) {
	t := c.pass.TypesInfo.TypeOf(e.X)
	if t == nil {
		return
	}

	if _, isSlice := t.Underlying().(*types.Slice); !isSlice {
		return
	}

	c.checkBase(e.X, useSite{pos: e.Lbrack, kind: useSliceIndex}, sc)
}
