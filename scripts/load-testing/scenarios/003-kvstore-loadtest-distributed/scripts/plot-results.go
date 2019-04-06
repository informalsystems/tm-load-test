package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
)

var rootCmd = flag.NewFlagSet("root", flag.ExitOnError)

var (
	flagInputDir  string
	flagOutputDir string
)

func init() {
	rootCmd.Usage = func() {
		fmt.Println(`Utility script to generate single-node load test plots using plotly.js

Usage:
  go run plot-results.go -in <load-test-results-dir> -out <plot-output-dir>

Example:
  go run plot-results.go \
    -in /tmp/003-kvstore-loadtest-distributed \
    -out /tmp/003-kvstore-loadtest-distributed/plots

Flags:`)
		rootCmd.PrintDefaults()
		fmt.Println("")
	}
	rootCmd.StringVar(&flagInputDir, "in", "", "the input directory in which to find the single load test's results")
	rootCmd.StringVar(&flagOutputDir, "out", "", "the output directory into which to write the plotly.js plots")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Please specify both the input and output directories (use -h for more details)")
		os.Exit(1)
	}
	if err := rootCmd.Parse(os.Args[1:]); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	if len(flagInputDir) == 0 {
		fmt.Println("Input directory path is missing (see -h for details)")
		os.Exit(1)
	}
	if len(flagOutputDir) == 0 {
		fmt.Println("Output directory path is missing (see -h for details)")
		os.Exit(1)
	}

	if err := loadtest.PlotSingleTestSummaryResults(flagInputDir, flagOutputDir); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(2)
	} else {
		fmt.Println("Single test summary plot written to:", flagOutputDir)
	}
}
