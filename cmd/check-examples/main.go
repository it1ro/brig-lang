// Command check-examples — погонщик примеров дизайн-доков (A2).
//
// Извлекает ```brig-блоки из Markdown-документов, оборачивает expr/stmt в
// fn main() -> ... и прогоняет через парсер, sema и компилятор. Блок
// `brig pending(T-NNN)` обязан не компилироваться; T-NNN ищется в --tasks.
// Exit 0 — все блоки прошли проверку по своим меткам.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/it1ro/brig-lang/internal/examples"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "использование: check-examples [--tasks dir] [--] docs/*.md ...")
		flag.PrintDefaults()
	}
	tasksDir := flag.String("tasks", "tasks", "каталог задач для меток pending(T-NNN)")
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	tasks, err := examples.LoadTasks(*tasksDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-examples: %v\n", err)
		os.Exit(2)
	}

	failed, checked, pending := 0, 0, 0
	for _, f := range files {
		results, err := examples.CheckFile(f, tasks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "check-examples: %v\n", err)
			os.Exit(2)
		}
		for _, r := range results {
			fmt.Println(r.String())
			checked++
			switch {
			case !r.OK:
				failed++
			case r.Pending != "":
				pending++
			}
		}
	}
	fmt.Printf("blocks: checked %d, failed %d, pending %d\n", checked, failed, pending)
	if failed > 0 {
		os.Exit(1)
	}
}
