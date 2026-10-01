//go:build !unix

package termio

import "time"

// PollReady без poll(2): ввод считается готовым, чтение блокирует.
func PollReady(int, time.Duration) (bool, error) { return true, nil }
