package tasks

import (
	"context"
	"github.com/adenta/codex-tasks/internal/buildinfo"
	"github.com/adenta/codex-tasks/internal/endpoints"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil || strings.TrimSpace(string(b)) != "1.1.0" {
		t.Fatalf("canonical version: %q %v", b, err)
	}
	original := buildinfo.BuildID
	defer func() { buildinfo.BuildID = original }()
	for _, version := range []string{"1.1.0", "development"} {
		buildinfo.BuildID = version
		var out strings.Builder
		if code, handled := Help([]string{"--version"}, &out); code != 0 || !handled || out.String() != "codex-tasks "+version+"\n" {
			t.Fatalf("version %q: %d %t %q", version, code, handled, out.String())
		}
	}
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil || !strings.Contains(string(makefile), "codex-tasks-$(VERSION)-linux-") {
		t.Fatalf("distribution is not versioned: %v", err)
	}
}

func TestHelpOfflineAndPerCommand(t *testing.T) {
	t.Setenv("CODEX_HOME", "relative-invalid")
	t.Setenv("CODEX_TASKS_CONFIG", "/missing")
	t.Setenv("PATH", "")
	for cmd := range commandHelp {
		for _, args := range [][]string{{"help", cmd}, {cmd, "--help"}, {cmd, "-h"}} {
			var out, err strings.Builder
			if code := Run(endpoints.Config{}, args, strings.NewReader(""), &out, &err); code != 0 || out.Len() < 100 || err.Len() != 0 {
				t.Fatal(args, code, out.String(), err.String())
			}
		}
	}
	var out strings.Builder
	if _, handled := Help([]string{"create", "--title", "--help"}, &out); handled {
		t.Fatal("interpreted flag value as help")
	}
}

func TestRemovedCommandsAndFlagsFailWithUsageExit(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	for _, args := range [][]string{
		{"models"}, {"environments", "--cwd", "/tmp"}, {"activity"},
		{"create", "--cwd", "/tmp", "--environment", "dev.toml"},
		{"create", "--cwd", "/tmp", "--wait-history"},
		{"list", "--source-task", id}, {"list", "--refresh"},
	} {
		var out, stderr strings.Builder
		if code := Run(endpoints.Config{}, args, strings.NewReader(""), &out, &stderr); code != 2 || stderr.Len() == 0 {
			t.Fatalf("removed interface %v: code=%d stdout=%q stderr=%q", args, code, out.String(), stderr.String())
		}
	}
}

func TestCursorRejectsEndpointRemap(t *testing.T) {
	p, _ := fixtureAccount("grace", "agent", t.TempDir())
	o := opts("find", "")
	o.Query = "review"
	o.Target = "grace/agent"
	rpc := func(_ context.Context, _ endpoints.Config, _ Options, r *Result) error {
		r.NextCursor = "more"
		return nil
	}
	r := Result{}
	if err := discover(context.Background(), p, o, &r, rpc); err != nil {
		t.Fatal(err)
	}
	o.Cursor = r.NextCursor
	p.Socket = "/changed.sock"
	if err := discover(context.Background(), p, o, &Result{}, rpc); err == nil {
		t.Fatal("cursor accepted after endpoint remap")
	}
}
