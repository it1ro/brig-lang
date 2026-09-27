// Command corpus — прогон корпуса библиотечного кода (T-115, make corpus).
//
// Каждый файл из corpus/manifest.tsv проверяется до своего уровня
// (parse / check / run). Exit 1 — регрессия, неожиданный проход, needs с
// несуществующей задачей или файл корпуса без строки в манифесте. С
// флагом -update неожиданные проходы и stdout переписываются в манифест
// и X.out (make update-corpus); регрессии остаются ошибкой.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/it1ro/brig-lang/internal/corpus"
	"github.com/it1ro/brig-lang/internal/examples"
)

func main() {
	root := flag.String("root", ".", "корень репозитория")
	dir := flag.String("dir", "corpus", "каталог корпуса от корня")
	manifest := flag.String("manifest", "corpus/manifest.tsv", "манифест от корня")
	tasksDir := flag.String("tasks", "tasks", "каталог задач для needs T-NNN")
	brig := flag.String("brig", "bin/brig", "бинарник brig для уровня run")
	timeout := flag.Duration("timeout", 30*time.Second, "таймаут одного запуска brig")
	top := flag.Int("top", 10, "сколько задач показать в сводке")
	update := flag.Bool("update", false, "переписать манифест и X.out по фактическому состоянию")
	verbose := flag.Bool("v", false, "по строке на файл: уровень и ошибка")
	flag.Parse()

	tasks, err := examples.LoadTasks(*tasksDir)
	if err != nil {
		fatal(err)
	}
	cfg := corpus.Config{
		Root:     *root,
		Dir:      *dir,
		Manifest: *manifest,
		Tasks:    tasks,
		Run:      corpus.ExecRunner(*brig, *timeout),
	}
	rep, err := corpus.Verify(cfg)
	if err != nil {
		fatal(err)
	}
	if *update {
		changes, err := corpus.Update(cfg, rep)
		for _, c := range changes {
			fmt.Println("update:", c)
		}
		if err != nil {
			fatal(err)
		}
		if rep, err = corpus.Verify(cfg); err != nil {
			fatal(err)
		}
	}
	if *verbose {
		if err := rep.Details(os.Stdout); err != nil {
			fatal(err)
		}
	}
	if err := rep.Summary(os.Stdout, *top); err != nil {
		fatal(err)
	}
	if !rep.OK() {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "corpus: %v\n", err)
	os.Exit(2)
}
