//go:build unix

package termio

import (
	"time"

	"golang.org/x/sys/unix"
)

// PollReady ждёт, пока fd станет читаемым, не дольше d (d <= 0 — не
// ждать). true — можно читать, не блокируясь на одном байте. Сигнал
// (EINTR) прерывает ожидание с false: вызывающий проверяет, что
// случилось (SIGWINCH), и ждёт снова.
func PollReady(fd int, d time.Duration) (bool, error) {
	ms := 0
	if d > 0 {
		ms = int(d / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
	}
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, ms)
	if err == unix.EINTR {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
