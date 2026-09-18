package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const setupTimeout = 10 * time.Minute

type Environment struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

type environmentScript struct {
	Script string `toml:"script"`
}
type environmentConfig struct {
	Version int    `toml:"version"`
	Name    string `toml:"name"`
	Setup   struct {
		Script  string             `toml:"script"`
		Linux   *environmentScript `toml:"linux"`
		MacOS   *environmentScript `toml:"macos"`
		Windows *environmentScript `toml:"windows"`
	} `toml:"setup"`
}

func validEnvironmentID(id string) bool {
	return id != "" && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\\x00") && strings.HasSuffix(id, ".toml")
}

// Read the selected checkout's configuration, but run it in the new worktree.
// A real TOML parser is needed for multiline setup scripts and OS overrides.
func readEnvironment(cwd, id string) (*environmentConfig, error) {
	if !validEnvironmentID(id) {
		return nil, fmt.Errorf("environment must be a filename from .codex/environments")
	}
	dir := filepath.Join(cwd, ".codex", "environments")
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("environment directory is unavailable")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(dir, id))
	if err != nil || filepath.Dir(path) != realDir {
		return nil, fmt.Errorf("environment file is missing or outside its environment directory")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("environment must be a regular TOML file of at most 1 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read environment file")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("environment must be a regular TOML file of at most 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("cannot read environment file within size limit")
	}
	var config environmentConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid environment TOML")
	}
	if config.Version != 1 {
		return nil, fmt.Errorf("unsupported environment version %d", config.Version)
	}
	return &config, nil
}

func environmentRepository(ctx context.Context, cwd string) (bool, error) {
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return false, fmt.Errorf("project directory is unavailable")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if strings.Contains(string(output), "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("cannot determine whether project is a Git repository")
}

func listEnvironments(ctx context.Context, cwd string, r *Result) error {
	isGit, err := environmentRepository(ctx, cwd)
	if err != nil {
		return err
	}
	r.EnvironmentGit = &isGit
	r.Environments = []Environment{}
	if !isGit {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(cwd, ".codex", "environments"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot list project environments")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		e := Environment{ID: entry.Name(), Name: entry.Name()}
		config, err := readEnvironment(cwd, e.ID)
		if err != nil {
			e.Error = err.Error()
		} else if strings.TrimSpace(config.Name) != "" {
			e.Name = clean(config.Name, 200)
		}
		r.Environments = append(r.Environments, e)
	}
	return nil
}

func (e *environmentConfig) script() string {
	var override *environmentScript
	switch runtime.GOOS {
	case "linux":
		override = e.Setup.Linux
	case "darwin":
		override = e.Setup.MacOS
	case "windows":
		override = e.Setup.Windows
	}
	if override != nil {
		return override.Script
	}
	return e.Setup.Script
}

// Output is bounded and kept separate from the CLI's JSON response stream.
type setupOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *setupOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	const limit = 8192
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		b.data = append(b.data, p...)
		if len(b.data) > limit {
			b.data = b.data[len(b.data)-limit:]
		}
	}
	return n, nil
}

func runEnvironmentSetup(ctx context.Context, workspace string, e *environmentConfig, r *Result) error {
	r.SetupStatus = "running"
	if strings.TrimSpace(e.script()) == "" {
		r.SetupStatus = "completed"
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", e.script())
	cmd.Dir = workspace
	cmd.Stdin = nil // Setup must not consume the user's task prompt.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var output setupOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if err == nil {
		r.SetupStatus = "completed"
		return nil
	}
	r.SetupStatus, r.ErrorCategory = "failed", "environment_setup_failed"
	r.SetupOutput = clean(string(output.data), 8192)
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		r.SetupExitCode = &code
	}
	if ctx.Err() != nil {
		r.SetupStatus = "timed_out"
	}
	return fmt.Errorf("environment setup %s; no task started. Inspect retained worktree %s before creating again", r.SetupStatus, workspace)
}

func operationTimeout(o Options) time.Duration {
	timeout := 30*time.Second + o.Wait
	if len(o.Images) > 0 {
		timeout += 2 * time.Minute
	}
	if o.Environment != "" {
		timeout += setupTimeout + 30*time.Second
	}
	return timeout
}
