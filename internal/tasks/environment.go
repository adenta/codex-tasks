package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
func (s *service) readEnvironment(ctx context.Context, cwd, id string) (*environmentConfig, error) {
	if !validEnvironmentID(id) {
		return nil, fmt.Errorf("environment must be a filename from .codex/environments")
	}
	dir := filepath.Join(cwd, ".codex", "environments")
	realDir, err := s.realpath(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("environment directory is unavailable")
	}
	path, err := s.realpath(ctx, filepath.Join(dir, id))
	if err != nil || filepath.Dir(path) != realDir {
		return nil, fmt.Errorf("environment file is missing or outside its environment directory")
	}
	data, err := s.readFile(ctx, path, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("cannot read environment file: %w", err)
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

func (s *service) environmentRepository(ctx context.Context, cwd string) (bool, error) {
	info, err := s.stat(ctx, cwd, true)
	if err != nil || !info.IsDir() {
		return false, fmt.Errorf("project directory is unavailable")
	}
	r, err := s.commandResult(ctx, "", false, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return false, err
	}
	if *r.ExitCode == 0 {
		return true, nil
	}
	if strings.Contains(r.Stderr, "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("cannot determine whether project is a Git repository")
}

func (s *service) listEnvironments(ctx context.Context, cwd string, r *Result) error {
	isGit, err := s.environmentRepository(ctx, cwd)
	if err != nil {
		return err
	}
	r.EnvironmentGit = &isGit
	r.Environments = []Environment{}
	if !isGit {
		return nil
	}
	_, err = s.stat(ctx, filepath.Join(cwd, ".codex", "environments"), true)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := s.readDir(ctx, filepath.Join(cwd, ".codex", "environments"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot list project environments")
	}
	for _, entry := range entries {
		if entry.IsDirectory || !strings.HasSuffix(entry.FileName, ".toml") {
			continue
		}
		e := Environment{ID: entry.FileName, Name: entry.FileName}
		config, err := s.readEnvironment(ctx, cwd, e.ID)
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

// Logs are private to the invoking account and expire after seven days.
// Keep the first 2 MiB in the file and the final 8 KiB in the result.
func (s *service) runEnvironmentSetup(ctx context.Context, workspace string, e *environmentConfig, r *Result) error {
	r.SetupStatus = "failed"
	if strings.TrimSpace(e.script()) == "" {
		r.SetupStatus = "completed"
		return nil
	}
	logHome := s.localHome
	if logHome == "" {
		logHome = s.home
	}
	dir := filepath.Join(logHome, "codex-tasks", "setup-logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create private setup log directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("setup log directory must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "setup-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > 7*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	log, err := os.CreateTemp(dir, "setup-*.log")
	if err != nil {
		return fmt.Errorf("cannot create setup log: %w", err)
	}
	r.SetupLogPath = log.Name()
	defer log.Close()
	var tail setupOutput
	written := 0
	var logErr error
	write := func(data []byte) {
		_, _ = tail.Write(data)
		const limit = 2 << 20
		if written >= limit || logErr != nil {
			return
		}
		if len(data) > limit-written {
			data = data[:limit-written]
		}
		n, err := log.Write(data)
		written += n
		if err != nil {
			logErr = err
		}
		if written == limit {
			_, logErr = log.WriteString("\n[Setup log limit reached; output truncated]\n")
		}
	}
	write([]byte("Operation: " + r.OperationID + "\nWorktree: " + workspace + "\nSetup started via Codex command/exec at " + time.Now().UTC().Format(time.RFC3339) + "\n"))
	if logErr != nil {
		return fmt.Errorf("cannot write setup log; setup was not started: %w", logErr)
	}
	r.SetupStatus = "running"
	ctx, cancel := context.WithTimeout(ctx, setupTimeout+5*time.Second)
	defer cancel()
	var reply commandResult
	params := map[string]any{
		"command": []string{"bash", "-c", e.script()}, "cwd": workspace,
		"processId": filepath.Base(r.SetupLogPath), "streamStdoutStderr": true,
		"timeoutMs": setupTimeout.Milliseconds(), "outputBytesCap": 1 << 20,
		// Environment setup already runs as the destination account, outside the
		// later task's sandbox. Server requirements may still reject this request.
		"sandboxPolicy": map[string]any{"type": "dangerFullAccess"},
	}
	err = s.rpc.CommandExec(ctx, params, &reply, write)
	if err != nil {
		r.SetupStatus, r.ErrorCategory = "unknown", "environment_setup_uncertain"
		var rejected *RPCError
		if errors.As(err, &rejected) {
			r.SetupStatus, r.ErrorCategory = "failed", "environment_setup_failed"
		} else {
			s.uncertain = true
		}
		write([]byte("\nSetup " + r.SetupStatus + ": " + err.Error() + "\n"))
	} else if reply.ExitCode == nil {
		r.SetupStatus, r.ErrorCategory = "unknown", "environment_setup_uncertain"
		s.uncertain = true
		err = fmt.Errorf("Codex did not return a setup exit code")
		write([]byte("\nSetup unknown: " + err.Error() + "\n"))
	} else {
		// Buffered fields should be empty for streaming; retain any returned diagnostics.
		write([]byte(reply.Stdout))
		write([]byte(reply.Stderr))
		r.SetupExitCode = reply.ExitCode
		r.SetupStatus = "completed"
		if *reply.ExitCode != 0 {
			r.SetupStatus, r.ErrorCategory = "failed", "environment_setup_failed"
		}
		if *reply.ExitCode == 124 {
			r.SetupStatus = "timed_out"
		}
		write([]byte(fmt.Sprintf("\nSetup %s (exit %d)\n", r.SetupStatus, *reply.ExitCode)))
	}
	if err := log.Sync(); err != nil {
		logErr = err
	}
	if logErr != nil && r.SetupStatus == "completed" {
		r.SetupStatus, r.ErrorCategory = "failed", "environment_setup_log_failed"
	}
	if r.SetupStatus == "completed" {
		return nil
	}
	r.SetupOutput = readable(string(tail.data))
	return fmt.Errorf("environment setup %s; no task started. Inspect the retained worktree and setup log before creating again", r.SetupStatus)
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
