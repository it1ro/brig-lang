// Package examples implements the check-examples tool (A2): extracting
// ```brig fenced blocks from design docs and running them through the parser.
//
// После ужесточения (этап 3, шаг 3):
//   - убраны текстовые эвристики (reResultType, reFnNoArgs, reArgDotDot,
//     reOkEquiv, reTopLevelBind): их роль теперь у парсера;
//   - добавлен режим "invalid" (G.5): блок обязан НЕ парситься;
//   - добавлен sanity-check parse → Format → parse ≡ parse.
package examples

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// Result — отчёт по одному блоку: file:line:col — status — [error] (A2).
type Result struct {
	File   string
	Line   int // строка открывающего fence
	Col    int
	Mode   string
	OK     bool
	ErrMsg string // сообщение об ошибке (пусто при OK)
}

func (r Result) String() string {
	status := "ok"
	if !r.OK {
		status = "FAIL"
	}
	if r.ErrMsg == "" {
		return fmt.Sprintf("%s:%d:%d — %s", r.File, r.Line, r.Col, status)
	}
	return fmt.Sprintf("%s:%d:%d — %s — [%s]", r.File, r.Line, r.Col, status, r.ErrMsg)
}

// CheckFile прогоняет все brig-блоки файла через парсер (A2).
func CheckFile(path string) ([]Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blocks := extractBlocks(path, string(data))
	results := make([]Result, 0, len(blocks))
	for _, b := range blocks {
		results = append(results, checkBlock(b))
	}
	return results, nil
}

// block — один fenced-блок с метаданными.
type block struct {
	file string
	line int // строка открывающего fence (1-based)
	lang string
	raw  string // содержимое блока
}

// fenceRe: линия-ограждение из 3+ бэктиков. CommonMark: закрывающий fence
// должен быть НЕ короче открывающего — в дизайн-доках brig-блоки
// закрываются и ``` и ```` (последнее — когда вокруг внешний 4-бэктиковый
// fence, который нужно честно пропускать).
var fenceRe = regexp.MustCompile("^[ \\t]*(`{3,})[ \\t]*(.*)$")

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
			// Закрывающий fence короче открывающего — это содержимое.
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
	for _, m := range []string{"module", "repl", "expr", "stmt", "invalid"} {
		if strings.HasPrefix(meta, m) {
			return m
		}
	}
	return ""
}

// heuristicMode — единственная оставшаяся эвристика: блок целиком из строк
// с '>' → repl. Во всех остальных случаях — stmt (дизайн-док обязан
// использовать явные метки, G.4).
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

// checkBlock: выбор режима, обёртка expr/stmt, парсинг, sanity-check.
func checkBlock(b block) Result {
	meta := strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))
	mode := modeByMeta(meta)
	if mode == "" {
		mode = heuristicMode(b.raw)
	}

	// G.5: invalid — блок обязан НЕ парситься.
	if mode == "invalid" {
		if err := parser.Parse(parser.ModeModule, b.raw); err == nil {
			return fail(b, mode, "invalid block parsed successfully")
		}
		return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
	}

	src := wrapForMode(mode, b.raw)

	switch mode {
	case "repl":
		if err := parseRepl(src); err != nil {
			return fail(b, mode, err.Error())
		}
		return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
	default:
		prog, err := parser.ParseProgram(parser.ModeModule, src)
		if err != nil {
			return fail(b, mode, err.Error())
		}
		// Sanity-check: parse → Format → parse ≡ parse.
		// Ловит баги парсера/форматтера на реальных примерах из доков,
		// не заводя отдельного golden-файла на каждый пример.
		formatted := ast.Format(prog)
		prog2, err := parser.ParseProgram(parser.ModeModule, formatted)
		if err != nil {
			return fail(b, mode, fmt.Sprintf("format round-trip re-parse: %v", err))
		}
		if !ast.Equal(prog, prog2) {
			return fail(b, mode, "format round-trip mismatch")
		}
		return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
	}
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

// parseRepl: каждая строка — отдельный top-level стейтмент (§10.7);
// '>' — приглашение; вывод REPL отбрасывается эвристикой (A2).
func parseRepl(src string) error {
	for _, ln := range strings.Split(src, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		if line == "" {
			continue
		}
		// Вывод REPL (значения без признаков кода) отбрасывается (A2).
		if isReplOutput(line) {
			continue
		}
		if _, err := parser.ParseProgram(parser.ModeRepl, line+"\n"); err != nil {
			return fmt.Errorf("repl line %q: %w", line, err)
		}
	}
	return nil
}

// isReplOutput: строка без '=' и операторов/скобок — вероятный вывод,
// а не код (A2: "строки без =, без оператора, не начинающиеся с ключевого слова").
func isReplOutput(line string) bool {
	if strings.ContainsAny(line, "=()[]{}<>+*%|:") {
		return false
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(line) {
		return false
	}
	return true
}
