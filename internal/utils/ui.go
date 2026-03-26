// Package utils provides utility functions for user interaction.
package utils

import (
	"bufio"
	"fmt"
	"strings"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

// PrintSuccess prints a success message in green.
func PrintSuccess(message string) {
	fmt.Printf("%s✔ %s%s\n", colorGreen, message, colorReset)
}

// PrintError prints an error message in red.
func PrintError(message string) {
	fmt.Printf("%s✘ %s%s\n", colorRed, message, colorReset)
}

// PrintWarning prints a warning message in yellow.
func PrintWarning(message string) {
	fmt.Printf("%s⚠ %s%s\n", colorYellow, message, colorReset)
}

// PrintInfo prints an informational message in blue.
func PrintInfo(message string) {
	fmt.Printf("%sℹ %s%s\n", colorBlue, message, colorReset)
}

// PrintTitle prints a title message in cyan.
func PrintTitle(message string) {
	fmt.Printf("\n%s%s%s\n", colorCyan, message, colorReset)
}

// AskQuestion asks a question to the user and returns the answer or a default value.
func AskQuestion(reader *bufio.Reader, question string, defaultValue string) string {
	fmt.Printf("%s%s%s [%s]: ", colorCyan, question, colorReset, defaultValue)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

// AskYesNo asks a yes/no question to the user and returns the boolean response.
func AskYesNo(reader *bufio.Reader, question string, defaultValue bool) bool {
	defaultStr := "y"
	if !defaultValue {
		defaultStr = "n"
	}
	fmt.Printf("%s%s (y/n)%s [%s]: ", colorCyan, question, colorReset, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultValue
	}
	return input == "y" || input == "yes"
}

// AskSelection asks a multiple choice question to the user and returns the selected value.
func AskSelection(reader *bufio.Reader, question string, options []string, defaultValue string) string {
	fmt.Printf("%s%s%s\n", colorCyan, question, colorReset)
	for i, option := range options {
		fmt.Printf("  [%d] %s\n", i+1, option)
	}

	defaultIdx := -1
	for i, option := range options {
		if option == defaultValue {
			defaultIdx = i + 1
			break
		}
	}

	if defaultIdx != -1 {
		fmt.Printf("Selection [%d]: ", defaultIdx)
	} else {
		fmt.Printf("Selection: ")
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" && defaultIdx != -1 {
		return defaultValue
	}

	var idx int
	_, err := fmt.Sscanf(input, "%d", &idx)
	if err != nil || idx < 1 || idx > len(options) {
		fmt.Println("Invalid selection, please try again.")
		return AskSelection(reader, question, options, defaultValue)
	}

	return options[idx-1]
}
