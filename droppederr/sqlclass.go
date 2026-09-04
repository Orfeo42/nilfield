package droppederr

import (
	"fmt"
	"go/ast"
	"strings"
)

var databaseMethods = map[string]bool{
	"Get":                 true,
	"Select":              true,
	"Query":               true,
	"QueryContext":        true,
	"QueryxContext":       true,
	"QueryRowxContext":    true,
	"Exec":                true,
	"ExecContext":         true,
	"GetContext":          true,
	"SelectContext":       true,
	"NamedExecContext":    true,
	"NamedQueryContext":   true,
	"PrepareNamedContext": true,
	"StructScan":          true,
	"Scan":                true,
	"Rebind":              true,
}

var databaseReceivers = []string{"r.ext", "r.db", "sqlx", "tx", "stmt", "rows"}

var domainWrappers = map[string]bool{
	"Error":                   true,
	"WrapError":               true,
	"WrapErrorSkip":           true,
	"GroupLookupError":        true,
	"GroupNotConfiguredError": true,
}

var nonClassifyingUtilityFuncs = map[string]bool{
	"AssertError":         true,
	"AssertErrorWithCode": true,
}

var sqlClassifierPredicates = map[string]bool{
	"IsDuplicateKey":        true,
	"IsDuplicateKeyError":   true,
	"IsForeignKeyViolation": true,
	"IsDeadlock":            true,
	"IsLockWaitTimeout":     true,
	"IsResourceExhausted":   true,
	"IsRetryable":           true,
	"IsDataError":           true,
}

// scanSQLClassification reports every error branch inside fd that guards a
// database call and raises through anything other than the configured SQL
// utility package's classifying helpers.
func (c *checker) scanSQLClassification(fd *ast.FuncDecl) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		stmts, ok := blockStatements(n)
		if !ok {
			return true
		}

		for i, stmt := range stmts {
			ifStmt, ok := stmt.(*ast.IfStmt)
			if !ok {
				continue
			}

			var previous ast.Stmt
			if i > 0 {
				previous = stmts[i-1]
			}

			c.reportSQLClass(fd.Name.Name, ifStmt, previous)
		}

		return true
	})
}

func (c *checker) reportSQLClass(function string, ifStmt *ast.IfStmt, previous ast.Stmt) {
	errVar, ok := errNilCheck(ifStmt.Cond)
	if !ok {
		return
	}

	if !c.assignsFromDatabase(ifStmt.Init, errVar) && !c.assignsFromDatabase(previous, errVar) {
		return
	}

	if c.callsSQLClassifier(ifStmt.Body) {
		return
	}

	detail, ok := c.sqlClassDetail(ifStmt.Body)
	if !ok {
		return
	}

	c.pass.Reportf(ifStmt.Pos(), "%s: %s drops the root error: %s", classSQLClass, function, detail)
}

func (c *checker) sqlClassDetail(body ast.Node) (string, bool) {
	if sentinel, ok := c.hardcodedSentinel(body); ok {
		return fmt.Sprintf("database error forced to %s, use %s.WrapQueryError", sentinel, c.sqlUtilityPackage), true
	}

	if raiser, ok := c.nonClassifyingRaiser(body); ok {
		return fmt.Sprintf("database error raised via %s, use %s.WrapQueryError", raiser, c.sqlUtilityPackage), true
	}

	return "", false
}

func (c *checker) nonClassifyingRaiser(body ast.Node) (string, bool) {
	raiser := ""

	ast.Inspect(body, func(n ast.Node) bool {
		if raiser != "" {
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		name := c.nonClassifyingRaiserName(pkg.Name, sel.Sel.Name)
		if name == "" {
			return true
		}

		raiser = name

		return false
	})

	return raiser, raiser != ""
}

func (c *checker) nonClassifyingRaiserName(pkg, fn string) string {
	switch pkg {
	case "gerror":
		if strings.HasPrefix(fn, "New") || strings.HasPrefix(fn, "Wrap") {
			return "gerror." + fn
		}
	case "fmt":
		if fn == "Errorf" {
			return "fmt.Errorf"
		}
	case c.domainPackage:
		if domainWrappers[fn] {
			return c.domainPackage + "." + fn
		}
	case c.assertPackage:
		if nonClassifyingUtilityFuncs[fn] {
			return c.assertPackage + "." + fn
		}
	}

	return ""
}

func (c *checker) assignsFromDatabase(stmt ast.Stmt, errVar string) bool {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || !assignsTo(assign, errVar) {
		return false
	}

	for _, rhs := range assign.Rhs {
		if c.isDatabaseCall(rhs) || c.isDaoCall(rhs) {
			return true
		}
	}

	return false
}

func (c *checker) isDaoCall(expr ast.Expr) bool {
	found := false

	inspectExcludingFuncLit(expr, func(n ast.Node) bool {
		if found {
			return false
		}

		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if ident, ok := rootIdent(sel.X); ok && ident.Name == c.daoPackage {
			found = true

			return false
		}

		return true
	})

	return found
}

func (c *checker) isDatabaseCall(expr ast.Expr) bool {
	found := false

	inspectExcludingFuncLit(expr, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if !databaseMethods[sel.Sel.Name] {
			return true
		}

		if isDatabaseReceiver(render(c.pass.Fset, sel.X)) {
			found = true

			return false
		}

		return true
	})

	return found
}

func isDatabaseReceiver(receiver string) bool {
	for _, name := range databaseReceivers {
		if receiver == name || strings.HasPrefix(receiver, name+".") {
			return true
		}
	}

	return false
}

func (c *checker) callsSQLClassifier(body ast.Node) bool {
	found := false

	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != c.sqlUtilityPackage {
			return true
		}

		if sel.Sel.Name == "WrapQueryError" || sel.Sel.Name == "Classify" || sqlClassifierPredicates[sel.Sel.Name] {
			found = true

			return false
		}

		return true
	})

	return found
}

func (c *checker) hardcodedSentinel(body ast.Node) (string, bool) {
	sentinel := ""

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !c.isDomainWrapper(call.Fun) || len(call.Args) == 0 {
			return true
		}

		sel, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "Err") {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != c.domainPackage {
			return true
		}

		sentinel = c.domainPackage + "." + sel.Sel.Name

		return false
	})

	return sentinel, sentinel != ""
}

func (c *checker) isDomainWrapper(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || !domainWrappers[sel.Sel.Name] {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == c.domainPackage
}
