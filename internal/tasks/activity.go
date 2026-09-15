package tasks

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const activityLimit = 5 << 20

// Activity deliberately has no field for prompts, replies, titles or raw errors.
type Activity struct {
	Event         string  `json:"event"`
	Timestamp     float64 `json:"timestamp"`
	OperationID   string  `json:"operation_id"`
	BuildID       string  `json:"build_id"`
	Action        string  `json:"action"`
	Host          string  `json:"host"`
	Account       string  `json:"account"`
	TargetHost    string  `json:"target_host"`
	TargetAccount string  `json:"target_account,omitempty"`
	SourceTaskID  string  `json:"source_task_id"`
	TaskID        string  `json:"task_id,omitempty"`
	TurnID        string  `json:"turn_id,omitempty"`
	Outcome       string  `json:"outcome"`
	InputAccepted bool    `json:"input_accepted,omitempty"`
	ErrorCategory string  `json:"error_category,omitempty"`
	DurationMS    int64   `json:"duration_ms,omitempty"`
}

type ActivityFilter struct {
	Since                 time.Time
	Task, Action, Outcome string
	Limit                 int
}
type ActivitySnapshot struct {
	Status      string         `json:"status"`
	WindowStart time.Time      `json:"window_start"`
	WindowEnd   time.Time      `json:"window_end"`
	Retention   string         `json:"retention"`
	Truncated   bool           `json:"truncated"`
	Operations  int            `json:"operations"`
	Outcomes    map[string]int `json:"outcomes"`
	Events      []Activity     `json:"events"`
}

func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

type activityLog struct {
	home  string
	limit int
}

func privateFile(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("activity path is not a regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
		f.Close()
		return nil, fmt.Errorf("activity file belongs to another account")
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (l activityLog) locked(create bool, visit func(string) error) error {
	root, err := filepath.EvalSymlinks(l.home)
	if err != nil {
		return fmt.Errorf("Codex home is unavailable")
	}
	dir := filepath.Join(root, "codex-tasks")
	if create {
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("activity directory is not a real directory")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("activity directory belongs to another account")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	lock, err := privateFile(filepath.Join(dir, "tasks-activity.lock"), syscall.O_CREAT|syscall.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	// A bounded lock wait avoids letting diagnostics stall task control.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return fmt.Errorf("activity log busy or unavailable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return visit(filepath.Join(dir, "tasks-activity.jsonl"))
}

func (l activityLog) append(e Activity) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	limit := l.limit
	if limit == 0 {
		limit = activityLimit
	}
	if len(b) > limit {
		return fmt.Errorf("activity record too large")
	}
	return l.locked(true, func(path string) error {
		f, err := privateFile(path, syscall.O_CREAT|syscall.O_APPEND|syscall.O_RDWR)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		// Finish a torn final line so subsequent records remain independently readable.
		separator := false
		if info.Size() > 0 {
			var last [1]byte
			_, err = f.ReadAt(last[:], info.Size()-1)
			if err != nil {
				f.Close()
				return err
			}
			separator = last[0] != '\n'
		}
		if info.Size()+int64(len(b))+1 > int64(limit) {
			f.Close()
			if err := os.Rename(path, path+".1"); err != nil {
				return err
			}
			f, err = privateFile(path, syscall.O_CREAT|syscall.O_APPEND|syscall.O_RDWR)
			if err != nil {
				return err
			}
			separator = false
		}
		defer f.Close()
		if separator {
			if _, err = f.Write([]byte{'\n'}); err != nil {
				return err
			}
		}
		if _, err := f.Write(b); err != nil {
			return err
		}
		return f.Sync()
	})
}

func (l activityLog) read(filter ActivityFilter) (ActivitySnapshot, error) {
	now := time.Now().UTC()
	s := ActivitySnapshot{Status: "ok", WindowStart: filter.Since.UTC(), WindowEnd: now, Retention: "size bounded: active and one rotated file, 5 MiB each; requested time coverage is not guaranteed", Events: []Activity{}, Outcomes: map[string]int{}}
	latest := map[string]string{}
	err := l.locked(false, func(path string) error {
		for _, p := range []string{path + ".1", path} {
			f, err := privateFile(p, syscall.O_RDONLY)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			// Bound even externally modified files; do not silently report partial counts.
			info, err := f.Stat()
			if err != nil {
				f.Close()
				return err
			}
			if info.Size() > activityLimit {
				f.Close()
				return fmt.Errorf("activity file exceeds retention limit")
			}
			scanner := bufio.NewScanner(io.LimitReader(f, activityLimit+1))
			scanner.Buffer(make([]byte, 4096), 64<<10)
			for scanner.Scan() {
				var e Activity
				if json.Unmarshal(scanner.Bytes(), &e) != nil || !validActivity(e) {
					s.Status = "incomplete"
					continue
				}
				at := time.UnixMilli(int64(e.Timestamp * 1000))
				if at.Before(filter.Since) || at.After(now) || (filter.Task != "" && e.TaskID != filter.Task && e.SourceTaskID != filter.Task) || (filter.Action != "" && e.Action != filter.Action) {
					continue
				}
				latest[e.OperationID] = e.Outcome
				if filter.Outcome != "" && e.Outcome != filter.Outcome {
					continue
				}
				s.Events = append(s.Events, e)
			}
			err = scanner.Err()
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		s.Status = "unavailable"
		return s, fmt.Errorf("activity history unavailable: %w", err)
	}
	for _, outcome := range latest {
		if filter.Outcome == "" || filter.Outcome == outcome {
			s.Operations++
			s.Outcomes[outcome]++
		}
	}
	if filter.Limit > 0 && len(s.Events) > filter.Limit {
		s.Truncated = true
		s.Events = s.Events[len(s.Events)-filter.Limit:]
	}
	return s, nil
}

func validActivity(e Activity) bool {
	if (e.Event != "task_operation_started" && e.Event != "task_operation_outcome") || e.Timestamp <= 0 || e.Timestamp > 1e12 || e.OperationID == "" {
		return false
	}
	// Bound and sanitize fields even when consuming externally altered log files.
	for _, s := range []string{e.OperationID, e.BuildID, e.Action, e.Host, e.Account, e.TargetHost, e.TargetAccount, e.SourceTaskID, e.TaskID, e.TurnID, e.Outcome, e.ErrorCategory} {
		if clean(s, 200) != s {
			return false
		}
	}
	return true
}
