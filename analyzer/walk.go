package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
)

func (c *checker) walk(stmts []ast.Stmt, sc scope) {
	for _, stmt := range stmts {
		c.walkStmt(stmt, sc)
	}
}

func (c *checker) walkStmt(stmt ast.Stmt, sc scope) {
	for _, name := range addressTakenNames(stmt) {
		sc.dropNil(name)
	}

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
		c.walkGo(s, sc)
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

	c.invalidateWrites(b.List, sc)
}

// invalidateWrites drops, in sc, every proof a write anywhere in stmts could have
// falsified. Used wherever a branch was walked in a cloned scope but its writes must
// still be reflected back onto the scope that follows it.
func (c *checker) invalidateWrites(stmts []ast.Stmt, sc scope) {
	for _, path := range assignedPaths(stmts, sc.alias) {
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
	if !isCall {
		return
	}

	arg, ok := c.assertedArg(call)
	if !ok {
		return
	}

	guarded, _ := c.nilGuards(call.Args[arg], sc)
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

	if len(s.Lhs) > 1 && len(s.Rhs) == 1 {
		c.markNilResultsFromCall(s.Lhs, s.Rhs[0], sc)
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

	if c.isDefinitelyNonNil(rhs) {
		sc.proven[path] = true

		for _, literalPath := range c.provenFromLiteral(path, rhs) {
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

	// A recognised nil origin takes priority over the generic alias: aliasing id
	// to rhs's own canonical path (here just rhs's bare name, or the meaningless
	// "nil") would let a later read of id resolve through that alias instead of
	// through id's own recorded nilable state — the state markNil is about to
	// record for id specifically, which a different-typed rhs (an interface
	// wrapping a nil pointer) does not share with the path it was copied from.
	if state, ok := c.nilOriginState(lhs, rhs, sc); ok {
		sc.markNil(id.Name, state)

		return
	}

	if p, ok := canonicalPath(rhs, sc.alias); ok {
		sc.alias[id.Name] = p
	}
}

// nilOriginState reports what lhs's value is known to be after `lhs = rhs`, when
// rhs is one of the recognised nil-producing shapes: the universe nil identifier,
// a local already known to be nilable, or a nil-origin expression such as a map
// index or a single-form type assertion.
func (c *checker) nilOriginState(lhs ast.Expr, rhs ast.Expr, sc scope) (nilState, bool) {
	rhs = ast.Unparen(rhs)

	if id, isIdent := rhs.(*ast.Ident); isIdent {
		if isUniverse(c.resolve(id), "nil") {
			return isNil, true
		}

		if state, ok := sc.nilable[id.Name]; ok {
			if state == isNil && c.assignsTypedNilToInterface(lhs, rhs) {
				return typedNil, true
			}

			return state, true
		}
	}

	if c.isNilOrigin(rhs) {
		return maybeNil, true
	}

	return 0, false
}

// assignsTypedNilToInterface reports whether lhs is interface-typed and rhs is
// pointer-typed, the shape that turns a nil pointer into a non-nil interface value
// holding a nil pointer.
func (c *checker) assignsTypedNilToInterface(lhs ast.Expr, rhs ast.Expr) bool {
	lt := c.pass.TypesInfo.TypeOf(lhs)
	rt := c.pass.TypesInfo.TypeOf(rhs)

	if lt == nil || rt == nil {
		return false
	}

	if _, isInterface := lt.Underlying().(*types.Interface); !isInterface {
		return false
	}

	_, isPointer := rt.Underlying().(*types.Pointer)

	return isPointer
}

// isNilOrigin reports whether expr is one of the expression shapes that yield a
// zero value which may itself be nil: a map or slice index whose element type is
// nillable, or a single-form type assertion (as opposed to the `.(type)` switch
// form, whose Type field is nil) to a nillable type.
func (c *checker) isNilOrigin(expr ast.Expr) bool {
	expr = ast.Unparen(expr)

	switch e := expr.(type) {
	case *ast.IndexExpr:
		t := c.pass.TypesInfo.TypeOf(e.X)
		if t == nil {
			return false
		}

		switch underlying := t.Underlying().(type) {
		case *types.Map:
			return isNillableKind(underlying.Elem())
		case *types.Slice:
			return isNillableKind(underlying.Elem())
		default:
			return false
		}
	case *ast.TypeAssertExpr:
		if e.Type == nil {
			return false
		}

		t := c.pass.TypesInfo.TypeOf(e.Type)

		return t != nil && isNillableKind(t)
	case *ast.CallExpr:
		if !slices.Contains(c.calleeNilResults(e), 0) {
			return false
		}

		t := c.pass.TypesInfo.TypeOf(e)
		if t == nil {
			return false
		}

		// A multi-result call cannot appear here as a sub-expression except as
		// the sole argument to another call, which is not a use site this
		// analyzer reports on; excluding a tuple type keeps that shape out.
		_, isTuple := t.(*types.Tuple)

		return !isTuple
	default:
		return false
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

			if len(value.Values) == 0 && name.Name != "_" {
				if t := c.pass.TypesInfo.TypeOf(name); t != nil && isNillableKind(t) {
					sc.markNil(name.Name, isNil)
				}
			}
		}

		if len(value.Names) == 1 && len(value.Values) == 1 {
			c.recordAssignment(value.Names[0], value.Values[0], sc)
		}

		if len(value.Names) > 1 && len(value.Values) == 1 {
			names := make([]ast.Expr, len(value.Names))
			for i, name := range value.Names {
				names[i] = name
			}

			c.markNilResultsFromCall(names, value.Values[0], sc)
		}
	}
}

func (c *checker) walkIf(s *ast.IfStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	c.checkExpr(s.Cond, inner)

	whenTrue, whenFalse := c.nilGuards(s.Cond, inner)

	c.walk(s.Body.List, inner.with(whenTrue))

	if s.Else != nil {
		c.walkStmt(s.Else, inner.with(whenFalse))
	}

	// A branch that cannot fall through proves the opposite condition for
	// everything after the statement.
	if c.blockExits(s.Body) {
		for _, path := range whenFalse {
			sc.proven[path] = true
		}
	}

	if eb, isBlock := s.Else.(*ast.BlockStmt); isBlock && c.blockExits(eb) {
		for _, path := range whenTrue {
			sc.proven[path] = true
		}
	}

	// A branch that falls through to the statement after the if runs zero or one
	// times on that path, but it still may have run, so its writes must be
	// reflected in the surrounding scope.
	if !c.blockExits(s.Body) {
		c.invalidateWrites(s.Body.List, sc)
	}

	switch elseStmt := s.Else.(type) {
	case *ast.BlockStmt:
		if !c.blockExits(elseStmt) {
			c.invalidateWrites(elseStmt.List, sc)
		}
	case *ast.IfStmt:
		// An else-if chain is not checked for exiting here; its own writes are
		// still reachable from this point, so invalidate unconditionally.
		c.invalidateWrites([]ast.Stmt{elseStmt}, sc)
	}
}

func (c *checker) walkFor(s *ast.ForStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	c.checkExpr(s.Cond, inner)

	whenTrue, _ := c.nilGuards(s.Cond, inner)

	body := c.loopScope(s.Body, inner).with(whenTrue)
	c.walk(s.Body.List, body)

	if s.Post != nil {
		c.walkStmt(s.Post, body)
	}

	// The body runs zero or more times, so its writes must invalidate the parent's
	// proofs regardless of any loop-condition guard.
	c.invalidateWrites(s.Body.List, sc)
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

	c.invalidateWrites(s.Body.List, sc)
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

// walkSwitch reads a switch statement as a guard: each non-default clause's
// expression list proves paths non-nil both within its own body (whenTrue) and, for
// every clause evaluated after it, in the fact that this clause did not match
// (whenFalse, accumulated into excluded).
func (c *checker) walkSwitch(s *ast.SwitchStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	c.checkExpr(s.Tag, inner)

	if s.Body == nil {
		return
	}

	var excluded []string
	var defaultClause *ast.CaseClause
	var clauseWhenFalse [][]string
	var clauseExits []bool

	for _, stmt := range s.Body.List {
		clause, isClause := stmt.(*ast.CaseClause)
		if !isClause {
			continue
		}

		if len(clause.List) == 0 {
			defaultClause = clause

			continue
		}

		for _, expr := range clause.List {
			c.checkExpr(expr, inner.with(excluded))
		}

		whenTrue, whenFalse := c.clauseListGuards(s.Tag, clause.List, inner)

		c.walk(clause.Body, inner.with(excluded).with(whenTrue))

		exits := c.blockExits(&ast.BlockStmt{List: clause.Body})
		if !exits {
			c.invalidateWrites(clause.Body, sc)
		}

		clauseWhenFalse = append(clauseWhenFalse, whenFalse)
		clauseExits = append(clauseExits, exits)

		excluded = append(excluded, whenFalse...)
	}

	if defaultClause != nil {
		c.walk(defaultClause.Body, inner.with(excluded))

		if !c.blockExits(&ast.BlockStmt{List: defaultClause.Body}) {
			c.invalidateWrites(defaultClause.Body, sc)
		}
	}

	// A clause's whenFalse only holds after the switch if we know its condition was
	// actually evaluated, which requires every clause before it to have exited too —
	// otherwise execution could have fallen out of an earlier, non-exiting clause
	// without this one's condition ever being tested. So stop at the first clause
	// that does not exit instead of skipping past it.
	for i, exits := range clauseExits {
		if !exits {
			break
		}

		for _, path := range clauseWhenFalse[i] {
			sc.proven[path] = true
		}
	}
}

func (c *checker) walkTypeSwitch(s *ast.TypeSwitchStmt, sc scope) {
	inner := sc.clone()

	if s.Init != nil {
		c.walkStmt(s.Init, inner)
	}

	if s.Assign != nil {
		c.walkStmt(s.Assign, inner)
	}

	c.walkCaseClauses(s.Body, inner, sc)
}

// walkCaseClauses walks each case clause of body in its own clone of sc, then, if the
// clause's body does not exit, invalidates its writes back onto parent — the scope
// that follows the whole switch, as opposed to sc which is local to the switch.
func (c *checker) walkCaseClauses(body *ast.BlockStmt, sc scope, parent scope) {
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

		if !c.blockExits(&ast.BlockStmt{List: clause.Body}) {
			c.invalidateWrites(clause.Body, parent)
		}
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

		if !c.blockExits(&ast.BlockStmt{List: clause.Body}) {
			c.invalidateWrites(clause.Body, sc)
		}
	}
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

// walkGo handles a `go` statement. When the call is an immediately-invoked
// func literal, its arguments are evaluated at the `go` statement itself (still
// under whatever guard holds there), but its body runs later on another
// goroutine, so the body is walked with goroutineScope instead of the literal's
// enclosing scope. Any other call (`go f(x)`, `go o.Method()`) is checked
// flow-insensitively like before, since there is no literal body to walk with an
// adjusted scope.
func (c *checker) walkGo(s *ast.GoStmt, sc scope) {
	lit, isLit := ast.Unparen(s.Call.Fun).(*ast.FuncLit)
	if !isLit || lit.Body == nil {
		c.checkExpr(s.Call, sc)

		return
	}

	for _, arg := range s.Call.Args {
		c.checkExpr(arg, sc)
	}

	c.walkGoroutine(lit, sc)
}

// walkGoroutine walks a closure launched with `go` using goroutineScope instead
// of the enclosing scope, since the guard that dominates the `go` statement only
// held at the moment the goroutine was scheduled. Mirrors walkFuncLit otherwise.
func (c *checker) walkGoroutine(lit *ast.FuncLit, sc scope) {
	c.walked[lit] = true

	c.walk(lit.Body.List, goroutineScope(sc))
}
