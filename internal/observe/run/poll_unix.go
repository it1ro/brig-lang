//go:build unix

package run

import (
	"time"

	"golang.org/x/sys/unix"
)

func interactive() bool { return true }

// pollReady ждёт, пока fd станет читаемым, не дольше d.
// true — можно читать, не блокируясь на одном байте.
func pollReady(fd int, d time.Duration) (bool, error) {
	ms := 0
	if d > 0 {
		ms = int(d / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
	}
	for {
		n, err := unix.Poll([]unix.PollFd{{
			Fd:     int32(fd),
			Events: unix.POLLIN,
		}}, ms)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		return n > 0, nil
	}
}
