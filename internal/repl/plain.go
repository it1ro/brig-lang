package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Plain — построчный фронтенд сессии без терминала: читает порции ввода
// из In (продолжение — по NeedMore), печатает в Out значение каждого
// ввода, кроме `()`, а в Err — ошибки и диагностику. Приглашений и
// escape-кодов нет, если Prompt не задан.
type Plain struct {
	In       io.Reader
	Out, Err io.Writer
	// Prompt — приглашение перед строкой: next — номер следующего ввода
	// со значением, more — строка продолжает начатый ввод. Пишется в Err:
	// в Out — только значения. nil — без приглашений (pipe, `brig -i -`).
	Prompt func(next int, more bool) string
	// Width — колонки терминала для pretty-printer. nil или <= 0 —
	// одна строка, как Inspect.
	Width func() int
	// Pal — палитра значений в Out, ErrPal — диагностики в Err: у
	// каждого потока цвет только если он — терминал. Нулевая — без
	// escape-кодов.
	Pal, ErrPal highlight.Palette
}

// Run исполняет ввод до EOF. Ошибка ввода не завершает сессию; Run
// возвращает только ошибку чтения или записи.
func (p Plain) Run(s *Session) error {
	s.SetOutput(p.Err)
	sc := bufio.NewScanner(p.In)
	var buf strings.Builder
	for {
		if p.Prompt != nil {
			if _, err := io.WriteString(p.Err, p.Prompt(s.Next(), buf.Len() > 0)); err != nil {
				return err
			}
		}
		if !sc.Scan() {
			break
		}
		line := sc.Text()
		if buf.Len() == 0 && strings.TrimSpace(line) == "" {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		if s.NeedMore(buf.String()) {
			continue
		}
		if err := p.Eval(s, buf.String()); err != nil {
			return err
		}
		buf.Reset()
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(buf.String()) != "" {
		return p.Eval(s, buf.String())
	}
	return nil
}

// Eval исполняет порцию src и печатает в Out её значение (кроме `()`):
// связывание — `name = <значение>`, выражение — `<значение>`.
// Ошибка разбора, sema или компиляции — формат E.1, строка ввода и `^`.
// Возвращает ошибку записи и *vm.ErrHalt: Sys.halt останавливает фронтенд.
func (p Plain) Eval(s *Session, src string) error {
	if p.Err != nil {
		s.SetOutput(p.Err)
	}
	s.SetPalette(p.ErrPal)
	res, err := s.Eval(src)
	if err != nil {
		var halt *vm.ErrHalt
		if errors.As(err, &halt) {
			return err
		}
		var pe *printedError
		if !errors.As(err, &pe) {
			if werr := writeEvalError(p.Err, s.diagFile, src, err, p.opt(s, p.ErrPal)); werr != nil {
				return werr
			}
		}
		return nil
	}
	if n := len(res); n > 0 {
		if text, ok := FormatAnswer(res[n-1].Name, res[n-1].Value, p.opt(s, p.Pal)); ok {
			if _, werr := fmt.Fprintln(p.Out, text); werr != nil {
				return werr
			}
		}
	}
	return nil
}

func (p Plain) opt(s *Session, pal highlight.Palette) Print {
	w := 0
	if p.Width != nil {
		w = p.Width()
	}
	env := highlight.Env{}
	if s != nil {
		env = s.HighlightEnv()
	}
	return Print{Width: w, Pal: pal, Env: env}
}
