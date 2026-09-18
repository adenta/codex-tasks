package tasks

import (
	"context"
	"encoding/json"
	"errors"
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
			setupCalls := 0
			s := service{home: t.TempDir(), rpc: &fakeRPC{handle: func(method string, p map[string]any) (any, error) {
				if method == "command/exec" {
					setupCalls++
					command := p["command"].([]string)
					if len(command) != 3 || command[0] != "bash" || command[1] != "-c" || strings.TrimSpace(command[2]) != tc.script {
						t.Fatal("wrong setup command", command)
					}
					if p["timeoutMs"] != setupTimeout.Milliseconds() || p["streamStdoutStderr"] != true {
						t.Fatal("missing command controls", p)
					}
					cwd := p["cwd"].(string)
					if cwd == repo {
						t.Fatal("setup in source checkout")
					}
					if err := os.WriteFile(filepath.Join(cwd, "prepared"), []byte(cwd), 0600); err != nil {
						t.Fatal(err)
					}
					if !tc.success {
						return map[string]any{"exitCode": 17, "stderr": "fixture failure\nsecond line\n"}, nil
					}
					return map[string]any{"exitCode": 0, "stdout": "fixture output\n"}, nil
				}
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
				if err == nil || calls != 0 || r.Task != nil || r.SetupStatus != "failed" || r.SetupExitCode == nil || *r.SetupExitCode != 17 || !strings.Contains(r.SetupOutput, "fixture failure\nsecond line\n") {
					t.Fatalf("%+v %v calls=%d", r, err, calls)
				}
				if _, err := os.Stat(filepath.Join(r.Worktree, "prepared")); err != nil {
					t.Fatal("failed worktree not retained", err)
				}
			}
			if tc.script != "" {
				if setupCalls != 1 {
					t.Fatal("setup replayed or missing", setupCalls)
				}
				info, err := os.Stat(r.SetupLogPath)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("setup log is not private", err)
				}
				b, err := os.ReadFile(r.SetupLogPath)
				if err != nil || !strings.Contains(string(b), "Setup "+r.SetupStatus) {
					t.Fatal("setup log incomplete", err)
				}
			} else if setupCalls != 0 {
				t.Fatal("empty setup made command call")
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
	e.Setup.Script = "fixture command"
	for _, tc := range []struct {
		name      string
		reply     any
		err       error
		status    string
		uncertain bool
	}{
		{"timeout", map[string]any{"exitCode": 124, "stdout": "begin\n"}, nil, "timed_out", false},
		{"disconnect", nil, errors.New("disconnected"), "unknown", true},
		{"unsupported", nil, &RPCError{Method: "command/exec", Code: -32601, Message: "unsupported"}, "failed", false},
		{"missing-exit", map[string]any{}, nil, "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := service{home: t.TempDir(), rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
				calls++
				if m != "command/exec" {
					t.Fatal(m)
				}
				return tc.reply, tc.err
			}}}
			r := Result{}
			if err := s.runEnvironmentSetup(context.Background(), t.TempDir(), e, &r); err == nil || r.SetupStatus != tc.status || s.uncertain != tc.uncertain || calls != 1 {
				t.Fatalf("%+v %v calls=%d", r, err, calls)
			}
		})
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

func TestSetupLogBoundsAndRetention(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "codex-tasks", "setup-logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "setup-old.log")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	ago := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, ago, ago); err != nil {
		t.Fatal(err)
	}
	e := &environmentConfig{}
	e.Setup.Script = "fixture"
	s := service{home: home, rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		return map[string]any{"exitCode": 1, "stdout": strings.Repeat("x", 3<<20), "stderr": "\nlast line\n"}, nil
	}}}
	r := Result{}
	if err := s.runEnvironmentSetup(context.Background(), t.TempDir(), e, &r); err == nil {
		t.Fatal("failure lost")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old log retained")
	}
	info, err := os.Stat(r.SetupLogPath)
	if err != nil || info.Size() > (2<<20)+100 || info.Mode().Perm() != 0600 {
		t.Fatal("invalid log bounds or mode", info, err)
	}
	if !strings.Contains(r.SetupOutput, "\nlast line\n") {
		t.Fatal("tail/newlines lost")
	}
}
