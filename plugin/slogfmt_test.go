package plugin

import (
	"testing"

	"github.com/golangci/plugin-module-register/register"
)

func TestNewSlogfmt(t *testing.T) {
	t.Run("valid settings decode and configure the analyzer", func(t *testing.T) {
		p, err := NewSlogfmt(map[string]any{"exclude-paths": "internal/dao/"})
		if err != nil {
			t.Fatalf("NewSlogfmt() error = %v, want nil", err)
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
	})

	t.Run("nil settings succeed with an empty exclude-paths", func(t *testing.T) {
		p, err := NewSlogfmt(nil)
		if err != nil {
			t.Fatalf("NewSlogfmt() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		flag := analyzers[0].Flags.Lookup("exclude-paths")
		if flag == nil {
			t.Fatalf("exclude-paths flag not found")
		}

		if flag.Value.String() != "" {
			t.Fatalf("exclude-paths flag = %q, want empty", flag.Value.String())
		}
	})

	t.Run("unknown key returns an error", func(t *testing.T) {
		_, err := NewSlogfmt(map[string]any{"unknown-key": "value"})
		if err == nil {
			t.Fatalf("NewSlogfmt() error = nil, want error")
		}
	})
}

func TestSlogfmtPluginBuildAnalyzers(t *testing.T) {
	t.Run("returns exactly one analyzer named slogfmt", func(t *testing.T) {
		p, err := NewSlogfmt(nil)
		if err != nil {
			t.Fatalf("NewSlogfmt() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		if len(analyzers) != 1 {
			t.Fatalf("BuildAnalyzers() returned %d analyzers, want 1", len(analyzers))
		}

		if analyzers[0].Name != "slogfmt" {
			t.Fatalf("analyzer name = %q, want %q", analyzers[0].Name, "slogfmt")
		}
	})
}

func TestSlogfmtPluginGetLoadMode(t *testing.T) {
	t.Run("returns LoadModeSyntax", func(t *testing.T) {
		p, err := NewSlogfmt(nil)
		if err != nil {
			t.Fatalf("NewSlogfmt() error = %v, want nil", err)
		}

		if got := p.GetLoadMode(); got != register.LoadModeSyntax {
			t.Fatalf("GetLoadMode() = %q, want %q", got, register.LoadModeSyntax)
		}
	})
}

func TestPluginRegistration(t *testing.T) {
	t.Run("nilfield registers under its own name", func(t *testing.T) {
		if _, err := register.GetPlugin("nilfield"); err != nil {
			t.Fatalf("GetPlugin(\"nilfield\") error = %v, want nil", err)
		}
	})

	t.Run("droppederr registers under its own name", func(t *testing.T) {
		if _, err := register.GetPlugin("droppederr"); err != nil {
			t.Fatalf("GetPlugin(\"droppederr\") error = %v, want nil", err)
		}
	})

	t.Run("slogfmt registers under its own name", func(t *testing.T) {
		if _, err := register.GetPlugin("slogfmt"); err != nil {
			t.Fatalf("GetPlugin(\"slogfmt\") error = %v, want nil", err)
		}
	})
}
