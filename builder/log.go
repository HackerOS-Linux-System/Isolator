package main

import "fmt"

const (
	colReset  = "\033[0m"
	colCyan   = "\033[36m"
	colGreen  = "\033[32m"
	colYellow = "\033[33m"
	colRed    = "\033[31m"
	colBold   = "\033[1m"
)

func banner() {
	fmt.Println(colBold + colYellow + "⚠ Builder is experimental — partial/best-effort, not a production image builder yet." + colReset)
	fmt.Println()
}

func logStep(s string) { fmt.Println(colCyan + "→ " + colReset + s) }
func logDone(s string) { fmt.Println(colGreen + "✓ " + colReset + s) }
func logWarn(s string) { fmt.Println(colYellow + "⚠ " + colReset + s) }
func logErr(s string)  { fmt.Println(colRed + "✗ " + colReset + s) }
