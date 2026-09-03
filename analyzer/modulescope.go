package analyzer

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// fieldInModule reports whether a field declared in package fieldPkgPath
// belongs to the module mod describes: either fieldPkgPath equals mod.Path,
// or it is nested under it. A nil mod, or one whose Path is empty, means the
// driver could not determine the module — confirmed empirically for
// analysistest, which resolves a non-nil *analysis.Module with an empty Path
// when no go.mod governs the tree being loaded, the same "possibly nil"
// case the analysis.Pass.Module doc comment warns about — and that case
// answers true: suppressing a finding needs positive evidence that the
// field lives outside the module, never a guess made from missing
// information.
func fieldInModule(fieldPkgPath string, mod *analysis.Module) bool {
	if mod == nil || mod.Path == "" {
		return true
	}

	if fieldPkgPath == mod.Path {
		return true
	}

	return strings.HasPrefix(fieldPkgPath, mod.Path+"/")
}

// isOutOfModuleField reports whether field is a struct field declared in a
// package outside the module being analyzed: a dependency's own invariant,
// such as gogf's ghttp.Response.BufferWriter or sqlx.Rows' embedded
// *sql.Rows, that the consuming project cannot fix and cannot prove
// nil-safe, since any code anywhere could construct the dependency type
// directly.
func (c *checker) isOutOfModuleField(field *types.Var) bool {
	if field == nil || !field.IsField() {
		return false
	}

	pkg := field.Pkg()
	if pkg == nil {
		return false
	}

	return !fieldInModule(pkg.Path(), c.pass.Module)
}

// isOutOfModuleFieldAccess reports whether base is itself the field-selector
// expression for a field declared outside the module being analyzed: the
// AST node for `r.Response.BufferWriter` in `r.Response.BufferWriter.Write`,
// the same shape isWiredFieldAccess recognises for an always-wired field.
func (c *checker) isOutOfModuleFieldAccess(base ast.Expr) bool {
	sel, isSelector := base.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}

	selection := c.pass.TypesInfo.Selections[sel]
	if selection == nil || selection.Kind() != types.FieldVal {
		return false
	}

	field, isVar := selection.Obj().(*types.Var)

	return isVar && c.isOutOfModuleField(field)
}
