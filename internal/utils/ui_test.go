package utils

import (
	"bufio"
	"strings"
	"testing"
)

func TestAskQuestion(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue string
		want         string
	}{
		{
			name:         "with input",
			input:        "custom value\n",
			defaultValue: "default",
			want:         "custom value",
		},
		{
			name:         "empty input",
			input:        "\n",
			defaultValue: "default",
			want:         "default",
		},
		{
			name:         "with spaces",
			input:        "  custom value  \n",
			defaultValue: "default",
			want:         "custom value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := AskQuestion(reader, "test question", tt.defaultValue)
			if got != tt.want {
				t.Errorf("AskQuestion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAskYesNo(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue bool
		want         bool
	}{
		{
			name:         "yes input",
			input:        "y\n",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "yes full input",
			input:        "yes\n",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "no input",
			input:        "n\n",
			defaultValue: true,
			want:         false,
		},
		{
			name:         "empty input uses default true",
			input:        "\n",
			defaultValue: true,
			want:         true,
		},
		{
			name:         "empty input uses default false",
			input:        "\n",
			defaultValue: false,
			want:         false,
		},
		{
			name:         "mixed case input",
			input:        "YeS\n",
			defaultValue: false,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := AskYesNo(reader, "test question", tt.defaultValue)
			if got != tt.want {
				t.Errorf("AskYesNo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAskSelection(t *testing.T) {
	options := []string{"7.0", "6.1", "6.0"}
	tests := []struct {
		name         string
		input        string
		defaultValue string
		want         string
	}{
		{
			name:         "valid selection 1",
			input:        "1\n",
			defaultValue: "6.1",
			want:         "7.0",
		},
		{
			name:         "valid selection 2",
			input:        "2\n",
			defaultValue: "7.0",
			want:         "6.1",
		},
		{
			name:         "empty input uses default",
			input:        "\n",
			defaultValue: "6.0",
			want:         "6.0",
		},
		{
			name:         "invalid then valid selection",
			input:        "99\n3\n",
			defaultValue: "7.0",
			want:         "6.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := AskSelection(reader, "Select version", options, tt.defaultValue)
			if got != tt.want {
				t.Errorf("AskSelection() = %v, want %v", got, tt.want)
			}
		})
	}
}
