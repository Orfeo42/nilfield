package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

var errorType = types.Universe.Lookup("error").Type()

// isUniverse reports whether obj is the predeclared universe object named name,
// which is what lets nil/new/panic resolution survive a user shadowing that name
// with their own identifier.
func isUniverse(obj types.Object, name string) bool {
	return obj != nil && obj == types.Universe.Lookup(name)
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
	case *ast.StarExpr:
		p, ok := canonicalPath(e.X, alias)
		if !ok {
			return "", false
		}

		return "(*" + p + ")", true
	default:
		return "", false
	}
}

// isFieldPath reports whether path was reached through a struct field.
func isFieldPath(path string) bool {
	return strings.Contains(path, ".")
}

// isStarPath reports whether path is an explicit dereference, e.g. "(*pp)".
func isStarPath(path string) bool {
	return strings.HasPrefix(path, "(*")
}

// isErrorType reports whether t is exactly the universe error type.
func isErrorType(t types.Type) bool {
	return types.Identical(t, errorType)
}

// isNillableKind reports whether t's underlying type is one of the kinds that
// can hold a nil value: pointer, interface, map, slice, chan or signature.
func isNillableKind(t types.Type) bool {
	switch types.Unalias(t).Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Map, *types.Slice, *types.Chan, *types.Signature:
		return true
	default:
		return false
	}
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

// addressTakenNames lists the locals whose address is taken anywhere under n.
// Handing &x to a call lets the callee assign x (errors.As, json.Unmarshal), so
// whatever nil state was known about x stops holding from that statement on.
func addressTakenNames(n ast.Node) []string {
	var names []string

	ast.Inspect(n, func(node ast.Node) bool {
		unary, isUnary := node.(*ast.UnaryExpr)
		if !isUnary || unary.Op != token.AND {
			return true
		}

		if id, isIdent := ast.Unparen(unary.X).(*ast.Ident); isIdent {
			names = append(names, id.Name)
		}

		return true
	})

	return names
}
