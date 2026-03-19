// Package main is the entry point for the Orobox CLI.
package main

import (
	"embed"
	"github.com/algoritma-dev/orobox/cmd"
	"github.com/algoritma-dev/orobox/internal/docker"
)

//go:embed all:templates/*
var templatesFS embed.FS

func main() {
	docker.Templates = templatesFS
	cmd.Execute()
}
