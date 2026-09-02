// nilfield reports method calls and field accesses made through a pointer-typed
// struct field that no nil guard dominates, the shape that turns a missing check
// into a nil-pointer panic at runtime.
//
// A path is proven non-nil by an `x != nil` condition (including inside `&&`/`||`
// chains and behind `!`), by an early return/panic in the opposite branch, by an
// assignment of an address or `new(...)`, by an Assert helper from the package
// named in -assert-package, or by a checked call to a validator method: a method
// returning a single `error` whose body rejects a nil receiver field before any
// path that can return a nil error. Writing to the field again drops the proof.
//
// The validator rule is derived from the callee's own guards and travels between
// packages as an analysis fact, so nothing has to be annotated at the call site.
// The one assumption it makes is that the expression a guard returns - a call, an
// address, or a package-level sentinel - is really non-nil; a guard returning a
// local variable or `nil` proves nothing. A validator that, outside a return
// statement, calls a pointer-receiver method through its receiver or hands a
// pointer, slice, map, channel, func or interface rooted at the receiver to
// another call exports no fact, since that call could clear the field.
//
// Only fields reached through a struct are reported: a bare local or parameter is
// out of scope, because the guard there is hard to miss.
//
// Every finding is reported; the gate is zero findings, there is no baseline.
//
// Install and run it from the root of the module being checked:
//
//	go install github.com/Orfeo42/nilfield@latest
//	nilfield ./...
//	nilfield -assert-package=example.com/app/utility -exclude-paths=internal/dao/ ./...
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/singlechecker"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "nilfield",
	Doc:      "reports method calls and field accesses on pointer-typed struct fields with no local nil guard",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
	FactTypes: []analysis.Fact{
		(*validatedFields)(nil),
	},
}

// validatedFields records, for one validator method, the receiver fields its body
// proves non-nil whenever it returns a nil error. It crosses package boundaries as
// an object fact, which is what lets a caller trust a validator it did not declare.
type validatedFields struct {
	Fields []string
}

func (*validatedFields) AFact() {}

func (f *validatedFields) String() string {
	return "validates " + strings.Join(f.Fields, ", ")
}

var (
	assertPackageFlag string
	excludePathsFlag  string
)

func init() {
	Analyzer.Flags.StringVar(&assertPackageFlag, "assert-package", "",
		"import path of the package whose Assert/AssertWithCode helpers prove their condition; empty disables assert-based proofs")
	Analyzer.Flags.StringVar(&excludePathsFlag, "exclude-paths", "",
		"comma-separated path fragments; a file whose slash-normalized path contains one is never reported")
}

func main() {
	singlechecker.Main(Analyzer)
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	c := &checker{
		pass:          pass,
		assertPackage: assertPackageFlag,
		excludePaths:  splitFragments(excludePathsFlag),
		walked:        map[*ast.FuncLit]bool{},
	}

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		c.exportValidatorFact(n.(*ast.FuncDecl))
	})

	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		var body *ast.BlockStmt

		switch f := n.(type) {
		case *ast.FuncDecl:
			body = f.Body
		case *ast.FuncLit:
			if c.walked[f] {
				return
			}

			body = f.Body
		}

		if body == nil {
			return
		}

		c.walk(body.List, newScope())
	})

	return nil, nil
}

func splitFragments(raw string) []string {
	var out []string

	for part := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		out = append(out, filepath.ToSlash(trimmed))
	}

	return out
}

type checker struct {
	pass          *analysis.Pass
	assertPackage string
	excludePaths  []string
	walked        map[*ast.FuncLit]bool
}

// scope carries the flow-sensitive state of one branch: which canonical paths are
// known non-nil, and which local names are known to alias which canonical path.
// errProof additionally maps a local error variable to the paths that are non-nil
// on the branch where that variable is nil, which is how a checked validator call
// hands its postcondition to the surrounding `if err != nil` guard.
type scope struct {
	proven   map[string]bool
	alias    map[string]string
	errProof map[string][]string
}

func newScope() scope {
	return scope{
		proven:   map[string]bool{},
		alias:    map[string]string{},
		errProof: map[string][]string{},
	}
}

func (s scope) clone() scope {
	proven := make(map[string]bool, len(s.proven))
	maps.Copy(proven, s.proven)

	alias := make(map[string]string, len(s.alias))
	maps.Copy(alias, s.alias)

	errProof := make(map[string][]string, len(s.errProof))
	maps.Copy(errProof, s.errProof)

	return scope{proven: proven, alias: alias, errProof: errProof}
}

func (s scope) with(paths []string) scope {
	out := s.clone()
	for _, path := range paths {
		out.proven[path] = true
	}

	return out
}

// invalidate drops every proof and alias that the write to path could have falsified:
// the path itself, everything reachable through it, and any local aliasing either.
func (s scope) invalidate(path string) {
	delete(s.proven, path)

	prefix := path + "."

	for k := range s.proven {
		if strings.HasPrefix(k, prefix) {
			delete(s.proven, k)
		}
	}

	for name, target := range s.alias {
		if target == path || strings.HasPrefix(target, prefix) {
			delete(s.alias, name)
		}
	}

	for name, paths := range s.errProof {
		kept := make([]string, 0, len(paths))

		for _, p := range paths {
			if p == path || strings.HasPrefix(p, prefix) {
				continue
			}

			kept = append(kept, p)
		}

		if len(kept) == 0 {
			delete(s.errProof, name)

			continue
		}

		s.errProof[name] = kept
	}
}

func (c *checker) walk(stmts []ast.Stmt, sc scope) {
	for _, stmt := range stmts {
		c.walkStmt(stmt, sc)
	}
}

func (c *checker) walkStmt(stmt ast.Stmt, sc scope) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		c.walkExprStmt(s, sc)
	case *ast.AssignStmt:
		c.walkAssign(s, sc)
	case *ast.DeclStmt:
		c.walkDecl(s, sc)
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			c.checkExpr(r, sc)
		}
	case *ast.IfStmt:
		c.walkIf(s, sc)
	case *ast.BlockStmt:
		c.walkBlock(s, sc)
	case *ast.ForStmt:
		c.walkFor(s, sc)
	case *ast.RangeStmt:
		c.walkRange(s, sc)
	case *ast.SwitchStmt:
		c.walkSwitch(s, sc)
	case *ast.TypeSwitchStmt:
		c.walkTypeSwitch(s, sc)
	case *ast.SelectStmt:
		c.walkSelect(s, sc)
	case *ast.LabeledStmt:
		c.walkStmt(s.Stmt, sc)
	case *ast.DeferStmt:
		c.checkExpr(s.Call, sc)
	case *ast.GoStmt:
		c.checkExpr(s.Call, sc)
	case *ast.SendStmt:
		c.checkExpr(s.Chan, sc)
		c.checkExpr(s.Value, sc)
	case *ast.IncDecStmt:
		c.checkExpr(s.X, sc)
	default:
		// Backstop so a statement kind nobody wired up explicitly still gets its
		// expressions checked (flow-insensitively) instead of silently passing.
		c.checkStmtExprs(stmt, sc)
	}
}

// walkBlock walks a bare block with its own clone so block-scoped `:=` names do not
// leak outward, then invalidates in the parent scope every path the block assigned,
// the same technique loopScope uses to drop proofs a write could have falsified.
func (c *checker) walkBlock(b *ast.BlockStmt, sc scope) {
	c.walk(b.List, sc.clone())

	for _, path := range assignedPaths(b.List, sc.alias) {
		sc.invalidate(path)
	}
}

func (c *checker) checkStmtExprs(stmt ast.Stmt, sc scope) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		expr, isExpr := n.(ast.Expr)
		if !isExpr {
			return true
		}

		c.checkExpr(expr, sc)

		return false
	})
}

func (c *checker) walkExprStmt(s *ast.ExprStmt, sc scope) {
	c.checkExpr(s.X, sc)

	call, isCall := s.X.(*ast.CallExpr)
	if !isCall || !c.isAssertCall(call) || len(call.Args) == 0 {
		return
	}

	guarded, _ := nilGuards(call.Args[0], sc)
	for _, path := range guarded {
		sc.proven[path] = true
	}
}

func (c *checker) walkAssign(s *ast.AssignStmt, sc scope) {
	for _, rhs := range s.Rhs {
		c.checkExpr(rhs, sc)
	}

	for _, lhs := range s.Lhs {
		c.checkExpr(lhs, sc)
	}

	for _, lhs := range s.Lhs {
		c.invalidateTarget(lhs, sc)
	}

	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}

	c.recordAssignment(s.Lhs[0], s.Rhs[0], sc)
}

// invalidateTarget removes the proofs a write to lhs falsifies. Writing to a local
// name only touches that name; writing to a selector touches the field itself, which
// is what makes the `o.P == nil` guard stop applying after `o.P = nil`.
func (c *checker) invalidateTarget(lhs ast.Expr, sc scope) {
	switch target := lhs.(type) {
	case *ast.Ident:
		delete(sc.alias, target.Name)
		delete(sc.errProof, target.Name)
		sc.invalidate(target.Name)
	case *ast.SelectorExpr:
		if path, ok := canonicalPath(target, sc.alias); ok {
			sc.invalidate(path)
		}
	}
}

func (c *checker) recordAssignment(lhs ast.Expr, rhs ast.Expr, sc scope) {
	path, ok := canonicalPath(lhs, sc.alias)
	if !ok {
		return
	}

	if isDefinitelyNonNil(rhs) {
		sc.proven[path] = true

		for _, literalPath := range provenFromLiteral(path, rhs) {
			sc.proven[literalPath] = true
		}

		return
	}

	id, isIdent := lhs.(*ast.Ident)
	if !isIdent || id.Name == "_" {
		return
	}

	if paths := c.validatorProof(rhs, sc); len(paths) > 0 {
		sc.errProof[id.Name] = paths

		return
	}

	if p, ok := canonicalPath(rhs, sc.alias); ok {
		sc.alias[id.Name] = p
	}
}

// validatorProof reports the paths a call proves non-nil when its error result is
// nil: the receiver path of the call joined with each field the callee's fact names.
func (c *checker) validatorProof(rhs ast.Expr, sc scope) []string {
	call, isCall := rhs.(*ast.CallExpr)
	if !isCall {
		return nil
	}

	sel, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return nil
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(sel.Sel).(*types.Func)
	if !isFunc || fn.Signature().Recv() == nil {
		return nil
	}

	var fact validatedFields
	if !c.pass.ImportObjectFact(fn, &fact) {
		return nil
	}

	base, ok := canonicalPath(sel.X, sc.alias)
	if !ok {
		return nil
	}

	paths := make([]string, 0, len(fact.Fields))
	for _, field := range fact.Fields {
		paths = append(paths, base+"."+field)
	}

	return paths
}

func (c *checker) walkDecl(s *ast.DeclStmt, sc scope) {
	decl, isGen := s.Decl.(*ast.GenDecl)
	if !isGen || decl.Tok != token.VAR {
		return
	}

	for _, spec := range decl.Specs {
		value, isValue := spec.(*ast.ValueSpec)
		if !isValue {
			continue
		}

		for _, v := range value.Values {
			c.checkExpr(v, sc)
		}

		for _, name := range value.Names {
			delete(sc.alias, name.Name)
			delete(sc.errProof, name.Name)
			sc.invalidate(name.Name)
		}

		if len(value.Names) == 1 && len(value.Values) == 1 {
			c.recordAssignment(value.Names[0], value.Values[0], sc)
		}
	}
}

func (c *checker) walkIf(s *ast.IfStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	c.checkExpr(s.Cond, inner)

	whenTrue, whenFalse := nilGuards(s.Cond, inner)

	c.walk(s.Body.List, inner.with(whenTrue))

	if s.Else != nil {
		c.walkStmt(s.Else, inner.with(whenFalse))
	}

	// A branch that cannot fall through proves the opposite condition for
	// everything after the statement.
	if blockExits(s.Body) {
		for _, path := range whenFalse {
			sc.proven[path] = true
		}
	}

	if eb, isBlock := s.Else.(*ast.BlockStmt); isBlock && blockExits(eb) {
		for _, path := range whenTrue {
			sc.proven[path] = true
		}
	}
}

func (c *checker) walkFor(s *ast.ForStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	c.checkExpr(s.Cond, inner)

	whenTrue, _ := nilGuards(s.Cond, inner)

	body := c.loopScope(s.Body, inner).with(whenTrue)
	c.walk(s.Body.List, body)

	if s.Post != nil {
		c.walkStmt(s.Post, body)
	}
}

func (c *checker) walkRange(s *ast.RangeStmt, sc scope) {
	inner := sc.clone()

	c.checkExpr(s.X, inner)

	for _, v := range []ast.Expr{s.Key, s.Value} {
		if v == nil {
			continue
		}

		c.invalidateTarget(v, inner)
	}

	c.walk(s.Body.List, c.loopScope(s.Body, inner))
}

// loopScope drops the proofs any iteration of the body can falsify, since the body
// runs again after its own writes and the entry proof no longer holds there.
func (c *checker) loopScope(body *ast.BlockStmt, sc scope) scope {
	out := sc.clone()

	if body == nil {
		return out
	}

	for _, path := range assignedPaths(body.List, sc.alias) {
		out.invalidate(path)
	}

	return out
}

func (c *checker) walkSwitch(s *ast.SwitchStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	c.checkExpr(s.Tag, inner)

	c.walkCaseClauses(s.Body, inner)
}

func (c *checker) walkTypeSwitch(s *ast.TypeSwitchStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	if s.Assign != nil {
		c.walkStmt(s.Assign, inner)
	}

	c.walkCaseClauses(s.Body, inner)
}

func (c *checker) walkCaseClauses(body *ast.BlockStmt, sc scope) {
	if body == nil {
		return
	}

	for _, stmt := range body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}

		for _, expr := range clause.List {
			c.checkExpr(expr, sc)
		}

		c.walk(clause.Body, sc.clone())
	}
}

func (c *checker) walkSelect(s *ast.SelectStmt, sc scope) {
	if s.Body == nil {
		return
	}

	for _, stmt := range s.Body.List {
		clause, isClause := stmt.(*ast.CommClause)
		if !isClause {
			continue
		}

		inner := sc.clone()

		if clause.Comm != nil {
			c.walkStmt(clause.Comm, inner)
		}

		c.walk(clause.Body, inner)
	}
}

func (c *checker) checkExpr(expr ast.Expr, sc scope) {
	if expr == nil {
		return
	}

	ast.Inspect(expr, func(n ast.Node) bool {
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
			c.checkPointerBase(e.X, e.Sel.Pos(), sc)
		case *ast.StarExpr:
			c.checkPointerBase(e.X, e.Pos(), sc)
		}

		return true
	})
}

// walkFuncLit walks a closure body with the scope proven at the point the closure is
// created, and records it as walked so the top-level Preorder pass does not re-walk it
// from an empty scope and lose that context.
func (c *checker) walkFuncLit(lit *ast.FuncLit, sc scope) {
	if lit.Body == nil {
		return
	}

	c.walked[lit] = true

	c.walk(lit.Body.List, sc.clone())
}

// checkShortCircuit gives the right operand the knowledge the left one established,
// which is what makes `x != nil && x.f > 0` a guard rather than two dereferences.
func (c *checker) checkShortCircuit(e *ast.BinaryExpr, sc scope) {
	c.checkExpr(e.X, sc)

	whenTrue, whenFalse := nilGuards(e.X, sc)

	reached := whenTrue
	if e.Op == token.LOR {
		reached = whenFalse
	}

	c.checkExpr(e.Y, sc.with(reached))
}

func (c *checker) isAssertCall(call *ast.CallExpr) bool {
	if c.assertPackage == "" {
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "Assert" && sel.Sel.Name != "AssertWithCode" {
		return false
	}

	obj := c.pass.TypesInfo.ObjectOf(sel.Sel)

	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}

	return fn.Pkg().Path() == c.assertPackage
}

func (c *checker) checkPointerBase(base ast.Expr, pos token.Pos, sc scope) {
	baseType := c.pass.TypesInfo.TypeOf(base)
	if baseType == nil {
		return
	}

	if _, isPtr := baseType.Underlying().(*types.Pointer); !isPtr {
		return
	}

	if c.isPackageQualified(base) {
		return
	}

	path, ok := canonicalPath(base, sc.alias)
	if !ok || sc.proven[path] {
		return
	}

	// Bare locals and parameters are out of scope: this analyzer is about fields
	// reached through a struct, which is where the guard is easy to forget.
	if !strings.Contains(path, ".") {
		return
	}

	if c.isExcluded(c.pass.Fset.Position(pos).Filename) {
		return
	}

	c.pass.Reportf(pos, "%s may be nil here", path)
}

// isPackageQualified reports whether base is a selector into another package
// (pkg.GlobalVar), which is not a struct field and out of scope the same way a bare
// local is.
func (c *checker) isPackageQualified(base ast.Expr) bool {
	sel, isSelector := base.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}

	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return false
	}

	_, isPkgName := c.pass.TypesInfo.ObjectOf(id).(*types.PkgName)

	return isPkgName
}

func (c *checker) isExcluded(filename string) bool {
	normalized := filepath.ToSlash(filename)

	if strings.HasSuffix(normalized, "_test.go") {
		return true
	}

	for _, fragment := range c.excludePaths {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}

func blockExits(b *ast.BlockStmt) bool {
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

		id, isIdent := call.Fun.(*ast.Ident)

		return isIdent && id.Name == "panic"
	default:
		return false
	}
}

// nilGuards decomposes a condition into the paths proven non-nil when it is true and
// the ones proven non-nil when it is false.
func nilGuards(cond ast.Expr, sc scope) (whenTrue []string, whenFalse []string) {
	switch e := cond.(type) {
	case *ast.ParenExpr:
		return nilGuards(e.X, sc)
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return nil, nil
		}

		inverted, straight := nilGuards(e.X, sc)

		return straight, inverted
	case *ast.BinaryExpr:
		return binaryNilGuards(e, sc)
	default:
		return nil, nil
	}
}

func binaryNilGuards(e *ast.BinaryExpr, sc scope) (whenTrue []string, whenFalse []string) {
	switch e.Op {
	case token.LAND:
		leftTrue, _ := nilGuards(e.X, sc)
		rightTrue, _ := nilGuards(e.Y, sc)

		return append(leftTrue, rightTrue...), nil
	case token.LOR:
		_, leftFalse := nilGuards(e.X, sc)
		_, rightFalse := nilGuards(e.Y, sc)

		return nil, append(leftFalse, rightFalse...)
	case token.NEQ, token.EQL:
		path, ok := nilComparisonPath(e, sc.alias)
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

func nilComparisonPath(be *ast.BinaryExpr, alias map[string]string) (string, bool) {
	var target ast.Expr

	switch {
	case isNilIdent(be.Y):
		target = be.X
	case isNilIdent(be.X):
		target = be.Y
	default:
		return "", false
	}

	return canonicalPath(target, alias)
}

func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)

	return ok && id.Name == "nil"
}

func isDefinitelyNonNil(expr ast.Expr) bool {
	unary, isUnary := expr.(*ast.UnaryExpr)
	if isUnary && unary.Op == token.AND {
		return true
	}

	call, isCall := expr.(*ast.CallExpr)
	if !isCall {
		return false
	}

	id, isIdent := call.Fun.(*ast.Ident)

	return isIdent && id.Name == "new"
}

// provenFromLiteral extends the proof for path to the fields a composite literal sets
// directly, recursing into nested literals so `&T{F: &U{G: &V{}}}` also proves path.F
// and path.F.G.
func provenFromLiteral(path string, expr ast.Expr) []string {
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
		if !isIdent || !isDefinitelyNonNil(kv.Value) {
			continue
		}

		fieldPath := path + "." + key.Name
		paths = append(paths, fieldPath)
		paths = append(paths, provenFromLiteral(fieldPath, kv.Value)...)
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

// assignedPaths collects every path written anywhere under stmts, nested statements
// included, so a caller can drop the proofs those writes falsify.
func assignedPaths(stmts []ast.Stmt, alias map[string]string) []string {
	var paths []string

	appendTarget := func(expr ast.Expr) {
		switch target := expr.(type) {
		case *ast.Ident:
			paths = append(paths, target.Name)
		case *ast.SelectorExpr:
			if path, ok := canonicalPath(target, alias); ok {
				paths = append(paths, path)
			}
		}
	}

	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range s.Lhs {
					appendTarget(lhs)
				}
			case *ast.IncDecStmt:
				appendTarget(s.X)
			}

			return true
		})
	}

	return paths
}

func canonicalPath(expr ast.Expr, alias map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return canonicalPath(e.X, alias)
	case *ast.Ident:
		if p, ok := alias[e.Name]; ok {
			return p, true
		}

		return e.Name, true
	case *ast.SelectorExpr:
		base, ok := canonicalPath(e.X, alias)
		if !ok {
			return "", false
		}

		return base + "." + e.Sel.Name, true
	default:
		return "", false
	}
}

// exportValidatorFact records what a validator method proves about its receiver.
//
// A method qualifies when it returns exactly one unnamed `error` and, before any
// statement that can return a nil error, rejects a nil receiver field with an early
// return of something non-nil. Callers that check the error therefore know those
// fields are set, which is the postcondition the fact carries.
func (c *checker) exportValidatorFact(fd *ast.FuncDecl) {
	recv, ok := receiverName(fd)
	if !ok || fd.Body == nil || !c.returnsBareError(fd.Type) {
		return
	}

	fields := c.validatedReceiverFields(fd.Body, recv)
	if len(fields) == 0 {
		return
	}

	fn, isFunc := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func)
	if !isFunc {
		return
	}

	c.pass.ExportObjectFact(fn, &validatedFields{Fields: fields})
}

func receiverName(fd *ast.FuncDecl) (string, bool) {
	if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return "", false
	}

	name := fd.Recv.List[0].Names[0].Name
	if name == "" || name == "_" {
		return "", false
	}

	return name, true
}

// returnsBareError insists on a single unnamed `error` result: a named result can be
// rewritten by a deferred closure after every guard has passed, and extra results
// would leave the error position ambiguous at the call site.
func (c *checker) returnsBareError(ft *ast.FuncType) bool {
	if ft.Results == nil || len(ft.Results.List) != 1 || len(ft.Results.List[0].Names) != 0 {
		return false
	}

	resultType := c.pass.TypesInfo.TypeOf(ft.Results.List[0].Type)

	return resultType != nil && types.Identical(resultType, errorType)
}

var errorType = types.Universe.Lookup("error").Type()

func (c *checker) validatedReceiverFields(body *ast.BlockStmt, recv string) []string {
	if containsJump(body) {
		return nil
	}

	written := c.writtenReceiverFields(body, recv)
	if written == nil {
		return nil
	}

	limit := c.firstNilErrorReturn(body)

	var fields []string

	for i, stmt := range body.List {
		if i >= limit {
			break
		}

		guarded, ok := c.rejectingGuard(stmt, recv)
		if !ok {
			continue
		}

		for _, field := range guarded {
			if written[field] || slices.Contains(fields, field) {
				continue
			}

			fields = append(fields, field)
		}
	}

	return fields
}

// rejectingGuard reports the receiver fields an `if <field> == nil { return err }`
// statement proves non-nil on its fall-through edge.
func (c *checker) rejectingGuard(stmt ast.Stmt, recv string) ([]string, bool) {
	ifStmt, isIf := stmt.(*ast.IfStmt)
	if !isIf || ifStmt.Init != nil || ifStmt.Else != nil {
		return nil, false
	}

	if !c.rejectsWithError(ifStmt.Body) {
		return nil, false
	}

	_, whenFalse := nilGuards(ifStmt.Cond, newScope())

	var fields []string

	for _, path := range whenFalse {
		field, ok := strings.CutPrefix(path, recv+".")
		if !ok || strings.Contains(field, ".") {
			continue
		}

		fields = append(fields, field)
	}

	return fields, len(fields) > 0
}

// rejectsWithError reports whether a block always leaves the function and can only
// leave it carrying a non-nil error.
func (c *checker) rejectsWithError(block *ast.BlockStmt) bool {
	if !blockExits(block) {
		return false
	}

	rejects := true

	inspectSkippingFuncLits(block, func(n ast.Node) {
		ret, isReturn := n.(*ast.ReturnStmt)
		if isReturn && !c.returnsNonNilError(ret) {
			rejects = false
		}
	})

	return rejects
}

// firstNilErrorReturn is the index of the earliest top-level statement from which the
// function can return a nil error. A guard placed after it proves nothing, because a
// caller can see a nil error without that guard having run.
func (c *checker) firstNilErrorReturn(body *ast.BlockStmt) int {
	for i, stmt := range body.List {
		reachesNil := false

		inspectSkippingFuncLits(stmt, func(n ast.Node) {
			ret, isReturn := n.(*ast.ReturnStmt)
			if isReturn && !c.returnsNonNilError(ret) {
				reachesNil = true
			}
		})

		if reachesNil {
			return i
		}
	}

	return len(body.List)
}

func (c *checker) returnsNonNilError(ret *ast.ReturnStmt) bool {
	if len(ret.Results) != 1 {
		return false
	}

	return c.nonNilErrorExpr(ret.Results[0])
}

// nonNilErrorExpr accepts the expressions an error guard actually returns: a
// constructor call, an address, or a package-level sentinel. A local variable is
// refused precisely because `return err` can hand back a nil error.
func (c *checker) nonNilErrorExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return c.nonNilErrorExpr(e.X)
	case *ast.CallExpr:
		return !c.pass.TypesInfo.Types[e.Fun].IsType()
	case *ast.UnaryExpr:
		return e.Op == token.AND
	case *ast.CompositeLit:
		return true
	case *ast.Ident, *ast.SelectorExpr:
		return c.isPackageLevelVar(expr)
	default:
		return false
	}
}

func (c *checker) isPackageLevelVar(expr ast.Expr) bool {
	var ident *ast.Ident

	switch e := expr.(type) {
	case *ast.Ident:
		ident = e
	case *ast.SelectorExpr:
		ident = e.Sel
	default:
		return false
	}

	v, isVar := c.pass.TypesInfo.ObjectOf(ident).(*types.Var)
	if !isVar || v.IsField() || v.Pkg() == nil {
		return false
	}

	return v.Parent() == v.Pkg().Scope()
}

func rootIdent(expr ast.Expr) (*ast.Ident, bool) {
	switch target := expr.(type) {
	case *ast.Ident:
		return target, true
	case *ast.UnaryExpr:
		if target.Op == token.AND {
			return rootIdent(target.X)
		}

		return nil, false
	case *ast.StarExpr:
		return rootIdent(target.X)
	case *ast.SelectorExpr:
		return rootIdent(target.X)
	case *ast.ParenExpr:
		return rootIdent(target.X)
	default:
		return nil, false
	}
}

// writtenReceiverFields collects the fields the body writes, so a guard on a field the
// method later clears proves nothing. A write to the receiver itself, or through it,
// is reported as nil: nothing about that receiver survives.
func (c *checker) writtenReceiverFields(body *ast.BlockStmt, recv string) map[string]bool {
	written := map[string]bool{}
	poisoned := false

	record := func(expr ast.Expr) {
		switch target := expr.(type) {
		case *ast.Ident:
			if target.Name == recv {
				poisoned = true
			}
		case *ast.StarExpr:
			if id, isIdent := target.X.(*ast.Ident); isIdent && id.Name == recv {
				poisoned = true
			}
		case *ast.SelectorExpr:
			id, isIdent := target.X.(*ast.Ident)
			if !isIdent || id.Name != recv {
				return
			}

			written[target.Sel.Name] = true
		}
	}

	poisonIfReceiverCall := func(sel *ast.SelectorExpr) {
		id, ok := rootIdent(sel.X)
		if !ok || id.Name != recv {
			return
		}

		if c.calleeHasPointerReceiver(sel.Sel) {
			poisoned = true
		}
	}

	poisonIfReceiverArg := func(expr ast.Expr) {
		id, ok := rootIdent(expr)
		if !ok || id.Name != recv {
			return
		}

		if c.argTypeReachesReceiver(expr) {
			poisoned = true
		}
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				record(lhs)
			}
		case *ast.UnaryExpr:
			if node.Op == token.AND {
				record(node.X)
			}
		case *ast.ReturnStmt:
			return false
		case *ast.CallExpr:
			if sel, isSel := node.Fun.(*ast.SelectorExpr); isSel {
				poisonIfReceiverCall(sel)
			}

			for _, arg := range node.Args {
				poisonIfReceiverArg(arg)
			}
		}

		return true
	})

	if poisoned {
		return nil
	}

	return written
}

// calleeHasPointerReceiver reports whether the method sel resolves to is declared
// with a pointer receiver, so a call through it can reach the receiver's storage.
// A selector that does not resolve to a concrete *types.Func - an interface method
// value, a func-typed field - poisons conservatively, since its target cannot be
// checked.
func (c *checker) calleeHasPointerReceiver(sel *ast.Ident) bool {
	fn, isFunc := c.pass.TypesInfo.ObjectOf(sel).(*types.Func)
	if !isFunc {
		return true
	}

	sig := fn.Signature()
	if sig == nil || sig.Recv() == nil {
		return false
	}

	unaliased := types.Unalias(sig.Recv().Type())
	if unaliased == nil {
		return true
	}

	_, isPointer := unaliased.(*types.Pointer)

	return isPointer
}

// argTypeReachesReceiver reports whether expr's static type could let the callee
// reach the receiver's storage through the argument: a pointer, slice, map,
// channel, func or interface value. A plain value type is passed as a copy and
// cannot.
func (c *checker) argTypeReachesReceiver(expr ast.Expr) bool {
	t := c.pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return true
	}

	unaliased := types.Unalias(t)
	if unaliased == nil {
		return true
	}

	switch unaliased.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	default:
		return false
	}
}

// containsJump reports a label or goto, which would let control reach a guard's
// fall-through without the guard having decided anything.
func containsJump(body *ast.BlockStmt) bool {
	found := false

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.LabeledStmt:
			found = true
		case *ast.BranchStmt:
			if node.Tok == token.GOTO {
				found = true
			}
		}

		return !found
	})

	return found
}

func inspectSkippingFuncLits(n ast.Node, visit func(ast.Node)) {
	ast.Inspect(n, func(node ast.Node) bool {
		if _, isLit := node.(*ast.FuncLit); isLit {
			return false
		}

		visit(node)

		return true
	})
}
