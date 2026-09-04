package droppederr

import (
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
)

// render prints node back to source using fset, for turning an AST fragment
// into the human-readable text a finding's detail quotes.
func render(fset *token.FileSet, node ast.Node) string {
	var sb strings.Builder

	if err := printer.Fprint(&sb, fset, node); err != nil {
		return ""
	}

	return sb.String()
}

// rootIdent walks down the left-hand side of a selector/call/index/paren
// chain to the identifier the whole expression is ultimately rooted at.
func rootIdent(expr ast.Expr) (*ast.Ident, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		return e, true
	case *ast.SelectorExpr:
		return rootIdent(e.X)
	case *ast.CallExpr:
		return rootIdent(e.Fun)
	case *ast.IndexExpr:
		return rootIdent(e.X)
	case *ast.ParenExpr:
		return rootIdent(e.X)
	default:
		return nil, false
	}
}

// blockStatements returns the statement list n holds, for the node kinds
// whose body a scan needs to walk statement-by-statement (to see each
// statement's predecessor), or false when n is not one of those kinds.
func blockStatements(n ast.Node) ([]ast.Stmt, bool) {
	switch node := n.(type) {
	case *ast.BlockStmt:
		return node.List, true
	case *ast.CaseClause:
		return node.Body, true
	case *ast.CommClause:
		return node.Body, true
	default:
		return nil, false
	}
}

// inspectExcludingFuncLit walks node like ast.Inspect, except it never
// descends into a nested function literal: a closure's own error is already
// propagated out through its return, so a database call inside it must not
// taint the enclosing statement's classification.
func inspectExcludingFuncLit(node ast.Node, visit func(ast.Node) bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}

		return visit(n)
	})
}

// assignsTo reports whether assign's left-hand side names name.
func assignsTo(assign *ast.AssignStmt, name string) bool {
	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if ok && ident.Name == name {
			return true
		}
	}

	return false
}

// errNilCheck reports whether cond is an `<ident> != nil` check on an
// identifier whose name looks like an error variable, returning that name.
func errNilCheck(cond ast.Expr) (string, bool) {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return "", false
	}

	nilIdent, ok := bin.Y.(*ast.Ident)
	if !ok || nilIdent.Name != "nil" {
		return "", false
	}

	errIdent, ok := bin.X.(*ast.Ident)
	if !ok || !strings.Contains(strings.ToLower(errIdent.Name), "err") {
		return "", false
	}

	return errIdent.Name, true
}
