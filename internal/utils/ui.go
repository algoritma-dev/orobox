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

// AskSelection asks a multiple choice question to the user and returns the selected value.
func AskSelection(reader *bufio.Reader, question string, options []string, defaultValue string) string {
	fmt.Printf("%s\n", question)
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
