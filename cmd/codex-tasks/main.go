package main

import (
	"fmt"
	"github.com/adenta/codex-tasks/internal/catalog"
	"github.com/adenta/codex-tasks/internal/endpoints"
	"github.com/adenta/codex-tasks/internal/tasks"
	"os"
)

func main() {
	if code, handled := tasks.Help(os.Args[1:], os.Stdout); handled {
		os.Exit(code)
	}
	if os.Args[1] == "models" {
		os.Exit(catalog.Run(os.Args[2:], os.Stdout, os.Stderr))
	}
	c, err := endpoints.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(tasks.Run(c, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
