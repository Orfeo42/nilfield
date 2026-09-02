package analyzer

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestConditionalNilResults(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), New(Config{}), "condresults")
}
