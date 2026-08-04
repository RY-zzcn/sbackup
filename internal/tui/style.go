package tui

import (
	"fmt"
	"os"
	"strings"

	"sbackup/internal/terminal"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

var colorsEnabled bool

func configureStyle() {
	colorsEnabled = terminal.IsTerminal(os.Stdout.Fd()) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func styled(code, value string) string {
	if !colorsEnabled {
		return value
	}
	return code + value + ansiReset
}

func printHeader(title string) {
	const innerWidth = 52
	padding := innerWidth - 2 - displayWidth(title)
	if padding < 0 {
		padding = 0
	}
	border := styled(ansiCyan, "╭"+strings.Repeat("─", innerWidth)+"╮")
	fmt.Println("\n" + border)
	fmt.Printf("%s  %s%s%s\n", styled(ansiCyan, "│"), styled(ansiBold+ansiCyan, title), strings.Repeat(" ", padding), styled(ansiCyan, "│"))
	fmt.Println(styled(ansiCyan, "╰"+strings.Repeat("─", innerWidth)+"╯"))
}

func printMenuItem(key, label, help string) {
	fmt.Printf("  %s  %s", styled(ansiBold+ansiGreen, key), padDisplay(label, 24))
	if help != "" {
		fmt.Print(styled(ansiDim, help))
	}
	fmt.Println()
}

func printHint(message string)    { fmt.Println(styled(ansiDim, "  提示: "+message)) }
func printSuccess(message string) { fmt.Println(styled(ansiGreen, "  ✓ "+message)) }
func printWarning(message string) { fmt.Println(styled(ansiYellow, "  ! "+message)) }
func printFailure(message string) { fmt.Println(styled(ansiRed, "  ✗ "+message)) }

func statusLabel(status string) string {
	switch status {
	case "success":
		return styled(ansiGreen, "成功")
	case "warning":
		return styled(ansiYellow, "警告")
	case "failed":
		return styled(ansiRed, "失败")
	case "running":
		return styled(ansiBlue, "运行中")
	default:
		return status
	}
}

func statusCell(status string, width int) string {
	label := status
	code := ""
	switch status {
	case "success":
		label, code = "成功", ansiGreen
	case "warning":
		label, code = "警告", ansiYellow
	case "failed":
		label, code = "失败", ansiRed
	case "running":
		label, code = "运行中", ansiBlue
	}
	return styled(code, padDisplay(label, width))
}

func padDisplay(value string, width int) string {
	padding := width - displayWidth(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		if r >= 0x2e80 {
			width += 2
		} else {
			width++
		}
	}
	return width
}
