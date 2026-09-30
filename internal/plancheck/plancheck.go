// Package plancheck проверяет согласованность плана в tasks/ (T-111,
// make plan-check).
//
// Офлайн-проверки (без сети): заголовки блоков `### T-NNN ·` уникальны;
// depends_on блоков ссылается только на меньшие номера, известные в tasks/
// (известны блоки, строки таблиц всех волн и упоминания вида `T-NNN (#M)`
// в тексте — задача вместе со своим issue); столбец «Ждёт»/depends_on
// таблиц ссылается на известные номера; относительные ссылки ведут на
// существующие файлы; у блока есть meta, «Файлы», «Тест-якорь», «DoD»,
// «НЕ делать»; текст ссылки на issue совпадает с номером в URL.
//
// Порядок номеров в строках таблиц не проверяется: у задач с issue он уже
// зафиксирован историей (DD T-90…T-95 заведены позже ждавших их T-48,
// T-80…T-88), правило «меньший номер» действует при планировании блока.
//
// Онлайн-проверки (CheckOnline) сверяют план со списком titles issues:
// номер блока без issue не занят issue, а номер строки таблицы со ссылкой
// на issue совпадает с T-NNN в title этого issue.
package plancheck

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Block — задача без issue: заголовок `### T-NNN · …` и тело до следующего
// заголовка.
type Block struct {
	File      string
	Line      int
	Num       int
	DependsOn []int
	body      string
}

// Row — строка таблицы волны со столбцом T-NN.
type Row struct {
	File      string
	Line      int
	Num       int
	Issue     int // 0 — ссылки на issue нет
	DependsOn []int
}

// Plan — всё, что извлечено из tasks/*.md.
type Plan struct {
	Blocks []Block
	Rows   []Row
	// Mentioned — номера из упоминаний `T-NNN (#M)` / `T-NNN [#M]`.
	Mentioned map[int]bool
	errs      []string
}

var (
	headingRe   = regexp.MustCompile(`^###\s+T-(\d+)\s+·`)
	taskRe      = regexp.MustCompile(`T-(\d+)`)
	issueLinkRe = regexp.MustCompile(`\[#(\d+)\]\(https://github\.com/it1ro/brig-lang/issues/(\d+)\)`)
	linkRe      = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	dependsRe   = regexp.MustCompile(`(?m)^depends_on:\s*(.*)$`)
	titleRe     = regexp.MustCompile(`^T-(\d+)\b`)
	mentionRe   = regexp.MustCompile(`T-(\d+)\s*[(\[]#\d+`)
)

// requiredParts — обязательные части блока задачи (tasks/README.md).
var requiredParts = []struct{ name, marker string }{
	{"meta", "<!-- meta"},
	{"Файлы", "**Файлы:**"},
	{"Тест-якорь", "**Тест-якорь:**"},
	{"DoD", "**DoD:**"},
	{"НЕ делать", "**НЕ делать:**"},
}

// Load читает все dir/*.md из fsys (корень репозитория: ссылки из tasks/
// могут вести в него, например ../AUDIT_REPORT-2.md).
func Load(fsys fs.FS, dir string) (*Plan, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no *.md in %s", dir)
	}
	sort.Strings(files)
	p := &Plan{Mentioned: map[int]bool{}}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		p.parseFile(fsys, f, string(data))
	}
	return p, nil
}

// table — столбцы текущей таблицы с T-NN; -1 — столбца нет.
type table struct{ num, issue, deps int }

func (p *Plan) parseFile(fsys fs.FS, file, src string) {
	lines := strings.Split(src, "\n")
	var cur *Block
	var tbl *table
	inFence := false
	for i, line := range lines {
		ln := i + 1
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if inFence {
			if cur != nil {
				cur.body += line + "\n"
			}
			continue
		}
		p.checkLinks(fsys, file, ln, line)
		for _, n := range taskNums(strings.Join(mentionRe.FindAllString(line, -1), " ")) {
			p.Mentioned[n] = true
		}

		if strings.HasPrefix(line, "#") {
			p.closeBlock(cur)
			cur = nil
			if m := headingRe.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				cur = &Block{File: file, Line: ln, Num: n}
			}
		} else if cur != nil {
			cur.body += line + "\n"
		}

		if !strings.HasPrefix(line, "|") {
			tbl = nil
			continue
		}
		cells := splitRow(line)
		if tbl == nil {
			tbl = headerOf(cells)
			if tbl == nil {
				// Не таблица задач: пропустить до конца таблицы.
				tbl = &table{num: -1, issue: -1, deps: -1}
			}
			continue
		}
		if tbl.num < 0 || tbl.num >= len(cells) || strings.HasPrefix(cells[0], "---") {
			continue
		}
		p.parseRow(file, ln, tbl, cells)
	}
	p.closeBlock(cur)
}

// headerOf распознаёт шапку таблицы задач: столбец «T-NN», опционально
// «Issue» и «Ждёт»/«depends_on».
func headerOf(cells []string) *table {
	t := &table{num: -1, issue: -1, deps: -1}
	for i, c := range cells {
		switch c {
		case "T-NN":
			t.num = i
		case "Issue":
			t.issue = i
		case "Ждёт", "depends_on":
			t.deps = i
		}
	}
	if t.num < 0 {
		return nil
	}
	if t.issue < 0 {
		t.issue = t.num
	}
	return t
}

func (p *Plan) parseRow(file string, ln int, t *table, cells []string) {
	m := taskRe.FindStringSubmatch(cells[t.num])
	if m == nil {
		p.errorf(file, ln, "table row without T-NNN in column T-NN")
		return
	}
	r := Row{File: file, Line: ln}
	r.Num, _ = strconv.Atoi(m[1])
	if t.issue < len(cells) {
		if lm := issueLinkRe.FindStringSubmatch(cells[t.issue]); lm != nil {
			r.Issue, _ = strconv.Atoi(lm[1])
		}
	}
	if t.deps >= 0 && t.deps < len(cells) {
		r.DependsOn = taskNums(cells[t.deps])
	}
	p.Rows = append(p.Rows, r)
}

func (p *Plan) closeBlock(b *Block) {
	if b == nil {
		return
	}
	if m := dependsRe.FindStringSubmatch(b.body); m != nil {
		b.DependsOn = taskNums(m[1])
	}
	p.Blocks = append(p.Blocks, *b)
}

// checkLinks: относительная ссылка ведёт на существующий файл, текст
// ссылки на issue совпадает с номером в URL.
func (p *Plan) checkLinks(fsys fs.FS, file string, ln int, line string) {
	for _, m := range issueLinkRe.FindAllStringSubmatch(line, -1) {
		if m[1] != m[2] {
			p.errorf(file, ln, "issue link text #%s points to issues/%s", m[1], m[2])
		}
	}
	for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
			continue
		}
		if i := strings.IndexByte(target, '#'); i >= 0 {
			target = target[:i]
		}
		if _, err := fs.Stat(fsys, path.Clean(path.Join(path.Dir(file), target))); err != nil {
			p.errorf(file, ln, "broken link %s", m[1])
		}
	}
}

// Check — офлайн-проверки. Пустой результат — план согласован.
func (p *Plan) Check() []string {
	errs := append([]string(nil), p.errs...)
	known := map[int]bool{}
	seen := map[int]Block{}
	for _, b := range p.Blocks {
		known[b.Num] = true
		if prev, ok := seen[b.Num]; ok {
			errs = append(errs, fmt.Sprintf("%s:%d: duplicate block T-%d (first at %s:%d)", b.File, b.Line, b.Num, prev.File, prev.Line))
		} else {
			seen[b.Num] = b
		}
		for _, part := range requiredParts {
			if !strings.Contains(b.body, part.marker) {
				errs = append(errs, fmt.Sprintf("%s:%d: block T-%d has no %s", b.File, b.Line, b.Num, part.name))
			}
		}
	}
	for _, r := range p.Rows {
		known[r.Num] = true
	}
	for n := range p.Mentioned {
		known[n] = true
	}
	checkDeps := func(file string, line, num int, deps []int, ordered bool) {
		for _, d := range deps {
			switch {
			case ordered && d >= num:
				errs = append(errs, fmt.Sprintf("%s:%d: T-%d depends on T-%d, not a smaller number", file, line, num, d))
			case !known[d]:
				errs = append(errs, fmt.Sprintf("%s:%d: T-%d depends on T-%d, unknown in tasks/", file, line, num, d))
			}
		}
	}
	for _, b := range p.Blocks {
		checkDeps(b.File, b.Line, b.Num, b.DependsOn, true)
	}
	for _, r := range p.Rows {
		checkDeps(r.File, r.Line, r.Num, r.DependsOn, false)
	}
	return errs
}

// CheckOnline сверяет план с titles issues (номер issue → title).
func (p *Plan) CheckOnline(titles map[int]string) []string {
	var errs []string
	byTask := map[int][]int{}
	for n, t := range titles {
		if num, ok := titleTask(t); ok {
			byTask[num] = append(byTask[num], n)
		}
	}
	linked := map[int]bool{}
	for _, r := range p.Rows {
		if r.Issue == 0 {
			continue
		}
		linked[r.Num] = true
		t, ok := titles[r.Issue]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s:%d: T-%d links to #%d, no such issue", r.File, r.Line, r.Num, r.Issue))
			continue
		}
		if num, ok := titleTask(t); !ok || num != r.Num {
			errs = append(errs, fmt.Sprintf("%s:%d: T-%d links to #%d titled %q", r.File, r.Line, r.Num, r.Issue, t))
		}
	}
	for _, b := range p.Blocks {
		if linked[b.Num] {
			continue
		}
		if ns := byTask[b.Num]; len(ns) > 0 {
			sort.Ints(ns)
			errs = append(errs, fmt.Sprintf("%s:%d: block T-%d has no issue link, but issue #%d is titled T-%d", b.File, b.Line, b.Num, ns[0], b.Num))
		}
	}
	return errs
}

// titleTask: номер T-NNN в начале title issue.
func titleTask(title string) (int, bool) {
	m := titleRe.FindStringSubmatch(title)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

func taskNums(s string) []int {
	var out []int
	for _, m := range taskRe.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func (p *Plan) errorf(file string, line int, format string, args ...any) {
	p.errs = append(p.errs, fmt.Sprintf("%s:%d: ", file, line)+fmt.Sprintf(format, args...))
}
