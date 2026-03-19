package utils

import (
	"bufio"
	"fmt"
	"strings"
)

func AskQuestion(reader *bufio.Reader, question string, defaultValue string) string {
	fmt.Printf("%s [%s]: ", question, defaultValue)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

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
