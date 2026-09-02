package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

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

	_, whenFalse := c.nilGuards(ifStmt.Cond, newScope())

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
	if !c.blockExits(block) {
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
