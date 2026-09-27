// Package examples implements the check-examples tool (A2): extracting
// ```brig fenced blocks from design docs and running them through the
// parser, sema and compiler; REPL blocks are executed (T-117).
package examples

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Result — отчёт по одному блоку (A2). Line:Col — позиция в markdown:
// ошибки — строка и колонка ошибки, иначе — строка fence.
type Result struct {
	File    string
	Line    int
	Col     int
	Mode    string
	OK      bool
	Pending string // T-NNN из метки pending(T-NNN), если блок ждёт задачу
	ErrMsg  string
}

func (r Result) String() string {
	status := "ok"
	switch {
	case !r.OK:
		status = "FAIL"
	case r.Pending != "":
		status = "pending(" + r.Pending + ")"
	}
	if r.ErrMsg == "" {
		return fmt.Sprintf("%s:%d:%d — %s", r.File, r.Line, r.Col, status)
	}
	return fmt.Sprintf("%s:%d:%d — %s — [%s]", r.File, r.Line, r.Col, status, r.ErrMsg)
}

// CheckFile прогоняет все brig-блоки файла через парсер, sema и компилятор
// (A2, T-116). tasks — известные номера задач для меток pending(T-NNN).
func CheckFile(path string, tasks map[string]bool) ([]Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blocks := extractBlocks(path, string(data))
	results := make([]Result, 0, len(blocks))
	for _, b := range blocks {
		results = append(results, checkBlock(b, tasks))
	}
	return results, nil
}

// taskRe — номер задачи T-NNN в tasks/*.md.
var taskRe = regexp.MustCompile(`\bT-\d+\b`)

// LoadTasks собирает номера задач, упомянутые в dir/*.md (T-116): метка
// pending(T-NNN) обязана ссылаться на существующую задачу.
func LoadTasks(dir string) (map[string]bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no *.md in %s", dir)
	}
	tasks := map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, id := range taskRe.FindAllString(string(data), -1) {
			tasks[id] = true
		}
	}
	return tasks, nil
}

// block — один fenced-блок с метаданными.
type block struct {
	file string
	line int
	lang string
	raw  string
}

// fenceRe: линия-ограждение из 3+ бэктиков.
var fenceRe = regexp.MustCompile("^[ \\t]*(`{3,})[ \\t]*(.*)$")

// modeRe извлекает режим из метки fence. \b гарантирует, что
// "invalid" матчится точно, а не как префикс "invalid_foo".
var modeRe = regexp.MustCompile(`^(module|repl|expr|stmt|invalid)\b`)

// extractBlocks находит fenced-блоки и возвращает только ```brig*.
func extractBlocks(path, src string) []block {
	lines := strings.Split(src, "\n")
	var out []block
	inFence := false
	fenceLen := 0
	var cur block
	var body []string
	for i, ln := range lines {
		if m := fenceRe.FindStringSubmatch(ln); m != nil {
			n := len(m[1])
			if !inFence {
				inFence = true
				fenceLen = n
				cur = block{file: path, line: i + 1, lang: strings.TrimSpace(m[2])}
				body = nil
				continue
			}
			if n >= fenceLen {
				inFence = false
				cur.raw = strings.Join(body, "\n")
				if strings.HasPrefix(cur.lang, "brig") {
					out = append(out, cur)
				}
				continue
			}
			body = append(body, ln)
			continue
		}
		if inFence {
			body = append(body, ln)
		}
	}
	return out
}

// modeByMeta — режим из метки fence (module/repl/expr/stmt/invalid) или "".
func modeByMeta(meta string) string {
	if m := modeRe.FindStringSubmatch(meta); m != nil {
		return m[1]
	}
	return ""
}

// pendingRe — метка pending(T-NNN) (T-116): блок ждёт фичу задачи T-NNN.
var pendingRe = regexp.MustCompile(`(^|\s)pending(\((T-\d+)\))?(\s|$)`)

// reasonRe — ожидаемая подстрока ошибки invalid-блока: invalid "...".
var reasonRe = regexp.MustCompile(`^invalid\s+"([^"]*)"`)

// heuristicMode — единственная оставшаяся эвристика: блок целиком из строк
// с '>' → repl. Остальное — stmt (G.4: дизайн-док обязан использовать
// явные метки).
func heuristicMode(raw string) string {
	hasPrompt := false
	hasOther := false
	for _, ln := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(ln)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, ">") {
			hasPrompt = true
			continue
		}
		hasOther = true
		break
	}
	if hasPrompt && !hasOther {
		return "repl"
	}
	return "stmt"
}

// checkBlock: выбор режима, обёртка expr/stmt, парсинг, sema, компиляция.
func checkBlock(b block, tasks map[string]bool) Result {
	meta := strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))
	mode := modeByMeta(meta)
	pending := ""
	if m := pendingRe.FindStringSubmatch(meta); m != nil {
		if m[3] == "" {
			return fail(b, mode, "pending без номера задачи: нужна метка pending(T-NNN)")
		}
		pending = m[3]
		if mode == "" {
			meta = strings.TrimSpace(pendingRe.ReplaceAllString(meta, " "))
			mode = modeByMeta(meta)
		}
	}
	if mode == "" {
		mode = heuristicMode(b.raw)
	}

	if pending != "" {
		if mode == "invalid" || mode == "repl" {
			return fail(b, mode, "pending(T-NNN) допустим только для module/stmt/expr")
		}
		if !tasks[pending] {
			return fail(b, mode, fmt.Sprintf("pending(%s): задачи %s нет в tasks/", pending, pending))
		}
	}

	if mode == "invalid" {
		err := compile(b.raw)
		if err == nil {
			return fail(b, mode, "invalid block compiled successfully")
		}
		if m := reasonRe.FindStringSubmatch(meta); m != nil && !strings.Contains(err.Error(), m[1]) {
			r := failAt(b, mode, 0, err)
			r.ErrMsg = fmt.Sprintf("invalid: want error containing %q, got %s", m[1], r.ErrMsg)
			return r
		}
		return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
	}

	src := wrapForMode(mode, b.raw)

	if mode == "repl" {
		if err := runRepl(src); err != nil {
			r := fail(b, mode, err.Error())
			var re *replError
			if errors.As(err, &re) {
				r.Line = b.line + re.line
			}
			return r
		}
		return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
	}

	indent := 0
	if mode != "module" {
		indent = 4
	}
	err := compile(src)
	if pending != "" {
		if err == nil {
			return fail(b, mode, fmt.Sprintf("блок компилируется — снять pending(%s)", pending))
		}
		return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true, Pending: pending}
	}
	if err != nil {
		return failAt(b, mode, indent, err)
	}
	if err := roundTrip(src); err != nil {
		return fail(b, mode, err.Error())
	}
	return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
}

// compile: парсинг (module), sema (§F.3) и компиляция в байткод. Ошибка —
// *lexer.Error, *parser.Error, *compiler.Error или *semaError.
func compile(src string) error {
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		return err
	}
	for _, d := range sema.Check(prog).Diagnostics {
		if d.Severity == sema.SeverityError {
			return &semaError{d}
		}
	}
	_, err = compiler.New().Compile(prog)
	return err
}

// roundTrip: parse → Format → parse даёт то же AST.
func roundTrip(src string) error {
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		return err
	}
	prog2, err := parser.ParseProgram(parser.ModeModule, ast.Format(prog))
	if err != nil {
		return fmt.Errorf("format round-trip re-parse: %v", err)
	}
	if !ast.Equal(prog, prog2) {
		return errors.New("format round-trip mismatch")
	}
	return nil
}

type semaError struct{ d sema.Diagnostic }

func (e *semaError) Error() string {
	return fmt.Sprintf("sema %d:%d: %s", e.d.Line, e.d.Col, e.d.Message)
}

// errPos — позиция ошибки в исходнике блока (0, 0 — неизвестна).
func errPos(err error) (line, col int) {
	var le *lexer.Error
	var pe *parser.Error
	var ce *compiler.Error
	var se *semaError
	switch {
	case errors.As(err, &le):
		return le.Line, le.Col
	case errors.As(err, &pe):
		return pe.Line, pe.Col
	case errors.As(err, &ce):
		return ce.Line, ce.Col
	case errors.As(err, &se):
		return se.d.Line, se.d.Col
	}
	return 0, 0
}

// failAt: FAIL с позицией ошибки в markdown. Строка 1 исходника — первая
// строка тела блока (у обёртки stmt/expr первая строка fn main() -> стоит
// на месте fence), indent — отступ, добавленный обёрткой.
func failAt(b block, mode string, indent int, err error) Result {
	r := fail(b, mode, err.Error())
	line, col := errPos(err)
	if line == 0 {
		return r
	}
	if indent == 0 {
		line++ // module/invalid: строка 1 — первая строка после fence
	}
	r.Line = b.line + line - 1
	if line > 1 && col > indent {
		r.Col = col - indent
	}
	return r
}

func fail(b block, mode, msg string) Result {
	return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: false, ErrMsg: msg}
}

// wrapForMode: expr/stmt оборачиваются в fn main() -> ... (A2).
func wrapForMode(mode, raw string) string {
	switch mode {
	case "expr":
		return "fn main() ->\n    " + strings.TrimSpace(raw) + "\n"
	case "stmt":
		var sb strings.Builder
		sb.WriteString("fn main() ->\n")
		for _, ln := range strings.Split(raw, "\n") {
			if strings.TrimSpace(ln) == "" {
				sb.WriteString("\n")
				continue
			}
			sb.WriteString("    ")
			sb.WriteString(ln)
			sb.WriteString("\n")
		}
		return sb.String()
	}
	return raw
}

// replError — провал REPL-блока на строке line тела блока (с 1).
type replError struct {
	line int
	msg  string
}

func (e *replError) Error() string { return e.msg }

// runRepl исполняет REPL-блок (§G.5, T-117): строки `> ввод` — в одной
// REPL-сессии; строка после ввода без `>` — ответ: выражение, которое
// вычисляется в отдельной чистой сессии и сравнивается с результатом
// через `==` (runtime.Equal), или `raise <терм>` — ожидаемый непойманный
// raise. Ввод без ответа исполняется, но не сравнивается; неожиданный
// raise — провал. Ошибка — *replError.
func runRepl(src string) error {
	session := repl.New(vm.New(), io.Discard)
	oracle := repl.New(vm.New(), io.Discard)

	var (
		input     string // последний ввод, ещё без ответа
		inputLine int
		res       runtime.Value
		runErr    error
		answered  bool
	)
	// finish: ввод без ответа — только неожиданный raise или ошибка.
	finish := func() error {
		if input == "" || answered || runErr == nil {
			return nil
		}
		return &replError{inputLine, fmt.Sprintf("> %s: %v", input, runErr)}
	}
	for i, ln := range strings.Split(src, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			if err := finish(); err != nil {
				return err
			}
			input = strings.TrimSpace(strings.TrimPrefix(line, ">"))
			inputLine, answered = i+1, false
			res, runErr = evalLine(session, input)
			if runErr != nil && !isRaise(runErr) {
				return &replError{inputLine, fmt.Sprintf("> %s: %v", input, runErr)}
			}
			continue
		}
		if input == "" {
			return &replError{i + 1, fmt.Sprintf("ответ %q без строки ввода `>`", line)}
		}
		if answered {
			return &replError{i + 1, fmt.Sprintf("> %s: больше одной строки ответа", input)}
		}
		answered = true
		if err := compareAnswer(oracle, input, line, res, runErr); err != nil {
			return &replError{i + 1, err.Error()}
		}
	}
	return finish()
}

// evalLine: одна строка ввода; сообщения sema — в текст ошибки.
func evalLine(r *repl.REPL, src string) (runtime.Value, error) {
	var diag strings.Builder
	r.SetOutput(&diag)
	defer r.SetOutput(io.Discard)
	v, err := r.Eval(src + "\n")
	if err != nil && diag.Len() > 0 {
		return v, fmt.Errorf("%w: %s", err, strings.TrimSpace(diag.String()))
	}
	return v, err
}

// compareAnswer сверяет результат ввода со строкой ответа.
func compareAnswer(oracle *repl.REPL, input, answer string, res runtime.Value, runErr error) error {
	wantRaise := false
	term := answer
	if rest, ok := strings.CutPrefix(answer, "raise "); ok {
		wantRaise, term = true, rest
	}
	want, err := evalAnswer(oracle, term)
	if err != nil {
		return fmt.Errorf("> %s: ответ %q: %v", input, answer, err)
	}
	got := "raise " + runErrVal(runErr).Inspect()
	if runErr == nil {
		got = res.Inspect()
	}
	wantStr := want.Inspect()
	if wantRaise {
		wantStr = "raise " + wantStr
	}
	if wantRaise != (runErr != nil) {
		return fmt.Errorf("> %s: want %s, got %s", input, wantStr, got)
	}
	if wantRaise {
		res = runErrVal(runErr)
	}
	if !runtime.Equal(res, want) {
		return fmt.Errorf("> %s: want %s, got %s", input, wantStr, got)
	}
	return nil
}

// evalAnswer: ответ — одно выражение (не связывание), вычисляется в
// чистой сессии, чтобы ответ не видел имён блока.
func evalAnswer(oracle *repl.REPL, src string) (runtime.Value, error) {
	prog, err := parser.ParseProgram(parser.ModeRepl, src+"\n")
	if err != nil {
		return runtime.Unit, err
	}
	if len(prog.Stmts) != 1 {
		return runtime.Unit, errors.New("ответ — ровно одно выражение")
	}
	if _, ok := prog.Stmts[0].(ast.ExprStmt); !ok {
		return runtime.Unit, errors.New("ответ — выражение, а не связывание")
	}
	return evalLine(oracle, src)
}

func isRaise(err error) bool {
	var re *vm.ErrRaise
	return errors.As(err, &re)
}

func runErrVal(err error) runtime.Value {
	var re *vm.ErrRaise
	if errors.As(err, &re) {
		return re.Val
	}
	return runtime.Unit
}
