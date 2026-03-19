package main

import (
	"embed"
	"orobox/cmd"
	"orobox/internal/docker"
)

//go:embed all:templates/*
var templatesFS embed.FS

func main() {
	docker.Templates = templatesFS
	cmd.Execute()
}
