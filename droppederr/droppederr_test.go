package droppederr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// parseFuncDecl parses src as a standalone Go file and returns its single
// top-level function declaration alongside the fset it was parsed with.
func parseFuncDecl(t *testing.T, src string) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "snippet.go", src, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			return fd, fset
		}
	}

	t.Fatal("snippet declares no function")

	return nil, nil
}

// parseExpr parses src as a standalone Go expression.
func parseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()

	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr() error = %v", err)
	}

	return expr
}

func TestNew(t *testing.T) {
	t.Run("wires the expected name and requires the inspector", func(t *testing.T) {
		a := New(Config{})

		if a.Name != "droppederr" {
			t.Fatalf("Name = %q, want droppederr", a.Name)
		}

		if len(a.Requires) != 1 {
			t.Fatalf("Requires = %v, want [inspect.Analyzer]", a.Requires)
		}
	})

	t.Run("binds every config field to a flag with the default value", func(t *testing.T) {
		a := New(Config{})

		f := a.Flags.Lookup("sql-utility-package")
		if f == nil || f.DefValue != "sql_utility" {
			t.Fatalf("sql-utility-package flag = %v, want default sql_utility", f)
		}
	})
}

func TestWithDefaults(t *testing.T) {
	t.Run("empty config gets every source-tool default", func(t *testing.T) {
		got := withDefaults(Config{})

		if got.SQLUtilityPackage != defaultSQLUtilityPackage {
			t.Fatalf("SQLUtilityPackage = %q, want %q", got.SQLUtilityPackage, defaultSQLUtilityPackage)
		}

		if got.DomainPackage != defaultDomainPackage {
			t.Fatalf("DomainPackage = %q, want %q", got.DomainPackage, defaultDomainPackage)
		}

		if got.DaoPackage != defaultDaoPackage {
			t.Fatalf("DaoPackage = %q, want %q", got.DaoPackage, defaultDaoPackage)
		}

		if got.AssertPackage != defaultAssertPackage {
			t.Fatalf("AssertPackage = %q, want %q", got.AssertPackage, defaultAssertPackage)
		}
	})

	t.Run("an already-set field is left untouched", func(t *testing.T) {
		got := withDefaults(Config{SQLUtilityPackage: "custom_sql"})

		if got.SQLUtilityPackage != "custom_sql" {
			t.Fatalf("SQLUtilityPackage = %q, want custom_sql", got.SQLUtilityPackage)
		}

		if got.DomainPackage != defaultDomainPackage {
			t.Fatalf("DomainPackage = %q, want %q", got.DomainPackage, defaultDomainPackage)
		}
	})

	t.Run("path fragments are not defaulted", func(t *testing.T) {
		got := withDefaults(Config{})

		if got.ExcludePaths != "" || got.SQLUtilityPaths != "" {
			t.Fatalf("ExcludePaths=%q SQLUtilityPaths=%q, want both empty", got.ExcludePaths, got.SQLUtilityPaths)
		}
	})
}

func TestContainsAny(t *testing.T) {
	t.Run("a present fragment matches", func(t *testing.T) {
		if !containsAny("internal/dao/entity.go", []string{"internal/dao/"}) {
			t.Fatal("containsAny() = false, want true")
		}
	})

	t.Run("an absent fragment does not match", func(t *testing.T) {
		if containsAny("src/invoice/service.go", []string{"internal/dao/"}) {
			t.Fatal("containsAny() = true, want false")
		}
	})

	t.Run("an empty fragment list matches nothing", func(t *testing.T) {
		if containsAny("src/invoice/service.go", nil) {
			t.Fatal("containsAny() = true, want false")
		}
	})
}

func TestSplitFragments(t *testing.T) {
	t.Run("empty string yields no fragments", func(t *testing.T) {
		if got := splitFragments(""); got != nil {
			t.Fatalf("splitFragments(\"\") = %v, want nil", got)
		}
	})

	t.Run("blank and whitespace-only entries are dropped", func(t *testing.T) {
		got := splitFragments("internal/dao/, ,  , src/gen/")

		want := []string{"internal/dao/", "src/gen/"}
		if len(got) != len(want) {
			t.Fatalf("splitFragments() = %v, want %v", got, want)
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitFragments()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		got := splitFragments("  internal/dao/  ")
		if len(got) != 1 || got[0] != "internal/dao/" {
			t.Fatalf("splitFragments() = %v, want [internal/dao/]", got)
		}
	})
}

func TestIsExcluded(t *testing.T) {
	c := &checker{excludePaths: []string{"internal/dao/"}}

	t.Run("a matching file is excluded", func(t *testing.T) {
		if !c.isExcluded("internal/dao/invoice.go") {
			t.Fatal("isExcluded() = false, want true")
		}
	})

	t.Run("a non-matching file is not excluded", func(t *testing.T) {
		if c.isExcluded("src/invoice/service.go") {
			t.Fatal("isExcluded() = true, want false")
		}
	})
}

func TestIsSQLUtilityPath(t *testing.T) {
	c := &checker{sqlUtilityPaths: []string{"src/utility/sql_utility/"}}

	t.Run("a matching file is a sql utility path", func(t *testing.T) {
		if !c.isSQLUtilityPath("src/utility/sql_utility/query.go") {
			t.Fatal("isSQLUtilityPath() = false, want true")
		}
	})

	t.Run("a non-matching file is not", func(t *testing.T) {
		if c.isSQLUtilityPath("src/invoice/repository.go") {
			t.Fatal("isSQLUtilityPath() = true, want false")
		}
	})
}

func TestErrNilCheck(t *testing.T) {
	t.Run("err != nil reports the variable name", func(t *testing.T) {
		name, ok := errNilCheck(parseExpr(t, "err != nil"))
		if !ok || name != "err" {
			t.Fatalf("errNilCheck() = %q, %v, want \"err\", true", name, ok)
		}
	})

	t.Run("err == nil is not a not-nil check", func(t *testing.T) {
		if _, ok := errNilCheck(parseExpr(t, "err == nil")); ok {
			t.Fatal("errNilCheck() reported ok for ==")
		}
	})

	t.Run("a differently-named error variable still matches", func(t *testing.T) {
		name, ok := errNilCheck(parseExpr(t, "scanErr != nil"))
		if !ok || name != "scanErr" {
			t.Fatalf("errNilCheck() = %q, %v, want \"scanErr\", true", name, ok)
		}
	})

	t.Run("an identifier without err in its name does not match", func(t *testing.T) {
		if _, ok := errNilCheck(parseExpr(t, "x != nil")); ok {
			t.Fatal("errNilCheck() reported ok for x != nil")
		}
	})

	t.Run("a non-nil comparison does not match", func(t *testing.T) {
		if _, ok := errNilCheck(parseExpr(t, "err != 0")); ok {
			t.Fatal("errNilCheck() reported ok for err != 0")
		}
	})
}

func TestIdentUsed(t *testing.T) {
	t.Run("a referenced identifier is used", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() {
	if err != nil {
		return err
	}
}
`)
		ifStmt := fd.Body.List[0].(*ast.IfStmt)

		if !identUsed(ifStmt.Body, "err") {
			t.Fatal("identUsed() = false, want true")
		}
	})

	t.Run("an unreferenced identifier is not used", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() {
	if err != nil {
		return errors.New("boom")
	}
}
`)
		ifStmt := fd.Body.List[0].(*ast.IfStmt)

		if identUsed(ifStmt.Body, "err") {
			t.Fatal("identUsed() = true, want false")
		}
	})
}

func TestFirstLine(t *testing.T) {
	t.Run("braces and blank lines are skipped", func(t *testing.T) {
		if got := firstLine("{\n\treturn err\n}"); got != "return err" {
			t.Fatalf("firstLine() = %q, want %q", got, "return err")
		}
	})

	t.Run("an empty block yields the placeholder", func(t *testing.T) {
		if got := firstLine("{\n}"); got != "<empty block>" {
			t.Fatalf("firstLine() = %q, want <empty block>", got)
		}
	})

	t.Run("a line longer than 80 characters is truncated", func(t *testing.T) {
		long := "return errors.New(\"this is a deliberately long error message padded out past eighty characters\")"

		got := firstLine("{\n\t" + long + "\n}")
		if len(got) != 83 || got[len(got)-3:] != "..." {
			t.Fatalf("firstLine() = %q (len %d), want 80 chars + \"...\"", got, len(got))
		}
	})
}

func TestAssignsTo(t *testing.T) {
	fd, _ := parseFuncDecl(t, `package p
func f() {
	x, err := g()
}
`)
	assign := fd.Body.List[0].(*ast.AssignStmt)

	t.Run("a named lhs matches", func(t *testing.T) {
		if !assignsTo(assign, "err") {
			t.Fatal("assignsTo() = false, want true")
		}
	})

	t.Run("an unnamed lhs does not match", func(t *testing.T) {
		if assignsTo(assign, "y") {
			t.Fatal("assignsTo() = true, want false")
		}
	})
}

func TestRootIdent(t *testing.T) {
	t.Run("a bare identifier is its own root", func(t *testing.T) {
		id, ok := rootIdent(parseExpr(t, "a"))
		if !ok || id.Name != "a" {
			t.Fatalf("rootIdent() = %v, %v, want a, true", id, ok)
		}
	})

	t.Run("a selector chain resolves to its leftmost identifier", func(t *testing.T) {
		id, ok := rootIdent(parseExpr(t, "a.b.c"))
		if !ok || id.Name != "a" {
			t.Fatalf("rootIdent() = %v, %v, want a, true", id, ok)
		}
	})

	t.Run("a call expression resolves through its callee", func(t *testing.T) {
		id, ok := rootIdent(parseExpr(t, "a.b()"))
		if !ok || id.Name != "a" {
			t.Fatalf("rootIdent() = %v, %v, want a, true", id, ok)
		}
	})

	t.Run("an index expression resolves through its operand", func(t *testing.T) {
		id, ok := rootIdent(parseExpr(t, "a[0]"))
		if !ok || id.Name != "a" {
			t.Fatalf("rootIdent() = %v, %v, want a, true", id, ok)
		}
	})

	t.Run("parentheses are transparent", func(t *testing.T) {
		id, ok := rootIdent(parseExpr(t, "(a)"))
		if !ok || id.Name != "a" {
			t.Fatalf("rootIdent() = %v, %v, want a, true", id, ok)
		}
	})

	t.Run("an unsupported expression has no root", func(t *testing.T) {
		if _, ok := rootIdent(parseExpr(t, "a + b")); ok {
			t.Fatal("rootIdent() reported ok for a binary expression")
		}
	})
}

func TestBlockStatements(t *testing.T) {
	t.Run("a block statement returns its list", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() {
	x := 1
	_ = x
}
`)
		stmts, ok := blockStatements(fd.Body)
		if !ok || len(stmts) != 2 {
			t.Fatalf("blockStatements() = %v, %v, want 2 statements, true", stmts, ok)
		}
	})

	t.Run("a case clause returns its body", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() {
	switch 1 {
	case 1:
		x := 1
		_ = x
	}
}
`)
		sw := fd.Body.List[0].(*ast.SwitchStmt)
		cc := sw.Body.List[0].(*ast.CaseClause)

		stmts, ok := blockStatements(cc)
		if !ok || len(stmts) != 2 {
			t.Fatalf("blockStatements() = %v, %v, want 2 statements, true", stmts, ok)
		}
	})

	t.Run("a comm clause returns its body", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f(c chan int) {
	select {
	case <-c:
		x := 1
		_ = x
	}
}
`)
		sel := fd.Body.List[0].(*ast.SelectStmt)
		cc := sel.Body.List[0].(*ast.CommClause)

		stmts, ok := blockStatements(cc)
		if !ok || len(stmts) != 2 {
			t.Fatalf("blockStatements() = %v, %v, want 2 statements, true", stmts, ok)
		}
	})

	t.Run("an unsupported node reports false", func(t *testing.T) {
		if _, ok := blockStatements(ast.NewIdent("x")); ok {
			t.Fatal("blockStatements() reported ok for an identifier")
		}
	})
}

func TestRender(t *testing.T) {
	fd, fset := parseFuncDecl(t, `package p
func f() {
	_ = r.ext.QueryxContext
}
`)
	assign := fd.Body.List[0].(*ast.AssignStmt)
	sel := assign.Rhs[0].(*ast.SelectorExpr)

	t.Run("a selector chain renders back to its source text", func(t *testing.T) {
		if got := render(fset, sel.X); got != "r.ext" {
			t.Fatalf("render() = %q, want r.ext", got)
		}
	})
}

func TestNonClassifyingRaiserName(t *testing.T) {
	c := &checker{domainPackage: "domain", assertPackage: "utility"}

	t.Run("gerror.New* matches", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("gerror", "Newf"); got != "gerror.Newf" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want gerror.Newf", got)
		}
	})

	t.Run("gerror.Wrap* matches", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("gerror", "WrapCode"); got != "gerror.WrapCode" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want gerror.WrapCode", got)
		}
	})

	t.Run("a gerror func with neither prefix does not match", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("gerror", "Code"); got != "" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want empty", got)
		}
	})

	t.Run("fmt.Errorf matches", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("fmt", "Errorf"); got != "fmt.Errorf" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want fmt.Errorf", got)
		}
	})

	t.Run("another fmt func does not match", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("fmt", "Println"); got != "" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want empty", got)
		}
	})

	t.Run("a configured domain wrapper matches", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("domain", "WrapError"); got != "domain.WrapError" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want domain.WrapError", got)
		}
	})

	t.Run("an unlisted domain func does not match", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("domain", "Something"); got != "" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want empty", got)
		}
	})

	t.Run("a configured assert helper matches", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("utility", "AssertError"); got != "utility.AssertError" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want utility.AssertError", got)
		}
	})

	t.Run("an unlisted utility func does not match", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("utility", "Something"); got != "" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want empty", got)
		}
	})

	t.Run("an unrelated package does not match", func(t *testing.T) {
		if got := c.nonClassifyingRaiserName("strings", "Errorf"); got != "" {
			t.Fatalf("nonClassifyingRaiserName() = %q, want empty", got)
		}
	})
}

func TestNonClassifyingRaiser(t *testing.T) {
	c := &checker{domainPackage: "domain", assertPackage: "utility"}

	t.Run("a gerror call is found", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return gerror.Newf("boom: %v", err)
}
`)
		got, ok := c.nonClassifyingRaiser(fd.Body)
		if !ok || got != "gerror.Newf" {
			t.Fatalf("nonClassifyingRaiser() = %q, %v, want gerror.Newf, true", got, ok)
		}
	})

	t.Run("a configured domain wrapper is found", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return domain.WrapError(err)
}
`)
		got, ok := c.nonClassifyingRaiser(fd.Body)
		if !ok || got != "domain.WrapError" {
			t.Fatalf("nonClassifyingRaiser() = %q, %v, want domain.WrapError, true", got, ok)
		}
	})

	t.Run("a sql utility call is not a raiser", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return sql_utility.WrapQueryError(err)
}
`)
		if _, ok := c.nonClassifyingRaiser(fd.Body); ok {
			t.Fatal("nonClassifyingRaiser() reported ok for sql_utility.WrapQueryError")
		}
	})

	t.Run("a body with no call has no raiser", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return err
}
`)
		if _, ok := c.nonClassifyingRaiser(fd.Body); ok {
			t.Fatal("nonClassifyingRaiser() reported ok for a bare return")
		}
	})
}

func TestIsDatabaseReceiver(t *testing.T) {
	t.Run("an exact match is a database receiver", func(t *testing.T) {
		if !isDatabaseReceiver("tx") {
			t.Fatal("isDatabaseReceiver() = false, want true")
		}
	})

	t.Run("a dotted prefix is a database receiver", func(t *testing.T) {
		if !isDatabaseReceiver("sqlx.Named") {
			t.Fatal("isDatabaseReceiver() = false, want true")
		}
	})

	t.Run("an unrelated receiver is not", func(t *testing.T) {
		if isDatabaseReceiver("other") {
			t.Fatal("isDatabaseReceiver() = true, want false")
		}
	})
}

func TestIsDomainWrapper(t *testing.T) {
	c := &checker{domainPackage: "domain"}

	t.Run("a configured domain wrapper matches", func(t *testing.T) {
		if !c.isDomainWrapper(parseExpr(t, "domain.WrapError")) {
			t.Fatal("isDomainWrapper() = false, want true")
		}
	})

	t.Run("an unlisted domain func does not match", func(t *testing.T) {
		if c.isDomainWrapper(parseExpr(t, "domain.NotAWrapper")) {
			t.Fatal("isDomainWrapper() = true, want false")
		}
	})

	t.Run("a wrapper name from another package does not match", func(t *testing.T) {
		if c.isDomainWrapper(parseExpr(t, "otherpkg.WrapError")) {
			t.Fatal("isDomainWrapper() = true, want false")
		}
	})

	t.Run("a non-selector expression does not match", func(t *testing.T) {
		if c.isDomainWrapper(parseExpr(t, "f")) {
			t.Fatal("isDomainWrapper() = true, want false")
		}
	})
}

func TestHardcodedSentinel(t *testing.T) {
	c := &checker{domainPackage: "domain"}

	t.Run("a domain sentinel passed to a wrapper is found", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return domain.WrapError(domain.ErrNotFound, err)
}
`)
		got, ok := c.hardcodedSentinel(fd.Body)
		if !ok || got != "domain.ErrNotFound" {
			t.Fatalf("hardcodedSentinel() = %q, %v, want domain.ErrNotFound, true", got, ok)
		}
	})

	t.Run("a wrapper with no sentinel argument is not found", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return domain.WrapError(err)
}
`)
		if _, ok := c.hardcodedSentinel(fd.Body); ok {
			t.Fatal("hardcodedSentinel() reported ok with no sentinel argument")
		}
	})

	t.Run("a body with no wrapper call is not found", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return err
}
`)
		if _, ok := c.hardcodedSentinel(fd.Body); ok {
			t.Fatal("hardcodedSentinel() reported ok for a bare return")
		}
	})
}

func TestCallsSQLClassifier(t *testing.T) {
	c := &checker{sqlUtilityPackage: "sql_utility"}

	t.Run("WrapQueryError classifies", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return sql_utility.WrapQueryError(err)
}
`)
		if !c.callsSQLClassifier(fd.Body) {
			t.Fatal("callsSQLClassifier() = false, want true")
		}
	})

	t.Run("a configured Is* predicate classifies", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	if sql_utility.IsDuplicateKey(err) {
		return domain.Error(domain.ErrConflict, "dup")
	}
	return err
}
`)
		if !c.callsSQLClassifier(fd.Body) {
			t.Fatal("callsSQLClassifier() = false, want true")
		}
	})

	t.Run("a non-classifying call does not classify", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return domain.WrapError(err)
}
`)
		if c.callsSQLClassifier(fd.Body) {
			t.Fatal("callsSQLClassifier() = true, want false")
		}
	})
}

func TestSQLClassDetail(t *testing.T) {
	c := &checker{domainPackage: "domain", sqlUtilityPackage: "sql_utility"}

	t.Run("a hardcoded sentinel produces a forced-sentinel detail", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return domain.WrapError(domain.ErrNotFound, err)
}
`)
		got, ok := c.sqlClassDetail(fd.Body)
		want := "database error forced to domain.ErrNotFound, use sql_utility.WrapQueryError"

		if !ok || got != want {
			t.Fatalf("sqlClassDetail() = %q, %v, want %q, true", got, ok, want)
		}
	})

	t.Run("a non-classifying raiser produces a raised-via detail", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return gerror.Newf("boom: %v", err)
}
`)
		got, ok := c.sqlClassDetail(fd.Body)
		want := "database error raised via gerror.Newf, use sql_utility.WrapQueryError"

		if !ok || got != want {
			t.Fatalf("sqlClassDetail() = %q, %v, want %q, true", got, ok, want)
		}
	})

	t.Run("neither shape yields no detail", func(t *testing.T) {
		fd, _ := parseFuncDecl(t, `package p
func f() error {
	return err
}
`)
		if _, ok := c.sqlClassDetail(fd.Body); ok {
			t.Fatal("sqlClassDetail() reported ok for a bare return")
		}
	})
}

func TestIsDaoCall(t *testing.T) {
	c := &checker{daoPackage: "dao"}

	t.Run("a direct dao call is found", func(t *testing.T) {
		if !c.isDaoCall(parseExpr(t, "dao.Invoice.Ctx(ctx).Insert(one)")) {
			t.Fatal("isDaoCall() = false, want true")
		}
	})

	t.Run("a dao call nested in a func literal is ignored", func(t *testing.T) {
		expr := parseExpr(t, `someWrapper(func() error {
	return dao.Invoice.Ctx(ctx).Insert(one)
})`)

		if c.isDaoCall(expr) {
			t.Fatal("isDaoCall() = true, want false")
		}
	})

	t.Run("a call on another package is not a dao call", func(t *testing.T) {
		if c.isDaoCall(parseExpr(t, "other.Invoice.Ctx(ctx).Insert(one)")) {
			t.Fatal("isDaoCall() = true, want false")
		}
	})
}

func TestIsDatabaseCall(t *testing.T) {
	t.Run("a configured receiver calling a database method is found", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	_, err = r.ext.QueryxContext(ctx, "select 1")
}
`)
		assign := fd.Body.List[0].(*ast.AssignStmt)
		c := &checker{pass: &analysis.Pass{Fset: fset}}

		if !c.isDatabaseCall(assign.Rhs[0]) {
			t.Fatal("isDatabaseCall() = false, want true")
		}
	})

	t.Run("an unrelated receiver is not a database call", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	_, err = other.QueryxContext(ctx, "select 1")
}
`)
		assign := fd.Body.List[0].(*ast.AssignStmt)
		c := &checker{pass: &analysis.Pass{Fset: fset}}

		if c.isDatabaseCall(assign.Rhs[0]) {
			t.Fatal("isDatabaseCall() = true, want false")
		}
	})

	t.Run("a database call nested in a func literal is ignored", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	err = someWrapper(func() error {
		_, err := r.ext.QueryxContext(ctx, "select 1")
		return err
	})
}
`)
		assign := fd.Body.List[0].(*ast.AssignStmt)
		c := &checker{pass: &analysis.Pass{Fset: fset}}

		if c.isDatabaseCall(assign.Rhs[0]) {
			t.Fatal("isDatabaseCall() = true, want false")
		}
	})
}

func TestAssignsFromDatabase(t *testing.T) {
	t.Run("a dao assignment matches", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	_, err := dao.Invoice.Ctx(ctx).Insert(one)
}
`)
		c := &checker{daoPackage: "dao", pass: &analysis.Pass{Fset: fset}}

		if !c.assignsFromDatabase(fd.Body.List[0], "err") {
			t.Fatal("assignsFromDatabase() = false, want true")
		}
	})

	t.Run("a sqlx receiver assignment matches", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	_, err := r.ext.QueryxContext(ctx, "select 1")
}
`)
		c := &checker{daoPackage: "dao", pass: &analysis.Pass{Fset: fset}}

		if !c.assignsFromDatabase(fd.Body.List[0], "err") {
			t.Fatal("assignsFromDatabase() = false, want true")
		}
	})

	t.Run("an unrelated assignment does not match", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	_, err := other.Get()
}
`)
		c := &checker{daoPackage: "dao", pass: &analysis.Pass{Fset: fset}}

		if c.assignsFromDatabase(fd.Body.List[0], "err") {
			t.Fatal("assignsFromDatabase() = true, want false")
		}
	})

	t.Run("an assignment to a different variable does not match", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	_, err2 := dao.Invoice.Ctx(ctx).Insert(one)
}
`)
		c := &checker{daoPackage: "dao", pass: &analysis.Pass{Fset: fset}}

		if c.assignsFromDatabase(fd.Body.List[0], "err") {
			t.Fatal("assignsFromDatabase() = true, want false")
		}
	})

	t.Run("a non-assignment statement does not match", func(t *testing.T) {
		fd, fset := parseFuncDecl(t, `package p
func f() {
	doSomething()
}
`)
		c := &checker{daoPackage: "dao", pass: &analysis.Pass{Fset: fset}}

		if c.assignsFromDatabase(fd.Body.List[0], "err") {
			t.Fatal("assignsFromDatabase() = true, want false")
		}
	})
}
