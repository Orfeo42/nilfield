package plugin

import (
	"testing"

	"github.com/golangci/plugin-module-register/register"
)

func TestNewDroppedErr(t *testing.T) {
	t.Run("valid settings decode and configure the analyzer", func(t *testing.T) {
		p, err := NewDroppedErr(map[string]any{
			"exclude-paths":     "internal/dao/",
			"sql-utility-paths": "src/utility/sql_utility/",
		})
		if err != nil {
			t.Fatalf("NewDroppedErr() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		if len(analyzers) != 1 {
			t.Fatalf("BuildAnalyzers() returned %d analyzers, want 1", len(analyzers))
		}

		flag := analyzers[0].Flags.Lookup("exclude-paths")
		if flag == nil {
			t.Fatalf("exclude-paths flag not found")
		}

		if flag.Value.String() != "internal/dao/" {
			t.Fatalf("exclude-paths flag = %q, want %q", flag.Value.String(), "internal/dao/")
		}

		flag = analyzers[0].Flags.Lookup("sql-utility-paths")
		if flag == nil {
			t.Fatalf("sql-utility-paths flag not found")
		}

		if flag.Value.String() != "src/utility/sql_utility/" {
			t.Fatalf("sql-utility-paths flag = %q, want %q", flag.Value.String(), "src/utility/sql_utility/")
		}
	})

	t.Run("an unset package selector keeps the analyzer default", func(t *testing.T) {
		p, err := NewDroppedErr(nil)
		if err != nil {
			t.Fatalf("NewDroppedErr() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		flag := analyzers[0].Flags.Lookup("sql-utility-package")
		if flag == nil {
			t.Fatalf("sql-utility-package flag not found")
		}

		if flag.Value.String() != "sql_utility" {
			t.Fatalf("sql-utility-package flag = %q, want %q", flag.Value.String(), "sql_utility")
		}
	})

	t.Run("a set package selector overrides the default", func(t *testing.T) {
		p, err := NewDroppedErr(map[string]any{"domain-package": "errs"})
		if err != nil {
			t.Fatalf("NewDroppedErr() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		flag := analyzers[0].Flags.Lookup("domain-package")
		if flag == nil {
			t.Fatalf("domain-package flag not found")
		}

		if flag.Value.String() != "errs" {
			t.Fatalf("domain-package flag = %q, want %q", flag.Value.String(), "errs")
		}
	})

	t.Run("unknown key returns an error", func(t *testing.T) {
		_, err := NewDroppedErr(map[string]any{"unknown-key": "value"})
		if err == nil {
			t.Fatalf("NewDroppedErr() error = nil, want error")
		}
	})
}

func TestDroppedErrPluginBuildAnalyzers(t *testing.T) {
	t.Run("returns exactly one analyzer named droppederr", func(t *testing.T) {
		p, err := NewDroppedErr(nil)
		if err != nil {
			t.Fatalf("NewDroppedErr() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		if len(analyzers) != 1 {
			t.Fatalf("BuildAnalyzers() returned %d analyzers, want 1", len(analyzers))
		}

		if analyzers[0].Name != "droppederr" {
			t.Fatalf("analyzer name = %q, want %q", analyzers[0].Name, "droppederr")
		}
	})
}

func TestDroppedErrPluginGetLoadMode(t *testing.T) {
	t.Run("returns LoadModeSyntax", func(t *testing.T) {
		p, err := NewDroppedErr(nil)
		if err != nil {
			t.Fatalf("NewDroppedErr() error = %v, want nil", err)
		}

		if got := p.GetLoadMode(); got != register.LoadModeSyntax {
			t.Fatalf("GetLoadMode() = %q, want %q", got, register.LoadModeSyntax)
		}
	})
}
