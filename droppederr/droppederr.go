// Package droppederr reports error-handling sites where the root cause never
// reaches the logs.
//
// Two classes are reported:
//
//	DROPPED  an `if err != nil { ... }` block whose body never references err,
//	         so the root cause is discarded before any boundary can log it.
//	SQLCLASS an error branch guarding a database call (a configured sqlx
//	         receiver, or a configured dao package call anywhere) that raises
//	         through anything other than the configured SQL utility package's
//	         WrapQueryError/Classify, or a guard that branches on one of that
//	         package's Is* predicates before raising — a hardcoded domain
//	         sentinel, gerror.New*/Wrap*, fmt.Errorf, a configured domain
//	         wrapper, or a configured assert helper — so duplicate key, FK
//	         violation, deadlock and data errors all collapse to one class.
//	         Database-call detection only looks at the outermost call chain of
//	         the assignment; calls nested inside a function literal (e.g. a
//	         retry/transaction closure) are ignored, since their error is
//	         already propagated out through the closure's own return.
//
// Every finding is always reported; there is no baseline.
package droppederr

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	classDropped  = "DROPPED"
	classSQLClass = "SQLCLASS"
)

const (
	defaultSQLUtilityPackage = "sql_utility"
	defaultDomainPackage     = "domain"
	defaultDaoPackage        = "dao"
	defaultAssertPackage     = "utility"
)

// Config holds the flags New wires onto the analyzer it builds.
type Config struct {
	ExcludePaths      string
	SQLUtilityPaths   string
	SQLUtilityPackage string
	DomainPackage     string
	DaoPackage        string
	AssertPackage     string
}

// New builds a droppederr analyzer configured with cfg, whose fields are also
// exposed as flags so a driver like singlechecker can still override them
// from the command line.
func New(cfg Config) *analysis.Analyzer {
	cfg = withDefaults(cfg)

	a := &analysis.Analyzer{
		Name:     "droppederr",
		Doc:      "reports error-handling sites where the root cause never reaches the logs",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}

	a.Flags.StringVar(&cfg.ExcludePaths, "exclude-paths", cfg.ExcludePaths,
		"comma-separated path fragments; a file whose slash-normalized path contains one is never reported")
	a.Flags.StringVar(&cfg.SQLUtilityPaths, "sql-utility-paths", cfg.SQLUtilityPaths,
		"comma-separated path fragments; a file whose slash-normalized path contains one is exempt from the SQLCLASS class only")
	a.Flags.StringVar(&cfg.SQLUtilityPackage, "sql-utility-package", cfg.SQLUtilityPackage,
		"package selector whose WrapQueryError/Classify/Is* predicates count as classifying")
	a.Flags.StringVar(&cfg.DomainPackage, "domain-package", cfg.DomainPackage,
		"package selector for the Error/WrapError/... wrappers and the Err* sentinels")
	a.Flags.StringVar(&cfg.DaoPackage, "dao-package", cfg.DaoPackage,
		"package selector whose calls count as database calls")
	a.Flags.StringVar(&cfg.AssertPackage, "assert-package", cfg.AssertPackage,
		"package selector for AssertError/AssertErrorWithCode")

	a.Run = func(pass *analysis.Pass) (any, error) {
		return run(pass, cfg)
	}

	return a
}

// Analyzer is the default droppederr analyzer, with every package selector at
// its source-tool default beyond what flags set at runtime.
var Analyzer = New(Config{})

// withDefaults fills every unset package-selector field of cfg with the value
// the source tool hardcoded, so the flags New binds show the same defaults.
func withDefaults(cfg Config) Config {
	if cfg.SQLUtilityPackage == "" {
		cfg.SQLUtilityPackage = defaultSQLUtilityPackage
	}

	if cfg.DomainPackage == "" {
		cfg.DomainPackage = defaultDomainPackage
	}

	if cfg.DaoPackage == "" {
		cfg.DaoPackage = defaultDaoPackage
	}

	if cfg.AssertPackage == "" {
		cfg.AssertPackage = defaultAssertPackage
	}

	return cfg
}

type checker struct {
	pass              *analysis.Pass
	excludePaths      []string
	sqlUtilityPaths   []string
	sqlUtilityPackage string
	domainPackage     string
	daoPackage        string
	assertPackage     string
}

func run(pass *analysis.Pass, cfg Config) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	c := &checker{
		pass:              pass,
		excludePaths:      splitFragments(cfg.ExcludePaths),
		sqlUtilityPaths:   splitFragments(cfg.SQLUtilityPaths),
		sqlUtilityPackage: cfg.SQLUtilityPackage,
		domainPackage:     cfg.DomainPackage,
		daoPackage:        cfg.DaoPackage,
		assertPackage:     cfg.AssertPackage,
	}

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		fd := n.(*ast.FuncDecl)
		if fd.Body == nil {
			return
		}

		filename := filepath.ToSlash(pass.Fset.Position(fd.Pos()).Filename)
		if c.isExcluded(filename) {
			return
		}

		c.scanDropped(fd)

		if c.isSQLUtilityPath(filename) {
			return
		}

		c.scanSQLClassification(fd)
	})

	return nil, nil
}

func (c *checker) isExcluded(filename string) bool {
	return containsAny(filename, c.excludePaths)
}

func (c *checker) isSQLUtilityPath(filename string) bool {
	return containsAny(filename, c.sqlUtilityPaths)
}

func containsAny(filename string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(filename, fragment) {
			return true
		}
	}

	return false
}

// splitFragments turns a comma-separated flag value into its trimmed,
// slash-normalized, non-empty parts.
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
