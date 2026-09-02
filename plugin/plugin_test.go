package plugin

import (
	"testing"

	"github.com/golangci/plugin-module-register/register"
)

func TestNew(t *testing.T) {
	t.Run("valid settings decode and configure the analyzer", func(t *testing.T) {
		p, err := New(map[string]any{"exclude-paths": "internal/dao/"})
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
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

	t.Run("unknown key returns an error", func(t *testing.T) {
		_, err := New(map[string]any{"unknown-key": "value"})
		if err == nil {
			t.Fatalf("New() error = nil, want error")
		}
	})

	t.Run("nil settings succeed with an empty exclude-paths", func(t *testing.T) {
		p, err := New(nil)
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
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

	t.Run("empty settings succeed with an empty exclude-paths", func(t *testing.T) {
		p, err := New(map[string]any{})
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
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
}

func TestBuildAnalyzers(t *testing.T) {
	t.Run("returns exactly one analyzer named nilfield", func(t *testing.T) {
		p, err := New(nil)
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
		}

		analyzers, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatalf("BuildAnalyzers() error = %v, want nil", err)
		}

		if len(analyzers) != 1 {
			t.Fatalf("BuildAnalyzers() returned %d analyzers, want 1", len(analyzers))
		}

		if analyzers[0].Name != "nilfield" {
			t.Fatalf("analyzer name = %q, want %q", analyzers[0].Name, "nilfield")
		}
	})
}

func TestGetLoadMode(t *testing.T) {
	t.Run("returns LoadModeTypesInfo", func(t *testing.T) {
		p, err := New(nil)
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
		}

		if got := p.GetLoadMode(); got != register.LoadModeTypesInfo {
			t.Fatalf("GetLoadMode() = %q, want %q", got, register.LoadModeTypesInfo)
		}
	})
}
