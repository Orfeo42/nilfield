package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
)

// useKind names the way an expression's base is used at a field-access site: a
// field or method selected through it, or an explicit dereference of it. These
// are the only two shapes nilfield reports on: a nil pointer or nil interface
// being dereferenced. A nil map, slice, channel or func value is a different
// hazard class and out of scope.
type useKind int

const (
	useSelector useKind = iota // field or method selected through the base
	useStar                    // explicit *base
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
// selection or an explicit dereference — m["k"].n, s[0].n, v.(*inner).n. The one
// use that stays silent is a method call on the nil-origin base whose callee
// itself carries the nilSafeReceiver fact, the same exception checkKnownNil
// makes for a bare local whose own nil origin was recorded in scope.
func (c *checker) checkNilOriginBase(base ast.Expr, site useSite) {
	if site.kind != useSelector && site.kind != useStar {
		return
	}

	if !c.isNilOrigin(base) {
		return
	}

	if site.kind == useSelector && c.methodHasNilSafeReceiver(site.sel) {
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
//
// A use of the enclosing method's own receiver is the one path this probes
// during exportNilSafeReceiverFact's silent walk: it marks the receiver
// dereferenced unless the use is a call to another method that itself carries
// the nilSafeReceiver fact, which is what makes nil-safety transitive through a
// delegating method.
func (c *checker) checkPath(path string, t types.Type, site useSite, sc scope) {
	if sc.proven[path] {
		return
	}

	if c.receiver != "" && path == c.receiver {
		if site.kind == useSelector && c.methodHasNilSafeReceiver(site.sel) {
			return
		}

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
	switch use {
	case useStar:
		c.report(pos, "%s may be nil here", path)
	case useSelector:
		switch t.Underlying().(type) {
		case *types.Pointer, *types.Interface:
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
