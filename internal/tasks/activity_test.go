package tasks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func event() Activity {
	return Activity{Event: "task_operation_outcome", Timestamp: float64(time.Now().UnixNano()) / 1e9, OperationID: uuid.NewString(), BuildID: "test", Action: "message", Host: "grace", Account: "agent", TargetHost: "grace", SourceTaskID: "unknown", TaskID: uuid.NewString(), Outcome: "accepted"}
}

func TestActivityConcurrentProcesses(t *testing.T) {
	if home := os.Getenv("TASKS_ACTIVITY_TEST_HOME"); home != "" {
		for i := 0; i < 30; i++ {
			if err := (activityLog{home: home}).append(event()); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	home := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestActivityConcurrentProcesses$")
			cmd.Env = []string{"TASKS_ACTIVITY_TEST_HOME=" + home}
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child: %v %s", err, b)
			}
		}()
	}
	wg.Wait()
	s, err := (activityLog{home: home}).read(ActivityFilter{Since: time.Now().Add(-time.Hour)})
	if err != nil || s.Status != "ok" || len(s.Events) != 120 || s.Operations != 120 {
		t.Fatalf("lost activity: %+v %v", s, err)
	}
	for _, path := range []string{"codex-tasks", "codex-tasks/tasks-activity.jsonl", "codex-tasks/tasks-activity.lock"} {
		info, err := os.Stat(filepath.Join(home, path))
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatal(path, info.Mode())
		}
	}
}

func TestActivityRotationAndOperationGrouping(t *testing.T) {
	home := t.TempDir()
	log := activityLog{home: home, limit: 1500}
	for i := 0; i < 20; i++ {
		if err := log.append(event()); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tasks-activity.jsonl", "tasks-activity.jsonl.1"} {
		info, err := os.Stat(filepath.Join(home, "codex-tasks", name))
		if err != nil || info.Size() > 1500 {
			t.Fatal(name, info, err)
		}
	}
	files, _ := os.ReadDir(filepath.Join(home, "codex-tasks"))
	if len(files) != 3 {
		t.Fatal("unexpected retained files", files)
	}
	s, err := log.read(ActivityFilter{Since: time.Now().Add(-time.Hour)})
	if err != nil || len(s.Events) >= 20 || len(s.Events) == 0 {
		t.Fatal(s, err)
	}
	e := event()
	e.Event = "task_operation_started"
	e.Outcome = "invoked"
	if err := log.append(e); err != nil {
		t.Fatal(err)
	}
	e.Event = "task_operation_outcome"
	e.Outcome = "started"
	if err := log.append(e); err != nil {
		t.Fatal(err)
	}
	s, err = log.read(ActivityFilter{Since: time.Now().Add(-time.Hour), Task: e.TaskID})
	if err != nil || s.Operations != 1 || s.Outcomes["started"] != 1 || len(s.Events) != 2 {
		t.Fatal(s, err)
	}
}

func TestActivityMalformedTailPrivacyAndNoImplicitZero(t *testing.T) {
	home := t.TempDir()
	log := activityLog{home: home}
	e := event()
	if err := log.append(e); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "codex-tasks/tasks-activity.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"broken":`)
	f.Close()
	if err := log.append(event()); err != nil {
		t.Fatal(err)
	}
	s, err := log.read(ActivityFilter{Since: time.Now().Add(-time.Hour)})
	if err != nil || s.Status != "incomplete" || len(s.Events) != 2 {
		t.Fatal(s, err)
	}
	b, _ := os.ReadFile(path)
	for _, forbidden := range []string{"prompt", "message_body", "title", "raw_error", "credentials"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("unexpected content field", forbidden)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	_ = os.WriteFile(outside, []byte("DO NOT READ OR CHANGE"), 0644)
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	s, err = log.read(ActivityFilter{Since: time.Now().Add(-time.Hour)})
	if err == nil || s.Status != "unavailable" {
		t.Fatal("symlink returned zero", s, err)
	}
	if err := log.append(event()); err == nil {
		t.Fatal("followed a log symlink")
	}
	if b, _ := os.ReadFile(outside); string(b) != "DO NOT READ OR CHANGE" {
		t.Fatal("changed target")
	}
}

func TestActivityWindowAndMissingOutcome(t *testing.T) {
	log := activityLog{home: t.TempDir()}
	e := event()
	e.Timestamp = float64(time.Now().Add(-25 * time.Hour).Unix())
	_ = log.append(e)
	e = event()
	e.Event = "task_operation_started"
	e.Outcome = "invoked"
	_ = log.append(e)
	s, err := log.read(ActivityFilter{Since: time.Now().Add(-24 * time.Hour)})
	if err != nil || s.Operations != 1 || s.Outcomes["invoked"] != 1 {
		t.Fatal(s, err)
	}
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), `"completed"`) {
		t.Fatal("orphan invocation reported complete")
	}
}
