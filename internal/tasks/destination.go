package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Destination operations always use the stock server, including local targets.
// The invoking computer owns only input snapshots, caches and operation logs.
func (s *service) commandResult(ctx context.Context, cwd string, mutation bool, argv ...string) (commandResult, error) {
	p := map[string]any{"command": argv, "timeoutMs": int64(30000), "outputBytesCap": 2 << 20, "sandboxPolicy": map[string]any{"type": "dangerFullAccess"}, "env": map[string]string{"LC_ALL": "C"}}
	if cwd != "" {
		p["cwd"] = cwd
	}
	var r commandResult
	err := s.call(ctx, "command/exec", p, &r, mutation)
	if err == nil && r.ExitCode == nil {
		if mutation {
			s.uncertain = true
		}
		err = fmt.Errorf("command response is missing its exit code")
	}
	return r, err
}
func (s *service) command(ctx context.Context, cwd string, mutation bool, argv ...string) (string, error) {
	r, err := s.commandResult(ctx, cwd, mutation, argv...)
	if err != nil {
		return "", err
	}
	if *r.ExitCode != 0 {
		return "", fmt.Errorf("%s failed on destination (exit %d)", argv[0], *r.ExitCode)
	}
	return strings.TrimSuffix(r.Stdout, "\n"), nil
}
func (s *service) git(ctx context.Context, cwd string, args ...string) (string, error) {
	mutation := len(args) > 0 && args[0] == "worktree" && len(args) > 1 && (args[1] == "add" || args[1] == "remove")
	return s.command(ctx, "", mutation, append([]string{"git", "-C", cwd}, args...)...)
}
func (s *service) realpath(ctx context.Context, path string) (string, error) {
	return s.command(ctx, "", false, "realpath", "-e", "--", path)
}

type destinationInfo struct {
	name string
	size int64
	mode os.FileMode
	mod  time.Time
}

func (i destinationInfo) Name() string       { return i.name }
func (i destinationInfo) Size() int64        { return i.size }
func (i destinationInfo) Mode() os.FileMode  { return i.mode }
func (i destinationInfo) ModTime() time.Time { return i.mod }
func (i destinationInfo) IsDir() bool        { return i.mode.IsDir() }
func (i destinationInfo) Sys() any           { return nil }
func (s *service) stat(ctx context.Context, path string, follow bool) (os.FileInfo, error) {
	args := []string{"stat"}
	if follow {
		args = append(args, "-L")
	}
	args = append(args, "-c", "%f %s %Y", "--", path)
	r, err := s.commandResult(ctx, "", false, args...)
	if err != nil {
		return nil, err
	}
	if *r.ExitCode != 0 {
		// Distinguish missing files from permission failures, without parsing locale-dependent diagnostics.
		missing, e := s.commandResult(ctx, "", false, "sh", "-c", `p=$1; while [ ! -e "$p" ] && [ ! -L "$p" ]; do parent=$(dirname -- "$p"); [ "$parent" != "$p" ] || exit 1; p=$parent; done; [ "$p" != "$1" ] && [ -d "$p" ] && [ -x "$p" ]`, "sh", path)
		if e == nil && *missing.ExitCode == 0 {
			return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
		}
		return nil, fmt.Errorf("cannot inspect destination path %s", path)
	}
	fields := strings.Fields(r.Stdout)
	if len(fields) != 3 {
		return nil, fmt.Errorf("invalid destination file metadata")
	}
	mode, e1 := strconv.ParseUint(fields[0], 16, 32)
	size, e2 := strconv.ParseInt(fields[1], 10, 64)
	stamp, e3 := strconv.ParseInt(fields[2], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return nil, fmt.Errorf("invalid destination file metadata")
	}
	m := os.FileMode(mode & 0777)
	switch mode & 0170000 {
	case 0040000:
		m |= os.ModeDir
	case 0120000:
		m |= os.ModeSymlink
	case 0100000:
	default:
		m |= os.ModeIrregular
	}
	return destinationInfo{filepath.Base(path), size, m, time.Unix(stamp, 0)}, nil
}
func (s *service) readFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	info, err := s.stat(ctx, path, true)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("destination file exceeds limit or is not regular")
	}
	var r struct {
		Data string `json:"dataBase64"`
	}
	if err = s.call(ctx, "fs/readFile", map[string]any{"path": path}, &r, false); err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil || int64(len(b)) > limit {
		return nil, fmt.Errorf("invalid or oversized file response")
	}
	return b, nil
}
func (s *service) writeFile(ctx context.Context, path string, b []byte) error {
	var r json.RawMessage
	return s.call(ctx, "fs/writeFile", map[string]any{"path": path, "dataBase64": base64.StdEncoding.EncodeToString(b)}, &r, true)
}
func (s *service) mkdir(ctx context.Context, path string, parents bool) error {
	args := []string{"mkdir", "-m", "0700"}
	if parents {
		args = append(args, "-p")
	}
	args = append(args, "--", path)
	_, err := s.command(ctx, "", true, args...)
	return err
}
func (s *service) remove(ctx context.Context, path string, recursive bool) error {
	var r json.RawMessage
	return s.call(ctx, "fs/remove", map[string]any{"path": path, "force": true, "recursive": recursive}, &r, true)
}

type directoryEntry struct {
	FileName    string `json:"fileName"`
	IsDirectory bool   `json:"isDirectory"`
}

func (s *service) readDir(ctx context.Context, path string) ([]directoryEntry, error) {
	var r struct {
		Entries []directoryEntry `json:"entries"`
	}
	err := s.call(ctx, "fs/readDirectory", map[string]any{"path": path}, &r, false)
	for _, entry := range r.Entries {
		if entry.FileName == "" || entry.FileName == "." || entry.FileName == ".." || filepath.Base(entry.FileName) != entry.FileName {
			return nil, fmt.Errorf("invalid directory entry from server")
		}
	}
	sort.Slice(r.Entries, func(i, j int) bool { return r.Entries[i].FileName < r.Entries[j].FileName })
	return r.Entries, err
}
