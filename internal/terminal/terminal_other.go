//go:build !linux

package terminal

func IsTerminal(uintptr) bool { return false }
