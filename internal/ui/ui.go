// Package ui centralizes the colored, aesthetically consistent output the
// CLI prints, so every command shares the same visual language.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
)

// Palette. Kept in one place so the whole tool stays visually consistent.
var (
	accent  = color.New(color.FgHiMagenta, color.Bold)
	success = color.New(color.FgHiGreen)
	warn    = color.New(color.FgHiYellow)
	fail    = color.New(color.FgHiRed, color.Bold)
	info    = color.New(color.FgHiCyan)
	dim     = color.New(color.FgHiBlack)
	label   = color.New(color.FgWhite, color.Bold)
)

// Symbols used as line prefixes.
const (
	symArrow = "❯"
	symOK    = "✔"
	symWarn  = "!"
	symErr   = "✖"
	symInfo  = "•"
	symTrash = "🗑"
)

// Banner prints the tool's header.
func Banner() {
	accent.Println()
	accent.Println("  discord-message-deleter")
	dim.Println("  cleans your message history, one channel at a time")
	fmt.Println()
}

// Step prints a primary action line.
func Step(format string, a ...any) {
	accent.Printf("%s ", symArrow)
	fmt.Printf(format+"\n", a...)
}

// Success prints a positive result line.
func Success(format string, a ...any) {
	success.Printf("%s ", symOK)
	fmt.Printf(format+"\n", a...)
}

// Warn prints a cautionary line.
func Warn(format string, a ...any) {
	warn.Printf("%s ", symWarn)
	fmt.Printf(format+"\n", a...)
}

// Error prints an error line to stderr.
func Error(format string, a ...any) {
	fail.Fprintf(os.Stderr, "%s ", symErr)
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// Info prints a neutral informational line.
func Info(format string, a ...any) {
	info.Printf("%s ", symInfo)
	fmt.Printf(format+"\n", a...)
}

// Deleted prints a line for a removed message.
func Deleted(format string, a ...any) {
	fmt.Printf("  %s ", symTrash)
	dim.Printf(format+"\n", a...)
}

// Field prints an aligned "label: value" detail line.
func Field(name, format string, a ...any) {
	label.Printf("  %-10s ", name)
	fmt.Printf(format+"\n", a...)
}

// Dim returns dimmed text, useful for inline secondary detail.
func Dim(s string) string { return dim.Sprint(s) }

// Accent returns accent-colored text.
func Accent(s string) string { return accent.Sprint(s) }

// Count returns an emphasized number for inline use.
func Count(n int) string { return color.HiCyanString("%d", n) }

// Confirm asks the user a yes/no question and returns true only for an
// explicit "y"/"yes". The default is no.
func Confirm(question string) bool {
	warn.Printf("%s ", symWarn)
	fmt.Printf("%s %s ", question, dim.Sprint("[y/N]"))

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// Rule prints a subtle separator.
func Rule() {
	dim.Println("  " + strings.Repeat("─", 40))
}
