//go:build !unix

package run

import "time"

func interactive() bool { return false }

func pollReady(int, time.Duration) (bool, error) { return false, nil }
