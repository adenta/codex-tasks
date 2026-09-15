package tasks

import (
	"context"
	"github.com/adenta/codex-tasks/internal/endpoints"
	"strings"
	"testing"
)

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
