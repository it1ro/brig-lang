// Package examples implements the check-examples tool (A2): extracting
// ```brig fenced blocks from design docs and running them through the parser.
package examples

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// Result — отчёт по одному блоку (A2).
type Result struct {
	File   string
	Line   int
	Col    int
	Mode   string
	OK     bool
	ErrMsg string
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

// checkBlock: выбор режима, обёртка expr/stmt, парсинг, sanity-check.
func checkBlock(b block) Result {
	meta := strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))
	mode := modeByMeta(meta)
	if mode == "" {
		mode = heuristicMode(b.raw)
	}

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

// parseRepl: каждая строка — отдельный top-level стейтмент (§10.7).
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
		if isReplOutput(line) {
			continue
		}
		if _, err := parser.ParseProgram(parser.ModeRepl, line+"\n"); err != nil {
			return fmt.Errorf("repl line %q: %w", line, err)
		}
	}
	return nil
}

// isReplOutput: строка без '=' и операторов/скобок — вероятный вывод (A2).
func isReplOutput(line string) bool {
	if strings.ContainsAny(line, "=()[]{}<>+*%|:") {
		return false
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(line) {
		return false
	}
	return true
}
