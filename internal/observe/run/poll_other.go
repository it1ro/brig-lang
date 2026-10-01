//go:build !unix

package run

func interactive() bool { return false }
