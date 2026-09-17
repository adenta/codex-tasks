package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectlessAllocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, err := newProjectlessWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newProjectlessWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, filepath.Join(home, "Documents", "Codex")+string(os.PathSeparator)) {
		t.Fatalf("invalid allocation %q %q", a, b)
	}
	for _, p := range []string{a, b, filepath.Join(a, "work"), filepath.Join(a, "outputs")} {
		i, err := os.Stat(p)
		if err != nil || !i.IsDir() {
			t.Fatalf("missing directory %s", p)
		}
	}
	instructions := projectlessInstructions(a)
	if !strings.Contains(instructions, filepath.Join(a, "outputs")) || !strings.Contains(instructions, filepath.Join(a, "work")) {
		t.Fatal(instructions)
	}
}
func TestLauncherOptions(t *testing.T) {
	if _, err := parse([]string{"create", "--projectless", "--wait-history", "--message-file", "-"}, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := parse([]string{"create", "--projectless", "--wait-history"}, strings.NewReader("")); err == nil {
		t.Fatal("accepted missing first message")
	}
	if _, err := parse([]string{"create", "--cwd", "relative"}, strings.NewReader("")); err == nil {
		t.Fatal("accepted relative cwd")
	}
}
func TestGeneratedWorkspaceInstructions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := service{rpc: &fakeRPC{handle: func(method string, p map[string]any) (any, error) {
		if method != "thread/start" {
			t.Fatalf("unexpected method %s", method)
		}
		cwd := p["cwd"].(string)
		if !strings.Contains(p["developerInstructions"].(string), filepath.Join(cwd, "outputs")) {
			t.Fatal("missing instructions")
		}
		if _, present := p["projectId"]; present {
			t.Fatal("project assignment on projectless task")
		}
		return map[string]any{"thread": map[string]any{"id": "generated", "cwd": cwd}}, nil
	}}}
	r := Result{}
	if err := s.create(context.Background(), Options{Projectless: true}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Workspace == "" || !r.Created {
		t.Fatalf("missing receipt: %+v", r)
	}
}
