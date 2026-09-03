package analyzer

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestModuleScope(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), New(Config{}), "modulescope")
}

func TestFieldInModule(t *testing.T) {
	t.Run("field package equal to module path is in module", func(t *testing.T) {
		if !fieldInModule("example.com/proj", &analysis.Module{Path: "example.com/proj"}) {
			t.Fatalf("fieldInModule() = false, want true")
		}
	})

	t.Run("field package nested under module path is in module", func(t *testing.T) {
		if !fieldInModule("example.com/proj/internal/foo", &analysis.Module{Path: "example.com/proj"}) {
			t.Fatalf("fieldInModule() = false, want true")
		}
	})

	t.Run("field package outside module path is out of module", func(t *testing.T) {
		if fieldInModule("github.com/gogf/gf/v2/net/ghttp", &analysis.Module{Path: "example.com/proj"}) {
			t.Fatalf("fieldInModule() = true, want false")
		}
	})

	t.Run("field package sharing a string prefix without a path boundary is out of module", func(t *testing.T) {
		if fieldInModule("example.com/projector", &analysis.Module{Path: "example.com/proj"}) {
			t.Fatalf("fieldInModule() = true, want false")
		}
	})

	t.Run("nil module decides in module so it reports as today", func(t *testing.T) {
		if !fieldInModule("github.com/gogf/gf/v2/net/ghttp", nil) {
			t.Fatalf("fieldInModule() = false, want true")
		}
	})

	t.Run("module with an empty path decides in module so it reports as today", func(t *testing.T) {
		if !fieldInModule("github.com/gogf/gf/v2/net/ghttp", &analysis.Module{Path: ""}) {
			t.Fatalf("fieldInModule() = false, want true")
		}
	})
}
