package analyzer

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNonNilResults(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), New(Config{}), "nonnilresults")
}
