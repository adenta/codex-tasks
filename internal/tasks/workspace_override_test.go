package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func overrideRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial"},
	} {
		if _, err := git(context.Background(), repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func overrideWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCopyLocalOverride(t *testing.T) {
	for _, scenario := range []string{"ignored", "missing", "unignored", "tracked", "symlink", "directory", "existing", "destination-symlink", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			repo, dest := overrideRepo(t), t.TempDir()
			source := filepath.Join(repo, "AGENTS.override.md")
			target := filepath.Join(dest, "AGENTS.override.md")
			overrideWrite(t, filepath.Join(repo, ".gitignore"), "AGENTS.override.md\n")
			if scenario != "missing" {
				overrideWrite(t, source, "local instructions")
			}
			switch scenario {
			case "unignored":
				overrideWrite(t, filepath.Join(repo, ".gitignore"), "")
			case "tracked":
				if _, err := git(context.Background(), repo, "add", "-f", "--", "AGENTS.override.md"); err != nil {
					t.Fatal(err)
				}
			case "symlink", "directory":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				var err error
				if scenario == "symlink" {
					err = os.Symlink(filepath.Join(repo, ".gitignore"), source)
				} else {
					err = os.Mkdir(source, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "existing":
				overrideWrite(t, target, "existing instructions")
			case "destination-symlink":
				backing := filepath.Join(dest, "backing")
				overrideWrite(t, backing, "existing instructions")
				if err := os.Symlink(backing, target); err != nil {
					t.Fatal(err)
				}
			case "failure":
				dest = filepath.Join(dest, "missing-directory")
			}
			err := copyLocalOverride(context.Background(), repo, dest)
			if scenario == "failure" {
				if err == nil {
					t.Fatal("copy failure was ignored")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			switch scenario {
			case "ignored":
				if err != nil || string(data) != "local instructions" {
					t.Fatalf("missing copied instructions: %q %v", data, err)
				}
				info, err := os.Stat(target)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("permissions changed: %v", err)
				}
			case "existing", "destination-symlink":
				if err != nil || string(data) != "existing instructions" {
					t.Fatalf("overwrote destination: %q %v", data, err)
				}
			default:
				if !os.IsNotExist(err) {
					t.Fatalf("unexpected copy: %q %v", data, err)
				}
			}
		})
	}
}

func TestCreationCopiesOverrideBeforeThreadStart(t *testing.T) {
	repo := overrideRepo(t)
	overrideWrite(t, filepath.Join(repo, ".gitignore"), "AGENTS.override.md\n.env\n")
	overrideWrite(t, filepath.Join(repo, "AGENTS.override.md"), "local instructions")
	overrideWrite(t, filepath.Join(repo, ".env"), "not copied")
	subdir := filepath.Join(repo, "subdir")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	o := opts("create", "")
	o.CWD, o.Ref, o.Projectless = subdir, "HEAD", true
	f := &fakeRPC{handle: func(method string, params map[string]any) (any, error) {
		if method != "thread/start" {
			t.Fatalf("unexpected RPC %s", method)
		}
		dest := params["cwd"].(string)
		data, err := os.ReadFile(filepath.Join(dest, "AGENTS.override.md"))
		if err != nil || string(data) != "local instructions" {
			t.Fatalf("task started without override: %q %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(dest, ".env")); !os.IsNotExist(err) {
			t.Fatal("copied unrelated ignored file")
		}
		return map[string]any{"thread": storedTask()}, nil
	}}
	s := service{rpc: f, home: t.TempDir()}
	r := Result{}
	if err := s.create(context.Background(), o, &r); err != nil || !r.Created || len(f.methods) != 1 {
		t.Fatalf("creation failed: %+v %v", r, err)
	}
}
