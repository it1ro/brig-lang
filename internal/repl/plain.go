package repl

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// Plain — построчный фронтенд сессии без терминала: читает порции ввода
// из In (продолжение — по NeedMore), печатает в Out значение каждого
// ввода, кроме `()`, а в Err — ошибки и диагностику. Приглашений и
// escape-кодов нет, если Prompt не задан.
type Plain struct {
	In       io.Reader
	Out, Err io.Writer
	// Prompt — приглашение перед строкой: next — номер следующего ввода
	// со значением, more — строка продолжает начатый ввод. nil — без
	// приглашений (pipe, `brig -i -`).
	Prompt func(next int, more bool) string
}

// Run исполняет ввод до EOF. Ошибка ввода не завершает сессию; Run
// возвращает только ошибку чтения или записи.
func (p Plain) Run(s *Session) error {
	s.SetOutput(p.Err)
	sc := bufio.NewScanner(p.In)
	var buf strings.Builder
	for {
		if p.Prompt != nil {
			if _, err := io.WriteString(p.Out, p.Prompt(s.Next(), buf.Len() > 0)); err != nil {
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
		if err := p.eval(s, buf.String()); err != nil {
			return err
		}
		buf.Reset()
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(buf.String()) != "" {
		return p.eval(s, buf.String())
	}
	return nil
}

// eval исполняет порцию и печатает её значение или ошибку.
func (p Plain) eval(s *Session, src string) error {
	res, err := s.Eval(src)
	if err != nil {
		_, werr := fmt.Fprintf(p.Err, "error: %v\n", err)
		return werr
	}
	if n := len(res); n > 0 && res[n-1].Value.Kind != runtime.KindUnit {
		_, werr := fmt.Fprintln(p.Out, res[n-1].Value.Inspect())
		return werr
	}
	return nil
}
