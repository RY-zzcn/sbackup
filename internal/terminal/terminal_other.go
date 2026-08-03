//go:build !linux

package terminal

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func IsTerminal(uintptr) bool { return false }

func ReadPassword(prompt string, reader *bufio.Reader) (string, error) {
	fmt.Print(prompt)
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	line, err := reader.ReadString('\n')
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}
