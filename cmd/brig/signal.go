package main

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/it1ro/brig-lang/internal/vm"
)

// osSignals — порт Signal (§12.12) на os/signal. Ядро VM ОС не знает
// (R14): cmd/brig подключает эту реализацию к машине (vm.SetSignals).
type osSignals struct {
	// skip — сигналы, которые подписка не получает: в REPL Ctrl-C
	// прерывает ввод и как :sigint не доставляется (§12.12).
	skip map[string]bool
}

var signalByName = map[string]os.Signal{
	"sigterm": syscall.SIGTERM,
	"sigint":  syscall.SIGINT,
}

// Open подписывает свой канал: сигнал получает каждая открытая подписка,
// и, пока она открыта, действие ОС по умолчанию не выполняется. Одна
// goroutine на подписку — события порта идут в порядке прихода (G5).
// signal.Stop в закрытии возвращает действие по умолчанию, если других
// подписок на сигнал нет.
func (h osSignals) Open(names []string, emit func(name string)) func() {
	var sigs []os.Signal
	for _, n := range names {
		if !h.skip[n] {
			sigs = append(sigs, signalByName[n])
		}
	}
	if len(sigs) == 0 {
		return func() {}
	}
	ch := make(chan os.Signal, 8)
	done := make(chan struct{})
	signal.Notify(ch, sigs...)
	go func() {
		for {
			select {
			case sig := <-ch:
				for n, s := range signalByName {
					if s == sig {
						emit(n)
					}
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// newMachine — VM программы: сигналы ОС подключены.
func newMachine() *vm.VM {
	m := vm.New()
	m.SetSignals(osSignals{})
	return m
}

// exitHalt — программа вызвала Sys.halt(code): выход с этим кодом (§12.12).
func exitHalt(err error) {
	var h *vm.ErrHalt
	if errors.As(err, &h) {
		os.Exit(h.Code)
	}
}
