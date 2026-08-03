//go:build linux

package terminal

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func IsTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return errno == 0
}

func ReadPassword(prompt string, reader *bufio.Reader) (string, error) {
	fd := os.Stdin.Fd()
	var old syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&old)), 0, 0, 0)
	if errno != 0 {
		return "", errno
	}
	updated := old
	updated.Lflag &^= syscall.ECHO
	_, _, errno = syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&updated)), 0, 0, 0)
	if errno != 0 {
		return "", errno
	}
	fmt.Print(prompt)
	defer func() {
		_, _, _ = syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&old)), 0, 0, 0)
		fmt.Println()
	}()
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	line, err := reader.ReadString('\n')
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}
