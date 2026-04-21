package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	file := flag.String("file", "", "path to ServiceNow export (.xlsx or .csv)")
	flag.Parse()

	if *file == "" {
		fmt.Fprintln(os.Stderr, "usage: ingest -file=path/to/export.xlsx")
		os.Exit(2)
	}

	fmt.Fprintf(os.Stderr, "ingest: stub — Phase 1 plan drives implementation\n")
	fmt.Fprintf(os.Stderr, "ingest: requested file: %s\n", *file)
	os.Exit(0)
}
