package main

import (
	clearslice "github.com/zcross/clearslice/analyzer"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(clearslice.NewAnalyzer())
}
