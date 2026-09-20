package main

import (
	"fmt"
	"github.com/adenta/codex-tasks/internal/endpoints"
	"github.com/adenta/codex-tasks/internal/tasks"
	"os"
)

func main() {
	if code, handled := tasks.Help(os.Args[1:], os.Stdout); handled {
		os.Exit(code)
	}
	c, err := endpoints.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(tasks.Run(c, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
