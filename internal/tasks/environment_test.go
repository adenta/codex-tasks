package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

func environmentFixture(t *testing.T, config string) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial"}, {"symbolic-ref", "refs/remotes/origin/HEAD", "refs/heads/main"}} {
		if _, err := git(context.Background(), repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(repo, ".codex", "environments")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "environment.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestEnvironmentDiscoveryAndValidation(t *testing.T) {
	repo := environmentFixture(t, "version = 1\nname = 'Fixture'\n[setup]\nscript = '''\nprintf default\n'''\n[setup.linux]\nscript = 'printf linux'\n[[actions]]\nname = 'Ignored'\nscript = 'exit 9'\n")
	r := Result{}
	// Discovery is independent of the app-server socket.
	if err := executeLocal(context.Background(), endpoints.Config{}, Options{Action: "environments", CWD: repo}, &r); err != nil {
		t.Fatal(err)
	}
	if r.EnvironmentGit == nil || !*r.EnvironmentGit || len(r.Environments) != 1 || r.Environments[0].Name != "Fixture" {
		t.Fatalf("%+v", r)
	}
	e, err := readEnvironment(repo, "environment.toml")
	if err != nil || e.script() != "printf linux" {
		t.Fatalf("%+v %v", e, err)
	}
	for _, name := range []string{"../environment.toml", "/tmp/environment.toml", "missing.toml", "foo\\environment.toml"} {
		if _, err := readEnvironment(repo, name); err == nil {
			t.Fatal("accepted", name)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.toml")
	if err := os.WriteFile(outside, []byte("version=1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".codex", "environments", "outside.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := readEnvironment(repo, "outside.toml"); err == nil {
		t.Fatal("followed escaping symlink")
	}
	if err := os.WriteFile(filepath.Join(repo, ".codex", "environments", "bad.toml"), []byte("invalid = ["), 0600); err != nil {
		t.Fatal(err)
	}
	r = Result{}
	if err := listEnvironments(context.Background(), repo, &r); err != nil || len(r.Environments) != 3 || r.Environments[0].Error == "" {
		t.Fatalf("%+v %v", r, err)
	}
	r = Result{}
	if err := listEnvironments(context.Background(), t.TempDir(), &r); err != nil || r.EnvironmentGit == nil || *r.EnvironmentGit || len(r.Environments) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestEnvironmentSetupBeforeTask(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		success      bool
	}{{"success", "pwd > prepared; printf 'fixture output'", true}, {"failure", "touch prepared; printf 'fixture failure'; exit 17", false}, {"empty", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := environmentFixture(t, "version=1\n[setup]\nscript=\"\"\"\n"+tc.script+"\n\"\"\"\n")
			calls := 0
			s := service{home: t.TempDir(), rpc: &fakeRPC{handle: func(method string, p map[string]any) (any, error) {
				calls++
				if !tc.success || method != "thread/start" {
					t.Fatal("unexpected RPC", method)
				}
				cwd := p["cwd"].(string)
				if tc.script != "" {
					if b, err := os.ReadFile(filepath.Join(cwd, "prepared")); err != nil || strings.TrimSpace(string(b)) != cwd {
						t.Fatal("setup did not finish in worktree", err)
					}
				}
				return map[string]any{"thread": map[string]any{"id": "fixture", "cwd": cwd}}, nil
			}}}
			o := opts("create", "")
			o.CWD = repo
			o.Projectless = true
			o.Environment = "environment.toml"
			r := Result{}
			err := s.create(context.Background(), o, &r)
			if tc.success {
				if err != nil || calls != 1 || r.SetupStatus != "completed" {
					t.Fatalf("%+v %v calls=%d", r, err, calls)
				}
			} else {
				if err == nil || calls != 0 || r.Task != nil || r.SetupStatus != "failed" || r.SetupExitCode == nil || *r.SetupExitCode != 17 || r.SetupOutput != "fixture failure" {
					t.Fatalf("%+v %v calls=%d", r, err, calls)
				}
				if _, err := os.Stat(filepath.Join(r.Worktree, "prepared")); err != nil {
					t.Fatal("failed worktree not retained", err)
				}
			}
			if _, err := os.Stat(filepath.Join(repo, "prepared")); !os.IsNotExist(err) {
				t.Fatal("setup modified original checkout")
			}
			git(context.Background(), repo, "worktree", "remove", "--force", r.Worktree)
		})
	}
}

func TestInvalidEnvironmentStopsBeforeCreation(t *testing.T) {
	for _, config := range []string{"version=2", "invalid = ["} {
		repo := environmentFixture(t, config)
		s := service{home: t.TempDir(), rpc: &fakeRPC{handle: func(method string, p map[string]any) (any, error) {
			t.Fatal("RPC before validation", method)
			return nil, nil
		}}}
		o := opts("create", "")
		o.CWD = repo
		o.Environment = "environment.toml"
		if err := s.create(context.Background(), o, &Result{}); err == nil {
			t.Fatal("invalid environment accepted")
		}
	}
	s := service{home: t.TempDir()}
	o := opts("create", "")
	o.CWD = t.TempDir()
	o.Environment = "environment.toml"
	if err := s.create(context.Background(), o, &Result{}); err == nil {
		t.Fatal("non-Git setup accepted")
	}
}

func TestEnvironmentTimeoutAndBoundedOutput(t *testing.T) {
	e := &environmentConfig{}
	e.Setup.Script = "printf started; sleep 30; touch should-not-exist"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := Result{}
	dir := t.TempDir()
	start := time.Now()
	if err := runEnvironmentSetup(ctx, dir, e, &r); err == nil || r.SetupStatus != "timed_out" || time.Since(start) > 3*time.Second {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("setup continued after timeout")
	}
	var b setupOutput
	b.Write([]byte(strings.Repeat("x", 20000)))
	b.Write([]byte("tail"))
	if len(b.data) != 8192 || !strings.HasSuffix(string(b.data), "tail") {
		t.Fatal("output not bounded")
	}
	o := opts("create", "")
	o.Environment = "environment.toml"
	if operationTimeout(o) < setupTimeout+30*time.Second {
		t.Fatal("operation truncates setup")
	}
}

func TestEnvironmentOptionsAndRemoteDiscovery(t *testing.T) {
	for _, args := range [][]string{{"create", "--cwd", "/repo", "--checkout", "--environment", "environment.toml"}, {"create", "--projectless", "--environment", "environment.toml"}, {"create", "--cwd", "/repo", "--environment", "../x.toml"}, {"environments", "--cwd", "relative"}} {
		if _, err := parse(args, strings.NewReader("")); err == nil {
			t.Fatal("accepted", args)
		}
	}
	repo := environmentFixture(t, "version=1\nname='Remote'\n")
	p, _ := fixtureAccount("grace", "agent", t.TempDir())
	o := opts("environments", "")
	o.CWD = repo
	o.Host = "grace"
	b, _ := json.Marshal(remoteRequest{Version: tasksProtocol, Account: p.Account, Options: o})
	var out strings.Builder
	if code := runRemote(context.Background(), p, nil, strings.NewReader(string(b)), &out); code != 0 {
		t.Fatal(code, out.String())
	}
	var r Result
	if err := json.Unmarshal([]byte(out.String()), &r); err != nil || r.Error != "" || len(r.Environments) != 1 || r.Environments[0].Name != "Remote" {
		t.Fatalf("%+v %v", r, err)
	}
}
