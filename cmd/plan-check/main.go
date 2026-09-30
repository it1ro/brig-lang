// Command plan-check — согласованность плана в tasks/ (T-111, make plan-check).
//
// Без флагов — только офлайн-проверки (входят в make all, сеть не нужна).
// С -online дополнительно сверяет план с titles issues через gh
// (make plan-check ONLINE=1). Exit 1 — найдено расхождение.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/it1ro/brig-lang/internal/plancheck"
)

func main() {
	root := flag.String("root", ".", "корень репозитория")
	dir := flag.String("dir", "tasks", "каталог плана от корня")
	online := flag.Bool("online", false, "сверить с titles issues через gh")
	repo := flag.String("repo", "it1ro/brig-lang", "репозиторий для -online")
	flag.Parse()

	plan, err := plancheck.Load(os.DirFS(*root), *dir)
	if err != nil {
		fatal(err)
	}
	errs := plan.Check()
	if *online {
		titles, err := issueTitles(*repo)
		if err != nil {
			fatal(err)
		}
		errs = append(errs, plan.CheckOnline(titles)...)
		fmt.Printf("plan-check: online, %d issues\n", len(titles))
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e)
	}
	fmt.Printf("plan-check: %d blocks, %d table rows, %d problems\n", len(plan.Blocks), len(plan.Rows), len(errs))
	if len(errs) > 0 {
		os.Exit(1)
	}
}

func issueTitles(repo string) (map[int]string, error) {
	out, err := exec.Command("gh", "issue", "list", "--repo", repo, "--state", "all",
		"--limit", "5000", "--json", "number,title").Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	var items []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}
	titles := make(map[int]string, len(items))
	for _, it := range items {
		titles[it.Number] = it.Title
	}
	return titles, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "plan-check:", err)
	os.Exit(2)
}
