package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/parser"
)

// replLoop — отладочный цикл: лексирует ввод, показывает токены.
// Многострочные вводы (INDENT-тела fn/match/recv/...) накапливаются
// по правилам offside (A5) — TODO Трек B, см. скилл brig-cli.
func replLoop() {
	sc := bufio.NewScanner(os.Stdin)
	fmt.Println("brig repl (debug, Трек B): введите выражение; Ctrl-D — выход")
	in := 0
	for {
		fmt.Printf("brig[%d]> ", in+1)
		if !sc.Scan() {
			break
		}
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Продолжение при незакрытых скобках (parenDepth > 0 после лексинга).
		buf := line
		for {
			if _, err := lexer.Lex(buf); err == nil {
				break
			}
			fmt.Print("      ... ")
			if !sc.Scan() {
				break
			}
			buf += "\n" + sc.Text()
		}
		toks, err := lexer.Lex(buf)
		if err != nil {
			fmt.Printf("  ! %v\n", err)
			continue
		}
		if pErr := parser.Parse(parser.ModeRepl, buf+"\n"); pErr != nil {
			fmt.Printf("  ! %v\n", pErr)
		}
		for _, tk := range toks {
			if tk.Type == lexer.EOF {
				continue
			}
			fmt.Printf("  %s\n", tk)
		}
		in++
	}
	fmt.Println("bye")
}
