package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelsWithoutTaskConfiguration(t *testing.T) {
	if os.Getenv("CODEX_TASKS_MODELS_TEST_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				main()
				return
			}
		}
		panic("missing command arguments")
	}
	cache := t.TempDir()
	dir := filepath.Join(cache, "codex-tasks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "openrouter-models.json"), []byte(`{"models":[{"id":"test/model","name":"Test","context_length":32000}],"refreshed_at":"2026-09-18T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_TASKS_MODELS_TEST_PROCESS", "1")
	t.Setenv("CODEX_TASKS_CONFIG", "/missing/config")
	t.Setenv("CODEX_HOME", "relative-invalid")
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("PATH", "")
	for _, args := range [][]string{{"models", "--json"}, {"help", "models"}, {"models", "--refresh", "--help"}, {"models", "-h"}} {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestModelsWithoutTaskConfiguration$", "--"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		if args[len(args)-1] == "--json" {
			var result struct{ Models []struct{ ID string } }
			if err := json.Unmarshal(out, &result); err != nil || len(result.Models) != 1 || result.Models[0].ID != "test/model" {
				t.Fatalf("bad result: %s %v", out, err)
			}
		} else if !strings.Contains(string(out), "--refresh") {
			t.Fatalf("missing offline help: %s", out)
		}
	}
}
