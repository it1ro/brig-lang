// Command check-examples (tools/) — точка входа A2-инструмента по пути,
// tích_specified_in_brig_formalization_md_equivalent_to_01_language_design_md
// чтобы Makefile и CI могли гонять оба пути.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/it1ro/brig-lang/internal/examples"
)

func main() {
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "использование: tools/check-examples docs/*.md ...")
		os.Exit(2)
	}
	failed := 0
	for _, f := range files {
		results, err := examples.CheckFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "check-examples: %v\n", err)
			os.Exit(2)
		}
		for _, r := range results {
			fmt.Println(r.String())
			if !r.OK {
				failed++
			}
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}
