// Package plugin exists only for the golangci-lint module plugin system.
// Consumers reach it through their own .custom-gcl.yml, which builds a
// custom-gcl binary with this package's init registration compiled in.
// The standalone binary in cmd/nilfield does not import it.
package plugin

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/Orfeo42/nilfield/analyzer"
)

func init() {
	register.Plugin("nilfield", New)
}

// Settings mirrors analyzer.Config for golangci-lint's settings decoding.
type Settings struct {
	ExcludePaths string `json:"exclude-paths"`
}

// New decodes settings and builds the nilfield linter plugin.
func New(settings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[Settings](settings)
	if err != nil {
		return nil, err
	}

	return &nilfieldPlugin{settings: s}, nil
}

type nilfieldPlugin struct {
	settings Settings
}

// BuildAnalyzers returns the nilfield analyzer configured from the plugin settings.
func (p *nilfieldPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		analyzer.New(analyzer.Config{ExcludePaths: p.settings.ExcludePaths}),
	}, nil
}

// GetLoadMode reports that nilfield needs resolved type information: it
// resolves field types, looks up methods and reads types.Info.
func (p *nilfieldPlugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
