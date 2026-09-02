package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/Orfeo42/nilfield/analyzer"
)

func main() {
	singlechecker.Main(analyzer.Analyzer)
}
