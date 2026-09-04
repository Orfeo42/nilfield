package plugin

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/Orfeo42/nilfield/droppederr"
)

func init() {
	register.Plugin("droppederr", NewDroppedErr)
}

// DroppedErrSettings mirrors droppederr.Config for golangci-lint's settings decoding.
type DroppedErrSettings struct {
	ExcludePaths      string `json:"exclude-paths"`
	SQLUtilityPaths   string `json:"sql-utility-paths"`
	SQLUtilityPackage string `json:"sql-utility-package"`
	DomainPackage     string `json:"domain-package"`
	DaoPackage        string `json:"dao-package"`
	AssertPackage     string `json:"assert-package"`
}

// NewDroppedErr decodes settings and builds the droppederr linter plugin.
func NewDroppedErr(settings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[DroppedErrSettings](settings)
	if err != nil {
		return nil, err
	}

	return &droppedErrPlugin{settings: s}, nil
}

type droppedErrPlugin struct {
	settings DroppedErrSettings
}

// BuildAnalyzers returns the droppederr analyzer configured from the plugin
// settings. An unset package selector stays empty here and New fills it with
// the analyzer's own default.
func (p *droppedErrPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		droppederr.New(droppederr.Config{
			ExcludePaths:      p.settings.ExcludePaths,
			SQLUtilityPaths:   p.settings.SQLUtilityPaths,
			SQLUtilityPackage: p.settings.SQLUtilityPackage,
			DomainPackage:     p.settings.DomainPackage,
			DaoPackage:        p.settings.DaoPackage,
			AssertPackage:     p.settings.AssertPackage,
		}),
	}, nil
}

// GetLoadMode reports that droppederr reads syntax only: it matches call
// shapes and identifier names, never resolved types.
func (p *droppedErrPlugin) GetLoadMode() string {
	return register.LoadModeSyntax
}
