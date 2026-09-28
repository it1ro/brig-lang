package term

import (
	"bufio"

	"github.com/it1ro/brig-lang/internal/termio"
)

// ReadKey читает одно действие из потока терминала.
// Разбор живёт в termio: его же зовёт observe, и тесты редактора
// импортируют repl, поэтому пакет term не может быть зависимостью repl.
func ReadKey(r *bufio.Reader) (termio.Key, error) { return termio.ReadKey(r) }
