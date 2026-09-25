package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

// replLoop — persistent REPL (§11.4, N12).
//
// Строки читаются построчно; незакрытые скобки/интерполяции активируют
// продолжение ввода. Каждая полная строка исполняется в persistent ВМ.
// Многострочные offside-блоки (fn/match/recv) в MVP REPL не поддерживаются —
// для них используйте `.brig`-файл и `brig run`.
func replLoop() {
	machine := vm.New()
	r := repl.New(machine, os.Stderr)

	sc := bufio.NewScanner(os.Stdin)
	fmt.Fprintln(os.Stderr, "brig repl (persistent): введите выражение; Ctrl-D — выход")
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
		// Многострочный ввод при незакрытых скобках.
		buf := line
		for repl.IsContinuation(buf) {
			fmt.Print("      ... ")
			if !sc.Scan() {
				break
			}
			buf += "\n" + sc.Text()
		}

		res, err := r.Eval(buf + "\n")
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %v\n", err)
			continue
		}
		if res.Kind != 0 { // KindUnit == 0
			fmt.Printf("  => %s\n", res.Inspect())
		}
		in++
	}
	fmt.Fprintln(os.Stderr, "bye")
}
