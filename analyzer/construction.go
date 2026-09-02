package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// checkConstruction reports a wiring struct - one whose fields are all pointers
// or interfaces, the shape of a struct that exists only to hold wired-in
// dependencies - that body constructs with some of those fields left nil. It is
// independent of the flow walker: a left-out field is wrong at the construction
// site regardless of whether anything downstream ever dereferences it.
func (c *checker) checkConstruction(body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			c.checkCompositeLitConstruction(node)
		case *ast.AssignStmt:
			c.checkAssignConstruction(node, body)
		case *ast.DeclStmt:
			c.checkDeclConstruction(node, body)
		}

		return true
	})
}

// isWiringStruct reports whether t is a struct - directly, or through one level
// of pointer indirection - every field of which is pointer- or interface-typed.
func isWiringStruct(t types.Type) (*types.Struct, bool) {
	underlying := types.Unalias(t).Underlying()

	if ptr, isPtr := underlying.(*types.Pointer); isPtr {
		underlying = types.Unalias(ptr.Elem()).Underlying()
	}

	st, isStruct := underlying.(*types.Struct)
	if !isStruct || st.NumFields() == 0 {
		return nil, false
	}

	for i := range st.NumFields() {
		switch types.Unalias(st.Field(i).Type()).Underlying().(type) {
		case *types.Pointer, *types.Interface:
			continue
		default:
			return nil, false
		}
	}

	return st, true
}

// wiringTypeName returns the declared name of the named struct t denotes,
// unwrapping one level of pointer indirection first. An anonymous struct type
// has no name to report and is skipped.
func wiringTypeName(t types.Type) (string, bool) {
	u := types.Unalias(t)

	if ptr, isPtr := u.(*types.Pointer); isPtr {
		u = types.Unalias(ptr.Elem())
	}

	named, isNamed := u.(*types.Named)
	if !isNamed {
		return "", false
	}

	return named.Obj().Name(), true
}

// checkCompositeLitConstruction reports a non-empty composite literal of a
// wiring struct that leaves some field nil. An empty literal (T{}, &T{}) is
// silent by design: it hands on a zero value as-is, which is the existing
// "may be nil" story for whatever later dereferences it, not a construction
// mistake.
func (c *checker) checkCompositeLitConstruction(lit *ast.CompositeLit) {
	if len(lit.Elts) == 0 {
		return
	}

	t := c.pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return
	}

	st, ok := isWiringStruct(t)
	if !ok {
		return
	}

	name, ok := wiringTypeName(t)
	if !ok {
		return
	}

	missing := c.missingWiringFields(st, lit)
	if len(missing) == 0 {
		return
	}

	c.report(lit.Pos(), "%s is constructed with %s left nil", name, strings.Join(missing, ", "))
}

// missingWiringFields reports, in declaration order, the fields of st that lit
// leaves nil: a field absent from the literal entirely, or present with an
// explicit nil value. A positional (unkeyed) element is matched to the field at
// its own index.
func (c *checker) missingWiringFields(st *types.Struct, lit *ast.CompositeLit) []string {
	present := make([]bool, st.NumFields())

	for i, elt := range lit.Elts {
		kv, isKeyValue := elt.(*ast.KeyValueExpr)
		if !isKeyValue {
			if i < len(present) && !c.isNilIdent(elt) {
				present[i] = true
			}

			continue
		}

		idx, ok := c.keyFieldIndex(st, kv.Key)
		if !ok {
			continue
		}

		if !c.isNilIdent(kv.Value) {
			present[idx] = true
		}
	}

	var missing []string

	for i, ok := range present {
		if !ok {
			missing = append(missing, st.Field(i).Name())
		}
	}

	return missing
}

// keyFieldIndex resolves a composite literal key to the index of the field it
// names in st.
func (c *checker) keyFieldIndex(st *types.Struct, key ast.Expr) (int, bool) {
	id, isIdent := key.(*ast.Ident)
	if !isIdent {
		return 0, false
	}

	fieldVar, isVar := c.pass.TypesInfo.Uses[id].(*types.Var)
	if !isVar {
		return 0, false
	}

	for i := range st.NumFields() {
		if st.Field(i).Name() == fieldVar.Name() {
			return i, true
		}
	}

	return 0, false
}

// checkAssignConstruction handles `s := new(T)` and `s := T{}` / `s := &T{}`
// (with T{} empty), the two shapes that hand a wiring struct to a local with no
// composite literal left to inspect at the construction site itself.
func (c *checker) checkAssignConstruction(assign *ast.AssignStmt, body *ast.BlockStmt) {
	if assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return
	}

	ident, isIdent := assign.Lhs[0].(*ast.Ident)
	if !isIdent || ident.Name == "_" {
		return
	}

	c.checkConstructionCandidate(ident, ast.Unparen(assign.Rhs[0]), body)
}

// checkDeclConstruction handles the `var s = new(T)` / `var s = T{}` form of the
// same shape.
func (c *checker) checkDeclConstruction(decl *ast.DeclStmt, body *ast.BlockStmt) {
	gen, isGenDecl := decl.Decl.(*ast.GenDecl)
	if !isGenDecl || gen.Tok != token.VAR || len(gen.Specs) != 1 {
		return
	}

	spec, isValueSpec := gen.Specs[0].(*ast.ValueSpec)
	if !isValueSpec || len(spec.Names) != 1 || len(spec.Values) != 1 {
		return
	}

	ident := spec.Names[0]
	if ident.Name == "_" {
		return
	}

	c.checkConstructionCandidate(ident, ast.Unparen(spec.Values[0]), body)
}

// checkConstructionCandidate reports a local, defined as new(T) or an empty T{}
// literal, that has at least one wiring field written through it elsewhere in
// body but at least one other left untouched. A local with NO field ever
// written through it is silent: a zero value handed on as-is, unmodified, is
// the existing "may be nil" story for whatever downstream dereferences it, not
// a construction mistake - the policy this rule adds is only for a wiring that
// was clearly begun (some field set) and left incomplete.
func (c *checker) checkConstructionCandidate(ident *ast.Ident, rhs ast.Expr, body *ast.BlockStmt) {
	st, name, ok := c.constructionWiringType(rhs)
	if !ok {
		return
	}

	obj := c.pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return
	}

	written := c.fieldsWrittenThrough(body, obj)
	if len(written) == 0 {
		return
	}

	missing := unwrittenFields(st, written)
	if len(missing) == 0 {
		return
	}

	c.report(rhs.Pos(), "%s is constructed with %s left nil", name, strings.Join(missing, ", "))
}

// constructionWiringType reports the wiring struct and its name that rhs
// constructs, for the two recognised shapes: a call to the universe builtin
// new, or an empty composite literal.
func (c *checker) constructionWiringType(rhs ast.Expr) (*types.Struct, string, bool) {
	if call, isCall := rhs.(*ast.CallExpr); isCall {
		return c.newCallWiringType(call)
	}

	lit, ok := unwrapCompositeLit(rhs)
	if !ok || len(lit.Elts) != 0 {
		return nil, "", false
	}

	return c.wiringTypeOf(lit)
}

func (c *checker) newCallWiringType(call *ast.CallExpr) (*types.Struct, string, bool) {
	id, isIdent := call.Fun.(*ast.Ident)
	if !isIdent || !isUniverse(c.resolve(id), "new") || len(call.Args) != 1 {
		return nil, "", false
	}

	return c.wiringTypeOf(call.Args[0])
}

func (c *checker) wiringTypeOf(expr ast.Expr) (*types.Struct, string, bool) {
	t := c.pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return nil, "", false
	}

	st, ok := isWiringStruct(t)
	if !ok {
		return nil, "", false
	}

	name, ok := wiringTypeName(t)
	if !ok {
		return nil, "", false
	}

	return st, name, true
}

// fieldsWrittenThrough collects the field names written anywhere under body
// through a selector rooted at obj, nested blocks and closures included.
func (c *checker) fieldsWrittenThrough(body *ast.BlockStmt, obj types.Object) map[string]bool {
	written := map[string]bool{}

	ast.Inspect(body, func(n ast.Node) bool {
		assign, isAssign := n.(*ast.AssignStmt)
		if !isAssign {
			return true
		}

		for _, lhs := range assign.Lhs {
			sel, isSelector := lhs.(*ast.SelectorExpr)
			if !isSelector {
				continue
			}

			id, isIdent := sel.X.(*ast.Ident)
			if !isIdent || c.pass.TypesInfo.ObjectOf(id) != obj {
				continue
			}

			written[sel.Sel.Name] = true
		}

		return true
	})

	return written
}

// unwrittenFields reports, in declaration order, the fields of st not present
// in written.
func unwrittenFields(st *types.Struct, written map[string]bool) []string {
	var missing []string

	for i := range st.NumFields() {
		name := st.Field(i).Name()
		if !written[name] {
			missing = append(missing, name)
		}
	}

	return missing
}
