// Package corpus — корпус библиотечного кода (T-115): файлы corpus/ и
// манифест corpus/manifest.tsv, где у каждого файла записан ожидаемый
// уровень (parse / check / run), ожидание (pass / fail) и задачи, которых
// файл ждёт (needs).
//
// Строка манифеста (L, pass) — файл проходит все уровни до L включительно.
// Строка (L, fail) — файл проходит уровни ниже L и падает на L; needs не
// пуст. Verify сообщает о регрессии (файл упал раньше, чем обещано), о
// неожиданном проходе (уровень L пройден, а по манифесту файл ещё ждёт
// задачу) и о needs с несуществующей задачей.
package corpus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
)

// Level — уровень проверки файла. None — файл не прошёл даже parse.
type Level int

// Уровни по возрастанию: каждый включает предыдущие.
const (
	None  Level = iota // не пройден ни один уровень
	Parse              // лексер и парсер
	Check              // + sema (включая имена, T-139) и компиляция в байткод
	Run                // + brig <file>: код 0, stdout совпадает с X.out, если он есть
)

var levelNames = [...]string{None: "none", Parse: "parse", Check: "check", Run: "run"}

func (l Level) String() string { return levelNames[l] }

func parseLevel(s string) (Level, bool) {
	for l := Parse; l <= Run; l++ {
		if levelNames[l] == s {
			return l, true
		}
	}
	return None, false
}

// Horizon — метка needs для фичи за горизонтом плана (tasks/README.md,
// «Горизонт после Wave 13»): задачи ещё нет, причина — в комментарии у
// `# needs:` в самом файле.
const Horizon = "horizon"

// Entry — строка манифеста.
type Entry struct {
	Path  string // от корня репозитория, через '/'
	Level Level
	Pass  bool
	Needs []string
	Line  int // строка в манифесте, с 1
}

// Runner исполняет файл как `brig <file>` и возвращает его stdout. Ошибка —
// ненулевой код выхода или невозможность запуска.
type Runner func(path string) (stdout string, err error)

// Config — где лежит корпус и чем его проверять.
type Config struct {
	Root     string          // корень репозитория
	Dir      string          // каталог корпуса от Root: каждый *.brig в нём обязан быть в манифесте
	Manifest string          // путь к манифесту от Root
	Tasks    map[string]bool // известные T-NNN (examples.LoadTasks)
	Run      Runner          // исполнитель уровня run
}

// Result — итог по одной строке манифеста.
type Result struct {
	Entry
	Reached Level  // до какого уровня файл дошёл (не выше Entry.Level)
	Err     string // ошибка первого непройденного уровня
	Problem string // расхождение с манифестом; "" — всё сходится
	outDiff bool   // run: код 0, но stdout не совпадает с X.out
	stdout  string // stdout уровня run при outDiff
}

// Report — результат прогона корпуса.
type Report struct {
	Results  []Result
	Problems []string // все расхождения: по файлам и по манифесту
}

// OK — корпус соответствует манифесту.
func (r *Report) OK() bool { return len(r.Problems) == 0 }

var (
	taskRe  = regexp.MustCompile(`^T-\d+$`)
	needsRe = regexp.MustCompile(`(?m)^#[ \t]*needs:[ \t]*(.*)$`)
)

// ParseManifest читает манифест: строки `путь<TAB>уровень<TAB>ожидание<TAB>needs`,
// needs — через запятую или `-`. Пустые строки и строки с `#` пропускаются.
func ParseManifest(src string) ([]Entry, error) {
	var out []Entry
	for i, ln := range strings.Split(src, "\n") {
		s := strings.TrimSpace(ln)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		var cols []string
		for _, c := range strings.Split(s, "\t") {
			if c = strings.TrimSpace(c); c != "" {
				cols = append(cols, c)
			}
		}
		if len(cols) != 4 {
			return nil, fmt.Errorf("manifest:%d: нужно 4 колонки через TAB (путь, уровень, ожидание, needs), найдено %d", i+1, len(cols))
		}
		lvl, ok := parseLevel(cols[1])
		if !ok {
			return nil, fmt.Errorf("manifest:%d: уровень %q: нужен parse, check или run", i+1, cols[1])
		}
		var pass bool
		switch cols[2] {
		case "pass":
			pass = true
		case "fail":
		default:
			return nil, fmt.Errorf("manifest:%d: ожидание %q: нужен pass или fail", i+1, cols[2])
		}
		out = append(out, Entry{Path: cols[0], Level: lvl, Pass: pass, Needs: splitNeeds(cols[3]), Line: i + 1})
	}
	return out, nil
}

func splitNeeds(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func formatNeeds(needs []string) string {
	if len(needs) == 0 {
		return "-"
	}
	return strings.Join(needs, ", ")
}

func (e Entry) expect() string {
	if e.Pass {
		return "pass"
	}
	return "fail"
}

// state — «уровень ожидание», например «check fail».
func (e Entry) state() string { return e.Level.String() + " " + e.expect() }

func (e Entry) format() string {
	return strings.Join([]string{e.Path, e.Level.String(), e.expect(), formatNeeds(e.Needs)}, "\t")
}

// Verify прогоняет корпус по манифесту.
func Verify(cfg Config) (*Report, error) {
	src, err := os.ReadFile(filepath.Join(cfg.Root, cfg.Manifest))
	if err != nil {
		return nil, err
	}
	entries, err := ParseManifest(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Manifest, err)
	}
	rep := &Report{}
	problem := func(format string, args ...any) {
		rep.Problems = append(rep.Problems, fmt.Sprintf(format, args...))
	}

	seen := map[string]bool{}
	for _, e := range entries {
		at := fmt.Sprintf("%s:%d", cfg.Manifest, e.Line)
		if seen[e.Path] {
			problem("%s: %s: вторая строка для того же файла", at, e.Path)
			continue
		}
		seen[e.Path] = true
		if bad := checkEntry(cfg, e); bad != "" {
			problem("%s: %s: %s", at, e.Path, bad)
			continue
		}
		res := evaluate(cfg, e, e.Level)
		res.Problem = judge(res)
		if res.Problem != "" {
			problem("%s: %s", e.Path, res.Problem)
		}
		rep.Results = append(rep.Results, res)
	}

	files, err := corpusFiles(cfg)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !seen[f] {
			problem("%s: нет строки в %s", f, cfg.Manifest)
		}
	}
	return rep, nil
}

// checkEntry — строка манифеста сама по себе корректна: файл есть, needs
// ссылаются на существующие задачи и совпадают с `# needs:` в файле.
func checkEntry(cfg Config, e Entry) string {
	if filepath.IsAbs(e.Path) || e.Path != filepath.ToSlash(filepath.Clean(e.Path)) || strings.HasPrefix(e.Path, "../") {
		return "путь должен быть относительным от корня репозитория, без ./ и ../"
	}
	data, err := os.ReadFile(filepath.Join(cfg.Root, e.Path))
	if err != nil {
		return "файла нет"
	}
	if e.Pass && len(e.Needs) > 0 {
		return "pass с needs: у прошедшего файла needs пуст (-)"
	}
	if !e.Pass && len(e.Needs) == 0 {
		return "fail без needs: укажите задачи T-NNN или horizon"
	}
	for _, n := range e.Needs {
		switch {
		case n == Horizon:
		case !taskRe.MatchString(n):
			return fmt.Sprintf("needs: %q — ни T-NNN, ни %s", n, Horizon)
		case !cfg.Tasks[n]:
			return fmt.Sprintf("needs: задачи %s нет в tasks/", n)
		}
	}
	if isBrig(e.Path) {
		if hdr := fileNeeds(string(data)); !sameSet(hdr, e.Needs) {
			return fmt.Sprintf("`# needs:` в файле (%s) не совпадает с манифестом (%s)", formatNeeds(hdr), formatNeeds(e.Needs))
		}
	}
	return ""
}

// fileNeeds — метки из строки `# needs: …` в файле (nil — строки нет).
func fileNeeds(src string) []string {
	m := needsRe.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	return splitNeeds(m[1])
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func isBrig(path string) bool { return strings.HasSuffix(path, ".brig") }

// judge сравнивает результат с ожиданием строки манифеста.
func judge(r Result) string {
	switch {
	case r.Pass && r.Reached < r.Level:
		return fmt.Sprintf("регрессия: ожидался pass на %s, файл падает на %s: %s", r.Level, r.Reached+1, r.Err)
	case !r.Pass && r.Reached >= r.Level:
		return fmt.Sprintf("неожиданный проход: по манифесту файл падает на %s и ждёт %s, а уровень пройден — снимите выполненные задачи из needs (манифест и `# needs:`) и поднимите уровень (make update-corpus)",
			r.Level, formatNeeds(r.Needs))
	case !r.Pass && r.Reached < r.Level-1:
		return fmt.Sprintf("регрессия: ожидался fail на %s, файл падает раньше — на %s: %s", r.Level, r.Reached+1, r.Err)
	}
	return ""
}

// evaluate проходит уровни parse…upTo и останавливается на первом
// непройденном.
func evaluate(cfg Config, e Entry, upTo Level) Result {
	res := Result{Entry: e}
	stop := func(reached Level, err string) Result {
		res.Reached, res.Err = reached, err
		return res
	}
	if !isBrig(e.Path) {
		return stop(None, "не .brig: парсера для этого формата нет")
	}
	path := filepath.Join(cfg.Root, e.Path)
	src, err := os.ReadFile(path)
	if err != nil {
		return stop(None, err.Error())
	}
	prog, err := parser.ParseProgram(parser.ModeModule, string(src))
	if err != nil {
		return stop(None, err.Error())
	}
	if upTo < Check {
		return stop(Parse, "")
	}
	for _, d := range sema.CheckNames(prog, nil).Diagnostics {
		if d.Severity == sema.SeverityError {
			return stop(Parse, fmt.Sprintf("sema %d:%d: %s", d.Line, d.Col, d.Message))
		}
	}
	if _, err := compiler.New().Compile(prog); err != nil {
		return stop(Parse, err.Error())
	}
	if upTo < Run {
		return stop(Check, "")
	}
	if cfg.Run == nil {
		return stop(Check, "уровень run: не задан Runner")
	}
	out, err := cfg.Run(path)
	if err != nil {
		return stop(Check, err.Error())
	}
	want, err := os.ReadFile(outPath(path))
	if err == nil && string(want) != out {
		res.outDiff, res.stdout = true, out
		return stop(Check, fmt.Sprintf("stdout не совпадает с %s", filepath.Base(outPath(path))))
	}
	return stop(Run, "")
}

func outPath(path string) string { return strings.TrimSuffix(path, ".brig") + ".out" }

// corpusFiles — все *.brig в cfg.Dir, пути от Root через '/'.
func corpusFiles(cfg Config) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(cfg.Root, cfg.Dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isBrig(p) {
			return nil
		}
		rel, err := filepath.Rel(cfg.Root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Details печатает по строке на файл: достигнутый уровень против
// ожидаемого и ошибку первого непройденного уровня.
func (r *Report) Details(w io.Writer) error {
	var sb strings.Builder
	for _, res := range r.Results {
		fmt.Fprintf(&sb, "%-5s / %-10s %s", res.Reached, res.state(), res.Path)
		if res.Err != "" {
			fmt.Fprintf(&sb, " — %s", res.Err)
		}
		sb.WriteByte('\n')
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

// Summary печатает сводку: файлы по достигнутому уровню и задачи по числу
// файлов, которые их ждут (вход для приоритизации волн), затем расхождения.
func (r *Report) Summary(w io.Writer, top int) error {
	var sb strings.Builder
	byLevel := map[Level]int{}
	blocked := map[string]int{}
	for _, res := range r.Results {
		byLevel[res.Reached]++
		if !res.Pass {
			for _, n := range res.Needs {
				blocked[n]++
			}
		}
	}
	fmt.Fprintf(&sb, "corpus: %d files — ", len(r.Results))
	var parts []string
	for l := Run; l >= None; l-- {
		parts = append(parts, fmt.Sprintf("%s %d", l, byLevel[l]))
	}
	fmt.Fprintln(&sb, strings.Join(parts, ", "))

	needs := make([]string, 0, len(blocked))
	for n := range blocked {
		needs = append(needs, n)
	}
	sort.Slice(needs, func(i, j int) bool {
		if blocked[needs[i]] != blocked[needs[j]] {
			return blocked[needs[i]] > blocked[needs[j]]
		}
		return needs[i] < needs[j]
	})
	if len(needs) > top {
		needs = needs[:top]
	}
	if len(needs) > 0 {
		fmt.Fprintln(&sb, "top needs (blocked files):")
		for _, n := range needs {
			fmt.Fprintf(&sb, "  %-8s %d\n", n, blocked[n])
		}
	}
	for _, p := range r.Problems {
		fmt.Fprintf(&sb, "FAIL %s\n", p)
	}
	if r.OK() {
		fmt.Fprintln(&sb, "corpus: ok")
	} else {
		fmt.Fprintf(&sb, "corpus: %d problems\n", len(r.Problems))
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

// Update переписывает манифест по фактическому состоянию (make update-corpus):
//   - неожиданный проход (L, fail): уровень поднимается до первого
//     непройденного, needs остаются — выполненные задачи снимает человек;
//     файл прошёл до run — строка становится (run, pass), needs и строка
//     `# needs:` в файле удаляются;
//   - (run, pass) с кодом 0 и другим stdout: X.out перезаписывается.
//
// Регрессии и ошибки манифеста не исправляются. Возвращает описания правок.
func Update(cfg Config, rep *Report) ([]string, error) {
	path := filepath.Join(cfg.Root, cfg.Manifest)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(src), "\n")
	var changes []string
	for _, res := range rep.Results {
		switch {
		case res.outDiff && res.Pass:
			if err := os.WriteFile(outPath(filepath.Join(cfg.Root, res.Path)), []byte(res.stdout), 0o644); err != nil {
				return changes, err
			}
			changes = append(changes, fmt.Sprintf("%s: перезаписан %s", res.Path, filepath.Base(outPath(res.Path))))
		case !res.Pass && res.Reached >= res.Level:
			e := res.Entry
			next := evaluate(cfg, e, Run)
			if next.Reached == Run {
				e.Level, e.Pass, e.Needs = Run, true, nil
				if err := dropNeedsHeader(filepath.Join(cfg.Root, e.Path)); err != nil {
					return changes, err
				}
			} else {
				e.Level = next.Reached + 1
			}
			lines[e.Line-1] = e.format()
			changes = append(changes, fmt.Sprintf("%s: %s → %s", e.Path, res.state(), e.state()))
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return changes, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// dropNeedsHeader удаляет строку `# needs:` из файла.
func dropNeedsHeader(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	loc := needsRe.FindIndex(data)
	if loc == nil {
		return nil
	}
	end := loc[1]
	if end < len(data) && data[end] == '\n' {
		end++
	}
	return os.WriteFile(path, append(data[:loc[0]:loc[0]], data[end:]...), 0o644)
}

// ExecRunner — Runner через бинарник brig: `brig <file>` с таймаутом.
func ExecRunner(brig string, timeout time.Duration) Runner {
	return func(path string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, brig, path)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return stdout.String(), fmt.Errorf("brig: таймаут %s", timeout)
		}
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if i := strings.IndexByte(msg, '\n'); i >= 0 {
				msg = msg[:i]
			}
			return stdout.String(), fmt.Errorf("brig: %v: %s", err, msg)
		}
		return stdout.String(), nil
	}
}
