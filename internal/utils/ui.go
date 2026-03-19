// Package utils provides utility functions for user interaction.
package utils

import (
	"bufio"
	"fmt"
	"strings"
)

// AskQuestion asks a question to the user and returns the answer or a default value.
func AskQuestion(reader *bufio.Reader, question string, defaultValue string) string {
	fmt.Printf("%s [%s]: ", question, defaultValue)
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
	fmt.Printf("%s (y/n) [%s]: ", question, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultValue
	}
	return input == "y" || input == "yes"
}
