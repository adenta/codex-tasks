package tasks

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
)

// A fixture backend for unit tests; production has no direct filesystem backend.
func fixtureDestination(ctx context.Context, m string, p map[string]any) (any, error, bool) {
	switch m {
	case "command/exec":
		if p["streamStdoutStderr"] == true {
			return nil, nil, false
		}
		args := p["command"].([]string)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if cwd, ok := p["cwd"].(string); ok {
			cmd.Dir = cwd
		}
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				return nil, err, true
			}
		}
		return map[string]any{"exitCode": code, "stdout": out.String(), "stderr": stderr.String()}, nil, true
	case "fs/readFile":
		b, err := os.ReadFile(p["path"].(string))
		return map[string]any{"dataBase64": base64.StdEncoding.EncodeToString(b)}, err, true
	case "fs/writeFile":
		b, err := base64.StdEncoding.DecodeString(p["dataBase64"].(string))
		if err == nil {
			err = os.WriteFile(p["path"].(string), b, 0600)
		}
		return map[string]any{}, err, true
	case "fs/readDirectory":
		entries, err := os.ReadDir(p["path"].(string))
		rows := []any{}
		for _, e := range entries {
			rows = append(rows, map[string]any{"fileName": e.Name(), "isDirectory": e.IsDir()})
		}
		return map[string]any{"entries": rows}, err, true
	case "fs/remove":
		var err error
		if p["recursive"] == true {
			err = os.RemoveAll(p["path"].(string))
		} else {
			err = os.Remove(p["path"].(string))
			if os.IsNotExist(err) {
				err = nil
			}
		}
		return map[string]any{}, err, true
	}
	return nil, nil, false
}
func git(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	b, err := cmd.Output()
	return string(bytes.TrimSpace(b)), err
}
func readEnvironment(cwd, id string) (*environmentConfig, error) {
	return (&service{rpc: &fakeRPC{}}).readEnvironment(context.Background(), cwd, id)
}
func listEnvironments(ctx context.Context, cwd string, r *Result) error {
	return (&service{rpc: &fakeRPC{}}).listEnvironments(ctx, cwd, r)
}
func copyLocalOverride(ctx context.Context, a, b string) error {
	return (&service{rpc: &fakeRPC{}}).copyLocalOverride(ctx, a, b)
}
func newProjectlessWorkspace() (string, error) {
	return (&service{rpc: &fakeRPC{}}).newProjectlessWorkspace(context.Background())
}
