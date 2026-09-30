// Command corpus — прогон корпуса библиотечного кода (T-115, make corpus).
//
// Каждый файл из corpus/manifest.tsv проверяется до своего уровня
// (parse / check / run). Exit 1 — регрессия, неожиданный проход, needs с
// несуществующей задачей или файл корпуса без строки в манифесте. С
// флагом -update неожиданные проходы и stdout переписываются в манифест
// и X.out (make update-corpus); регрессии остаются ошибкой. С -markers
// (make markers-check, T-246) корпус не запускается: проверяется только, что
// needs манифеста и pending(T-NNN) спеки не ссылаются на закрытые задачи.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	markers := flag.Bool("markers", false, "только гигиена меток (T-246): needs манифеста и pending спеки не ссылаются на закрытые задачи")
	online := flag.Bool("online", false, "с -markers: закрытые задачи — по titles issues через gh")
	closedList := flag.String("closed", "", "с -markers: закрытые задачи списком T-NNN через запятую (PR: задача из title)")
	repo := flag.String("repo", "it1ro/brig-lang", "репозиторий для -online")
	spec := flag.String("spec", "docs/01-language-design.md", "с -markers: спека с метками pending(T-NNN) от корня")
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
	if *markers {
		os.Exit(checkMarkers(cfg, *spec, *online, *closedList, *repo))
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

// checkMarkers — make markers-check (T-246): ссылки needs манифеста и
// pending(T-NNN) спеки на закрытые задачи. Без -online и -closed ничего не
// проверяется (сети нет, PR-задача неизвестна). Возвращает код выхода.
func checkMarkers(cfg corpus.Config, spec string, online bool, closedList, repo string) int {
	if !online && closedList == "" {
		fmt.Println("markers-check: пропущено (нужен ONLINE=1 или CLOSED=T-NNN)")
		return 0
	}
	closed := map[string]bool{}
	if online {
		c, err := examples.LoadClosedTasks(repo)
		if err != nil {
			fatal(err)
		}
		closed = c
	}
	extra, err := examples.ParseClosed(closedList)
	if err != nil {
		fatal(err)
	}
	for id := range extra {
		closed[id] = true
	}

	problems, err := corpus.CheckClosed(cfg, closed)
	if err != nil {
		fatal(err)
	}
	pending, err := examples.ClosedPending(filepath.Join(cfg.Root, spec), cfg.Tasks, closed)
	if err != nil {
		fatal(err)
	}
	problems = append(problems, pending...)
	for _, p := range problems {
		fmt.Println("FAIL", p)
	}
	if len(problems) > 0 {
		fmt.Printf("markers-check: %d ссылок на закрытые задачи\n", len(problems))
		return 1
	}
	fmt.Printf("markers-check: ok (закрытых задач: %d)\n", len(closed))
	return 0
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "corpus: %v\n", err)
	os.Exit(2)
}
