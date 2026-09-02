package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
)

// computeWiredFields identifies, for every unexported named struct type
// declared in the package under analysis, which pointer- or interface-typed
// fields are always non-nil: every construction of the type anywhere in the
// package's non-test files proves the field non-nil, and no invalidating
// write, embedding, or reflect use appears anywhere in those files either. A
// dereference through such a field is not reported, the same way a proven
// path is not reported.
//
// The result is keyed by the field's own *types.Var, resolved through
// pass.TypesInfo.Selections at the use site, so two accesses through
// differently-named receivers (s.repo, svc.repo) are recognised as the same
// field. Test files are excluded from every check this computes, via the
// same isExcluded predicate the checker already uses to skip them at report
// time: a partial construction that only ever exists in a _test.go file must
// not poison the field for the production code that always wires it.
//
// Every fact this decision needs is gathered in one sweep over the package's
// files (collectWiringFacts), rather than a separate AST walk per type or
// per field: the cost is O(package AST size), not O(types x fields x AST).
func (c *checker) computeWiredFields() map[*types.Var]bool {
	wired := map[*types.Var]bool{}

	structTypes := c.unexportedStructTypes()
	if len(structTypes) == 0 {
		return wired
	}

	typeSet := make(map[*types.Named]bool, len(structTypes))
	for _, named := range structTypes {
		typeSet[named] = true
	}

	facts := c.collectWiringFacts(c.wiringFiles(), typeSet)
	embedded := c.typesEmbeddedInExported()

	for _, named := range structTypes {
		if facts.zeroValue[named] || embedded[named] || facts.unprovenCollection[named] || facts.reflectEscaped[named] {
			continue
		}

		sites := facts.literalSites[named]
		if len(sites) == 0 {
			continue
		}

		st := named.Underlying().(*types.Struct)

		for i := range st.NumFields() {
			f := st.Field(i)
			if isPointerOrInterface(f.Type()) && fieldWired(sites, st, f, facts) {
				wired[f] = true
			}
		}
	}

	return wired
}

// wiringFiles is the subset of the package's files this computation ever
// looks at: every file isExcluded already keeps out of reporting, test files
// included.
func (c *checker) wiringFiles() []*ast.File {
	var out []*ast.File

	for _, file := range c.pass.Files {
		if !c.isExcluded(c.pass.Fset.Position(file.Pos()).Filename) {
			out = append(out, file)
		}
	}

	return out
}

// isWiredFieldAccess reports whether base is itself the field-selector
// expression of an always-wired field: the AST node for `s.repo` in both
// `s.repo.List(ctx)` and `*s.repo`, since checkExpr hands that node to
// checkBase as the base of the outer selector or star.
func (c *checker) isWiredFieldAccess(base ast.Expr) bool {
	sel, isSelector := base.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}

	selection := c.pass.TypesInfo.Selections[sel]
	if selection == nil || selection.Kind() != types.FieldVal {
		return false
	}

	field, isVar := selection.Obj().(*types.Var)

	return isVar && c.wired[field]
}

// unexportedStructTypes lists the unexported named struct types declared at
// package scope: the only types a composite literal, a `new(...)`, or a
// method receiver of this type can appear for exclusively within this
// package.
func (c *checker) unexportedStructTypes() []*types.Named {
	var out []*types.Named

	scope := c.pass.Pkg.Scope()

	for _, name := range scope.Names() {
		tn, isTypeName := scope.Lookup(name).(*types.TypeName)
		if !isTypeName || tn.Exported() {
			continue
		}

		named, isNamed := tn.Type().(*types.Named)
		if !isNamed {
			continue
		}

		if _, isStruct := named.Underlying().(*types.Struct); isStruct {
			out = append(out, named)
		}
	}

	return out
}

// typesEmbeddedInExported returns the set of named types held as a value
// field, embedded or not, by some exported struct type declared in the
// package: external code can zero-construct that exported type and reach the
// held type's fields, and its promoted methods, still nil. Computed once
// over the package scope, rather than once per candidate type.
func (c *checker) typesEmbeddedInExported() map[*types.Named]bool {
	out := map[*types.Named]bool{}

	scope := c.pass.Pkg.Scope()

	for _, name := range scope.Names() {
		tn, isTypeName := scope.Lookup(name).(*types.TypeName)
		if !isTypeName || !tn.Exported() || c.isExcluded(c.pass.Fset.Position(tn.Pos()).Filename) {
			continue
		}

		named, isNamed := tn.Type().(*types.Named)
		if !isNamed {
			continue
		}

		st, isStruct := named.Underlying().(*types.Struct)
		if !isStruct {
			continue
		}

		for i := range st.NumFields() {
			if held := resolveNamed(st.Field(i).Type()); held != nil {
				out[held] = true
			}
		}
	}

	return out
}

func isPointerOrInterface(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	default:
		return false
	}
}

// resolveNamed extracts the *types.Named a type resolves to, unwrapping an
// alias, or nil if t is not a named type.
func resolveNamed(t types.Type) *types.Named {
	if t == nil {
		return nil
	}

	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}

	return named
}

// namedOrPointerElem extracts the *types.Named of t itself, or of the type t
// points to, whichever applies.
func namedOrPointerElem(t types.Type) *types.Named {
	if named := resolveNamed(t); named != nil {
		return named
	}

	ptr, isPointer := types.Unalias(t).(*types.Pointer)
	if !isPointer {
		return nil
	}

	return resolveNamed(ptr.Elem())
}

// collectionElemNamed extracts the named type held as the element of t, when
// t is a slice, array, or map: proving every element of an unbounded or
// sparsely-initialized container is out of scope, so any occurrence of a
// candidate type there disqualifies it.
func collectionElemNamed(t types.Type) *types.Named {
	if t == nil {
		return nil
	}

	switch u := types.Unalias(t).Underlying().(type) {
	case *types.Slice:
		return resolveNamed(u.Elem())
	case *types.Array:
		return resolveNamed(u.Elem())
	case *types.Map:
		return resolveNamed(u.Elem())
	default:
		return nil
	}
}

// literalSite is a single composite literal construction of a candidate
// type, with the provenness of every value it sets already decided at
// collection time, so deciding a field against it later is a map/slice
// lookup rather than a fresh AST walk.
type literalSite struct {
	keyedProven  map[string]bool
	positionalOK []bool
	positional   bool
}

// provesField reports whether this site proves field f of st non-nil: f is
// set, keyed or positional, to a value already proven non-nil.
func (s *literalSite) provesField(st *types.Struct, f *types.Var) bool {
	if proven, ok := s.keyedProven[f.Name()]; ok {
		return proven
	}

	if !s.positional {
		return false
	}

	idx := fieldIndex(st, f)
	if idx < 0 || idx >= len(s.positionalOK) {
		return false
	}

	return s.positionalOK[idx]
}

// wiringFacts is everything computeWiredFields needs about the package's
// non-test files, gathered in one sweep (collectWiringFacts) and then only
// looked up, keyed by type and field, while deciding each field.
type wiringFacts struct {
	zeroValue          map[*types.Named]bool
	unprovenCollection map[*types.Named]bool
	reflectEscaped     map[*types.Named]bool
	literalSites       map[*types.Named][]*literalSite
	fieldInvalidated   map[*types.Var]bool
}

// collectWiringFacts walks files once, tracking the innermost enclosing
// function or closure body as it goes, and records every fact
// computeWiredFields later decides fields from: zero-value constructions
// (`var x T` with no initializer, `new(T)`), an unproven container element,
// a reflect escape, every composite literal of a candidate type together
// with which keys it proves non-nil, and every write to a struct field that
// does not preserve a non-nil proof.
func (c *checker) collectWiringFacts(files []*ast.File, typeSet map[*types.Named]bool) *wiringFacts {
	facts := &wiringFacts{
		zeroValue:          map[*types.Named]bool{},
		unprovenCollection: map[*types.Named]bool{},
		reflectEscaped:     map[*types.Named]bool{},
		literalSites:       map[*types.Named][]*literalSite{},
		fieldInvalidated:   map[*types.Var]bool{},
	}

	for _, file := range files {
		c.collectWiringFactsFromFile(file, typeSet, facts)
	}

	return facts
}

func (c *checker) collectWiringFactsFromFile(file *ast.File, typeSet map[*types.Named]bool, facts *wiringFacts) {
	var bodyStack []*ast.BlockStmt
	var pushed []bool

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			last := len(pushed) - 1
			if pushed[last] {
				bodyStack = bodyStack[:len(bodyStack)-1]
			}

			pushed = pushed[:last]

			return true
		}

		body := funcBody(n)
		pushed = append(pushed, body != nil)

		if body != nil {
			bodyStack = append(bodyStack, body)
		}

		var currentBody *ast.BlockStmt
		if len(bodyStack) > 0 {
			currentBody = bodyStack[len(bodyStack)-1]
		}

		c.recordWiringNode(n, typeSet, currentBody, facts)

		return true
	})
}

// recordWiringNode updates facts with whatever n, seen inside currentBody,
// contributes: the several checks are independent, not mutually exclusive,
// since a single node (a *ast.CallExpr, for instance) can match more than
// one of them.
func (c *checker) recordWiringNode(n ast.Node, typeSet map[*types.Named]bool, currentBody *ast.BlockStmt, facts *wiringFacts) {
	switch node := n.(type) {
	case *ast.ValueSpec:
		c.recordZeroValueSpec(node, typeSet, facts)
	case *ast.CallExpr:
		c.recordNewCall(node, typeSet, facts)
		c.recordReflectCall(node, typeSet, facts)
	case *ast.CompositeLit:
		c.recordCompositeLit(node, typeSet, currentBody, facts)
	case *ast.AssignStmt:
		c.recordFieldWrites(node, currentBody, facts)
	}

	if expr, isExpr := n.(ast.Expr); isExpr {
		if named := collectionElemNamed(c.pass.TypesInfo.TypeOf(expr)); named != nil && typeSet[named] {
			facts.unprovenCollection[named] = true
		}
	}
}

func (c *checker) recordZeroValueSpec(node *ast.ValueSpec, typeSet map[*types.Named]bool, facts *wiringFacts) {
	if len(node.Values) != 0 || node.Type == nil {
		return
	}

	if named := resolveNamed(c.pass.TypesInfo.TypeOf(node.Type)); named != nil && typeSet[named] {
		facts.zeroValue[named] = true
	}
}

func (c *checker) recordNewCall(call *ast.CallExpr, typeSet map[*types.Named]bool, facts *wiringFacts) {
	id, isIdent := call.Fun.(*ast.Ident)
	if !isIdent || !isUniverse(c.resolve(id), "new") || len(call.Args) != 1 {
		return
	}

	if named := resolveNamed(c.pass.TypesInfo.TypeOf(call.Args[0])); named != nil && typeSet[named] {
		facts.zeroValue[named] = true
	}
}

// recordReflectCall reports, for a call into package "reflect", every
// candidate type mentioned by its arguments - directly, or as the argument
// to a nested call passed straight into this one - as escaped to reflect.
func (c *checker) recordReflectCall(call *ast.CallExpr, typeSet map[*types.Named]bool, facts *wiringFacts) {
	if !c.isReflectCall(call) {
		return
	}

	for _, arg := range call.Args {
		c.markReflectMention(arg, typeSet, facts)

		nested, isCall := arg.(*ast.CallExpr)
		if !isCall {
			continue
		}

		for _, nestedArg := range nested.Args {
			c.markReflectMention(nestedArg, typeSet, facts)
		}
	}
}

func (c *checker) markReflectMention(expr ast.Expr, typeSet map[*types.Named]bool, facts *wiringFacts) {
	if named := namedOrPointerElem(c.pass.TypesInfo.TypeOf(expr)); named != nil && typeSet[named] {
		facts.reflectEscaped[named] = true
	}
}

func (c *checker) isReflectCall(call *ast.CallExpr) bool {
	sel, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}

	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return false
	}

	pkgName, isPkgName := c.pass.TypesInfo.ObjectOf(id).(*types.PkgName)

	return isPkgName && pkgName.Imported().Path() == "reflect"
}

// recordCompositeLit records lit, whether written as a value (`T{...}`) or
// taken by address (`&T{...}`), as a construction site of its own type, with
// every value it sets already decided proven or not against currentBody.
func (c *checker) recordCompositeLit(lit *ast.CompositeLit, typeSet map[*types.Named]bool, currentBody *ast.BlockStmt, facts *wiringFacts) {
	named := resolveNamed(c.pass.TypesInfo.TypeOf(lit))
	if named == nil || !typeSet[named] {
		return
	}

	facts.literalSites[named] = append(facts.literalSites[named], c.newLiteralSite(lit, currentBody))
}

func (c *checker) newLiteralSite(lit *ast.CompositeLit, body *ast.BlockStmt) *literalSite {
	site := &literalSite{}

	if allPositional(lit) {
		site.positional = true
		site.positionalOK = make([]bool, len(lit.Elts))

		for i, elt := range lit.Elts {
			site.positionalOK[i] = c.isProvenValue(elt, lit, body)
		}

		return site
	}

	site.keyedProven = map[string]bool{}

	for _, elt := range lit.Elts {
		kv, isKeyValue := elt.(*ast.KeyValueExpr)
		if !isKeyValue {
			continue
		}

		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			continue
		}

		site.keyedProven[key.Name] = c.isProvenValue(kv.Value, lit, body)
	}

	return site
}

func allPositional(lit *ast.CompositeLit) bool {
	if len(lit.Elts) == 0 {
		return false
	}

	for _, elt := range lit.Elts {
		if _, isKeyValue := elt.(*ast.KeyValueExpr); isKeyValue {
			return false
		}
	}

	return true
}

func fieldIndex(st *types.Struct, target *types.Var) int {
	for i := range st.NumFields() {
		if st.Field(i) == target {
			return i
		}
	}

	return -1
}

// recordFieldWrites records, for every struct field assigned by assign, that
// the field is invalidated when the write does not preserve a non-nil
// proof - a plain assignment whose value is not proven, or a multi-assign
// whose shapes do not line up 1:1. A composite literal's own keyed value is
// never an *ast.AssignStmt, so this never doubles up with a literal site's
// own proof of the same field.
func (c *checker) recordFieldWrites(assign *ast.AssignStmt, currentBody *ast.BlockStmt, facts *wiringFacts) {
	for i, lhs := range assign.Lhs {
		sel, isSelector := lhs.(*ast.SelectorExpr)
		if !isSelector {
			continue
		}

		selection := c.pass.TypesInfo.Selections[sel]
		if selection == nil || selection.Kind() != types.FieldVal {
			continue
		}

		field, isVar := selection.Obj().(*types.Var)
		if !isVar {
			continue
		}

		if len(assign.Lhs) != len(assign.Rhs) || i >= len(assign.Rhs) {
			facts.fieldInvalidated[field] = true
			continue
		}

		if !c.isProvenValue(assign.Rhs[i], assign, currentBody) {
			facts.fieldInvalidated[field] = true
		}
	}
}

// fieldWired reports whether every one of sites proves f non-nil, and no
// write anywhere in the package clears or unproves it afterwards.
func fieldWired(sites []*literalSite, st *types.Struct, f *types.Var, facts *wiringFacts) bool {
	for _, site := range sites {
		if !site.provesField(st, f) {
			return false
		}
	}

	return !facts.fieldInvalidated[f]
}

// isProvenValue reports whether expr, appearing at node at inside body, is
// non-nil: a syntactic address-of or new(...), or a bare identifier whose
// own assignment earlier in the same function body is either of those, or a
// call whose paired error result was checked with a branch that exits. body
// is the innermost enclosing function or closure body containing at, or nil
// when at is not inside one - a package-level construction, for instance.
func (c *checker) isProvenValue(expr ast.Expr, at ast.Node, body *ast.BlockStmt) bool {
	if c.isNilIdent(expr) {
		return false
	}

	if c.isDefinitelyNonNil(expr) {
		return true
	}

	id, isIdent := expr.(*ast.Ident)
	if !isIdent {
		return false
	}

	return body != nil && c.identProvenInBlock(body, id.Name, at)
}

func funcBody(n ast.Node) *ast.BlockStmt {
	switch f := n.(type) {
	case *ast.FuncDecl:
		return f.Body
	case *ast.FuncLit:
		return f.Body
	default:
		return nil
	}
}

type identAssignEffect int

const (
	identUnchanged identAssignEffect = iota
	identDirectNonNil
	identReassigned
	identPendingCheck
)

// identProvenInBlock reports whether name is proven non-nil, by the time
// control reaches upTo, from body's own statement list: either a direct
// non-nil assignment, or a checked-error call (`name, err := f()` followed
// by `if err != nil { <exits> }`) with nothing in between that unproves it.
func (c *checker) identProvenInBlock(body *ast.BlockStmt, name string, upTo ast.Node) bool {
	proven := false
	pendingErr := ""

	for _, stmt := range body.List {
		if stmt.Pos() >= upTo.Pos() {
			break
		}

		if pendingErr != "" {
			if c.isCheckedErrorGuard(stmt, pendingErr) {
				proven = true
			}

			pendingErr = ""
		}

		assign, isAssign := stmt.(*ast.AssignStmt)
		if !isAssign {
			continue
		}

		switch effect, errName := c.identAssignState(assign, name); effect {
		case identDirectNonNil:
			proven = true
		case identReassigned:
			proven = false
		case identPendingCheck:
			pendingErr = errName
		case identUnchanged:
		}
	}

	return proven
}

// identAssignState reports the effect assign has on name: proven directly, a
// plain reassignment that drops any earlier proof, or a two-result call
// paired with a checked error, whose proof is pending on the guard that
// follows.
func (c *checker) identAssignState(assign *ast.AssignStmt, name string) (identAssignEffect, string) {
	idx := lhsIndex(assign, name)
	if idx < 0 {
		return identUnchanged, ""
	}

	if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
		if c.isDefinitelyNonNil(assign.Rhs[0]) {
			return identDirectNonNil, ""
		}

		return identReassigned, ""
	}

	if len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
		return identReassigned, ""
	}

	if _, isCall := assign.Rhs[0].(*ast.CallExpr); !isCall {
		return identReassigned, ""
	}

	errID, isIdent := assign.Lhs[1-idx].(*ast.Ident)
	if !isIdent || !isErrorType(c.pass.TypesInfo.TypeOf(errID)) {
		return identReassigned, ""
	}

	return identPendingCheck, errID.Name
}

func lhsIndex(assign *ast.AssignStmt, name string) int {
	for i, lhs := range assign.Lhs {
		if id, isIdent := lhs.(*ast.Ident); isIdent && id.Name == name {
			return i
		}
	}

	return -1
}

// isCheckedErrorGuard reports whether stmt is `if errName != nil { <exits> }`,
// with no init statement and no else branch.
func (c *checker) isCheckedErrorGuard(stmt ast.Stmt, errName string) bool {
	ifStmt, isIf := stmt.(*ast.IfStmt)
	if !isIf || ifStmt.Init != nil {
		return false
	}

	be, isBinary := ifStmt.Cond.(*ast.BinaryExpr)
	if !isBinary || be.Op != token.NEQ {
		return false
	}

	id, isIdent := be.X.(*ast.Ident)
	if !isIdent || id.Name != errName || !c.isNilIdent(be.Y) {
		return false
	}

	return c.blockExits(ifStmt.Body)
}
