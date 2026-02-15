// Package cli provides utilities for enhanced CLI output and user experience.
package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
)

var (
	// Colors for different message types
	successColor = color.New(color.FgGreen, color.Bold)
	errorColor   = color.New(color.FgRed, color.Bold)
	warnColor    = color.New(color.FgYellow, color.Bold)
	infoColor    = color.New(color.FgCyan)
	dimColor     = color.New(color.Faint)

	// Symbols for different message types
	successSymbol = "✓"
	errorSymbol   = "✗"
	warnSymbol    = "⚠"
	infoSymbol    = "ℹ"
)

// Success prints a success message in green with a checkmark.
func Success(format string, args ...interface{}) {
	successColor.Printf("%s ", successSymbol)
	fmt.Printf(format+"\n", args...)
}

// Error prints an error message in red with an X symbol.
func Error(format string, args ...interface{}) {
	errorColor.Fprintf(os.Stderr, "%s ", errorSymbol)
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// Warn prints a warning message in yellow with a warning symbol.
func Warn(format string, args ...interface{}) {
	warnColor.Printf("%s ", warnSymbol)
	fmt.Printf(format+"\n", args...)
}

// Info prints an info message in cyan with an info symbol.
func Info(format string, args ...interface{}) {
	infoColor.Printf("%s ", infoSymbol)
	fmt.Printf(format+"\n", args...)
}

// Dim prints dimmed text (for less important information).
func Dim(format string, args ...interface{}) {
	dimColor.Printf(format+"\n", args...)
}

// Header prints a bold header.
func Header(text string) {
	color.New(color.Bold).Println(text)
}

// Spinner creates and returns a new spinner for long-running operations.
type Spinner struct {
	s *spinner.Spinner
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(message string) *Spinner {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " " + message
	s.Color("cyan")
	return &Spinner{s: s}
}

// Start starts the spinner.
func (s *Spinner) Start() {
	s.s.Start()
}

// Stop stops the spinner.
func (s *Spinner) Stop() {
	s.s.Stop()
}

// Success stops the spinner and shows a success message.
func (s *Spinner) Success(message string) {
	s.s.Stop()
	Success(message)
}

// Error stops the spinner and shows an error message.
func (s *Spinner) Error(message string) {
	s.s.Stop()
	Error(message)
}

// UpdateMessage updates the spinner message.
func (s *Spinner) UpdateMessage(message string) {
	s.s.Suffix = " " + message
}

// ErrorWithHelp prints an error with helpful context and suggestions.
func ErrorWithHelp(err error, help string) {
	Error("%s", err)
	if help != "" {
		Dim("  → %s", help)
	}
}

// ErrorWithSuggestion prints an error with a suggested fix.
func ErrorWithSuggestion(err error, suggestion string) {
	Error("%s", err)
	if suggestion != "" {
		infoColor.Printf("  %s Try: %s\n", infoSymbol, suggestion)
	}
}

// Table provides simple table formatting.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable creates a new table with the given headers.
func NewTable(headers ...string) *Table {
	return &Table{
		headers: headers,
		rows:    make([][]string, 0),
	}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

// Print prints the table to stdout.
func (t *Table) Print() {
	if len(t.headers) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	headerColor := color.New(color.Bold, color.FgCyan)
	for i, h := range t.headers {
		headerColor.Printf("%-*s  ", widths[i], h)
	}
	fmt.Println()

	// Print separator
	for i := range t.headers {
		for j := 0; j < widths[i]; j++ {
			fmt.Print("─")
		}
		fmt.Print("  ")
	}
	fmt.Println()

	// Print rows
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) {
				fmt.Printf("%-*s  ", widths[i], cell)
			}
		}
		fmt.Println()
	}
}
