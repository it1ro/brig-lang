// Command check-examples — погонщик примеров дизайн-доков (A2).
//
// Извлекает ```brig-блоки из Markdown-документов, оборачивает expr/stmt в
// fn main() -> ... и прогоняет через парсер-заглушку. Exit 0 — все распарсились.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/it1ro/brig-lang/internal/examples"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "использование: check-examples [--] docs/*.md ...")
		flag.PrintDefaults()
	}
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	failed := 0
	checked := 0
	for _, f := range files {
		results, err := examples.CheckFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "check-examples: %v\n", err)
			os.Exit(2)
		}
		for _, r := range results {
			fmt.Println(r.String())
			checked++
			if !r.OK {
				failed++
			}
		}
	}
	fmt.Printf("blocks: checked %d, failed %d\n", checked, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
