package plugin

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/Orfeo42/nilfield/slogfmt"
)

func init() {
	register.Plugin("slogfmt", NewSlogfmt)
}

// SlogfmtSettings mirrors slogfmt.Config for golangci-lint's settings decoding.
type SlogfmtSettings struct {
	ExcludePaths string `json:"exclude-paths"`
}

// NewSlogfmt decodes settings and builds the slogfmt linter plugin.
func NewSlogfmt(settings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[SlogfmtSettings](settings)
	if err != nil {
		return nil, err
	}

	return &slogfmtPlugin{settings: s}, nil
}

type slogfmtPlugin struct {
	settings SlogfmtSettings
}

// BuildAnalyzers returns the slogfmt analyzer configured from the plugin settings.
func (p *slogfmtPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		slogfmt.New(slogfmt.Config{ExcludePaths: p.settings.ExcludePaths}),
	}, nil
}

// GetLoadMode reports that slogfmt reads syntax only: it matches slog call
// shapes and rewrites their source text, never resolved types.
func (p *slogfmtPlugin) GetLoadMode() string {
	return register.LoadModeSyntax
}
