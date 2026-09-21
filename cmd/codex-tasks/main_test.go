package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHelpAndVersionWithoutTaskConfiguration(t *testing.T) {
	if os.Getenv("CODEX_TASKS_MAIN_TEST_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				main()
				return
			}
		}
		panic("missing command arguments")
	}
	t.Setenv("CODEX_TASKS_MAIN_TEST_PROCESS", "1")
	t.Setenv("CODEX_TASKS_CONFIG", "/missing/config")
	t.Setenv("CODEX_HOME", "relative-invalid")
	t.Setenv("PATH", "")
	for _, args := range [][]string{{"help", "create"}, {"create", "--help"}, {"--version"}} {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelpAndVersionWithoutTaskConfiguration$", "--"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		if args[0] == "--version" && !strings.HasPrefix(string(out), "codex-tasks ") {
			t.Fatalf("unexpected version output: %s", out)
		}
	}
}
