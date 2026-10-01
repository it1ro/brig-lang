package repl

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// printedError — диагностика уже записана в вывод сессии.
// Error() сохраняет текст исходной ошибки (`sema: …`).
type printedError struct{ err error }

func (e *printedError) Error() string { return e.err.Error() }
func (e *printedError) Unwrap() error { return e.err }

// FormatE1 — одна диагностика в формате §E.1.
func FormatE1(sev, file string, line, col int, msg string) string {
	return fmt.Sprintf("%s: %s:%d:%d: %s", sev, file, line, col, msg)
}

// DescribeCompileError достаёт позицию из ошибки лексера, парсера или
// компилятора. Незнакомая ошибка возвращается как 1:1 и err.Error(),
// located == false.
func DescribeCompileError(file string, err error) (outFile string, line, col int, msg string, located bool) {
	outFile = file
	line, col = 1, 1
	if err == nil {
		return outFile, line, col, "", false
	}
	msg = err.Error()
	var le *lexer.Error
	var pe *parser.Error
	var ce *compiler.Error
	switch {
	case errors.As(err, &le):
		return file, le.Line, le.Col, le.Msg, true
	case errors.As(err, &pe):
		return file, pe.Line, pe.Col, pe.Msg, true
	case errors.As(err, &ce):
		if ce.Line > 0 {
			line, col = ce.Line, ce.Col
		}
		if ce.File != "" {
			outFile = ce.File
		}
		return outFile, line, col, ce.Msg, true
	default:
		return outFile, line, col, msg, false
	}
}

// WriteDiagnostics печатает диагностики sema. info (затенение прелюдии)
// не является ошибкой: строка приглушается классом comment, если палитра
// включена. showLine добавляет строку исходника и `^` под колонкой.
func WriteDiagnostics(w io.Writer, file, src string, diags []sema.Diagnostic, pal highlight.Palette, env highlight.Env, showLine bool) error {
	for _, d := range diags {
		sev := "error"
		dim := false
		if d.Severity == sema.SeverityInfo {
			sev = "info"
			dim = true
		}
		if err := writeLocated(w, sev, file, d.Line, d.Col, d.Message, src, pal, env, showLine, dim); err != nil {
			return err
		}
	}
	return nil
}

func writeEvalError(w io.Writer, file, src string, err error, opt Print) error {
	var rerr *vm.ErrRaise
	if errors.As(err, &rerr) {
		return writeRaise(w, file, src, rerr, opt)
	}
	var uerr *vm.ErrUndefined
	if errors.As(err, &uerr) {
		return writeUndefined(w, file, src, uerr, opt)
	}
	f, line, col, msg, located := DescribeCompileError(file, err)
	if located {
		return writeLocated(w, "error", f, line, col, msg, src, opt.Pal, opt.Env, src != "", false)
	}
	_, werr := fmt.Fprintf(w, "%s %v\n", errorHead(opt.Pal, "error:"), err)
	return werr
}

func writeRaise(w io.Writer, file, src string, e *vm.ErrRaise, opt Print) error {
	prefix := "error: raise: "
	body := render(e.Val, false, 1, displayWidth(prefix), opt.Width, opt.Limits)
	text := joinPrefix(prefix, colorize(body, opt))
	text = errorHead(opt.Pal, "error:") + text[len("error:"):]
	if _, err := fmt.Fprintln(w, text); err != nil {
		return err
	}
	// Кадр ввода REPL — строка ввода и `^`, как у ошибки разбора (§E.1).
	for _, fr := range e.Trace {
		if inputFrame(file, src, fr) {
			if err := writeSourceLine(w, src, int(fr.Pos.Line), int(fr.Pos.Col), opt.Pal, opt.Env, false); err != nil {
				return err
			}
			break
		}
	}
	top := file
	if file == "<repl>" {
		top = "<input>"
	}
	for _, fr := range e.Trace {
		f := fr.File
		if f == "" {
			f = file
		}
		name := runtime.FrameName(fr.Func, top)
		if _, err := fmt.Fprintf(w, "  at %s (%s:%d:%d)\n", name, f, fr.Pos.Line, fr.Pos.Col); err != nil {
			return err
		}
	}
	return nil
}

// inputFrame — кадр кода верхнего уровня текущего ввода REPL: его
// позиция считается по src.
func inputFrame(file, src string, fr vm.TraceFrame) bool {
	return file == "<repl>" && src != "" && fr.File == "" && fr.Pos.Line > 0 &&
		runtime.DemangleFunc(fr.Func).Top
}

// writeUndefined — `undefined: g` в формате §E.1: позиция, строка ввода и
// `^`, если имя в коде ввода, и подсказка по похожим именам сессии.
func writeUndefined(w io.Writer, file, src string, e *vm.ErrUndefined, opt Print) error {
	msg := e.Error()
	if hint := didYouMean(e.Name, opt.Env); hint != "" {
		msg += "; did you mean " + hint + "?"
	}
	if !inputFrame(file, src, e.At) {
		if e.At.File == "" || e.At.Pos.Line < 1 {
			_, err := fmt.Fprintf(w, "%s %s\n", errorHead(opt.Pal, "error:"), msg)
			return err
		}
		return writeLocated(w, "error", e.At.File, int(e.At.Pos.Line), int(e.At.Pos.Col), msg, "", opt.Pal, opt.Env, false, false)
	}
	return writeLocated(w, "error", file, int(e.At.Pos.Line), int(e.At.Pos.Col), msg, src, opt.Pal, opt.Env, true, false)
}

func writeLocated(w io.Writer, sev, file string, line, col int, msg, src string, pal highlight.Palette, env highlight.Env, showLine, dim bool) error {
	head := FormatE1(sev, file, line, col, msg)
	if dim {
		head = paintText(pal, head, highlight.Comment)
	} else {
		head = errorHead(pal, sev+":") + head[len(sev)+1:]
	}
	if _, err := fmt.Fprintln(w, head); err != nil {
		return err
	}
	if !showLine || src == "" {
		return nil
	}
	return writeSourceLine(w, src, line, col, pal, env, dim)
}

// errorHead красит заголовок диагностики `error:` (§E.1, C.6).
func errorHead(pal highlight.Palette, head string) string {
	return paintText(pal, head, highlight.Error)
}

// writeSourceLine печатает строку line из src и `^` под колонкой col.
func writeSourceLine(w io.Writer, src string, line, col int, pal highlight.Palette, env highlight.Env, dim bool) error {
	if line < 1 {
		line = 1
	}
	if _, err := fmt.Fprintln(w, paintedLine(src, line, pal, env)); err != nil {
		return err
	}
	if col < 1 {
		col = 1
	}
	caret := strings.Repeat(" ", col-1) + "^"
	cls := highlight.Error
	if dim {
		cls = highlight.Comment
	}
	if _, err := fmt.Fprintln(w, paintText(pal, caret, cls)); err != nil {
		return err
	}
	return nil
}

func paintedLine(src string, n int, pal highlight.Palette, env highlight.Env) string {
	if env.Prelude == nil && env.Modules == nil && env.Bindings == nil {
		env = highlight.REPLEnv()
	}
	res := highlight.Classify(src, -1, env)
	res.Guides = nil
	lines := strings.Split(pal.Paint(src, res), "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}

func paintText(pal highlight.Palette, text string, c highlight.Class) string {
	if text == "" {
		return ""
	}
	return pal.Paint(text, highlight.Result{Spans: []highlight.Span{{
		Start: 0, End: len(text), Class: c,
	}}})
}
