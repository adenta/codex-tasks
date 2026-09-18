package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteHelperProcess(t *testing.T) {
	if os.Getenv("TASKS_REMOTE_HELPER") != "1" {
		return
	}
	var request remoteRequest
	if json.NewDecoder(os.Stdin).Decode(&request) != nil {
		os.Exit(2)
	}
	if request.Options.Message != "SECRET_PROMPT\n`literal` $(literal)" || request.Version != tasksProtocol {
		os.Exit(2)
	}
	r := Result{OperationID: request.Options.OperationID, Action: request.Options.Action, Host: "love", Account: "agent", Outcome: "failed", ErrorCategory: "server_rejected", Error: "fixture server rejected the operation"}
	_ = json.NewEncoder(os.Stdout).Encode(r)
	os.Exit(0)
}

func TestRemoteUsesStdinRecordsOnceAndPreservesSemanticFailure(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nfor arg do last_arg=$arg; done\nif [ \"$last_arg\" = _capabilities ]; then printf '%s\\n' '{\"tasks_protocol\":6,\"host\":\"love\",\"account\":\"agent\"}'; exit 0; fi\nexec '" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run=^TestRemoteHelperProcess$\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TASKS_REMOTE_HELPER", "1")
	p, _ := fixtureRole("xps")
	p.CodexHome = t.TempDir()
	o := opts("message", storedTask().ID)
	o.Host = "love"
	o.JSON = true
	o.Message = "SECRET_PROMPT\n`literal` $(literal)"
	o.Title = "SECRET_TITLE"
	var out, stderr strings.Builder
	if code := run(context.Background(), p, o, &out, &stderr); code != 1 {
		t.Fatalf("transport masked failure: %d %s", code, out.String())
	}
	var r Result
	_ = json.Unmarshal([]byte(out.String()), &r)
	if r.Host != "love" || r.Account != "agent" || r.Outcome != "failed" || r.ErrorCategory != "server_rejected" || r.ActivityStatus != "ok" {
		t.Fatal(r)
	}
	s, err := (activityLog{home: p.CodexHome}).read(ActivityFilter{Since: time.Now().Add(-time.Hour)})
	if err != nil || s.Operations != 1 || len(s.Events) != 2 {
		t.Fatal(s, err)
	}
	b, _ := os.ReadFile(filepath.Join(p.CodexHome, "codex-tasks/tasks-activity.jsonl"))
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "fixture server") {
		t.Fatal("content leaked into activity")
	}
}

func TestRemoteUncertainWriteIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nfor arg do last_arg=$arg; done\nif [ \"$last_arg\" = _capabilities ]; then printf '%s\\n' '{\"tasks_protocol\":6,\"host\":\"love\",\"account\":\"agent\"}'; exit 0; fi\nprintf x >> '" + marker + "'\nexit 255\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	o := opts("message", storedTask().ID)
	o.Host = "love"
	r := Result{Account: "agent"}
	if err := dispatch(context.Background(), "love", o, &r); err == nil || r.Outcome != "unknown" {
		t.Fatal(r, err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "x" {
		t.Fatal("retried uncertain remote mutation")
	}
	o.Action = "read"
	r = Result{Account: "agent"}
	if err := dispatch(context.Background(), "love", o, &r); err == nil || r.Outcome != "failed" {
		t.Fatal(r, err)
	}
}

func TestRemotePreflightRejectsOldHostWithoutSending(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nfor arg do last_arg=$arg; done\nprintf '%s\\n' \"$last_arg\" >> '" + marker + "'\nexit 2\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	o := opts("create", "")
	o.Host = "grace"
	o.Projectless = true
	o.Message = "Do not send this"
	r := Result{Account: "agent"}
	if err := dispatch(context.Background(), "grace", o, &r); err == nil || r.Outcome != "failed" || r.ErrorCategory != "remote_incompatible" || r.InputAccepted || r.Task != nil {
		t.Fatalf("%+v %v", r, err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "_capabilities\n" {
		t.Fatalf("sent a mutation: %s", b)
	}
}

func TestRemoteCompatibleHostReceivesExactMessageOnce(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nfor arg do last_arg=$arg; done\nif [ \"$last_arg\" = _capabilities ]; then printf '%s\\n' '{\"tasks_protocol\":6,\"host\":\"love\",\"account\":\"agent\"}'; exit 0; fi\nexec '" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run=^TestRemoteHelperProcess$\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TASKS_REMOTE_HELPER", "1")
	o := opts("create", "")
	o.Host = "love"
	o.CWD = "/existing"
	o.Projectless = true
	o.Message = "SECRET_PROMPT\n`literal` $(literal)"
	r := Result{Account: "agent"}
	if err := dispatch(context.Background(), "love", o, &r); err != nil || r.ErrorCategory != "server_rejected" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRemoteProtocolMismatchStopsBeforeDispatch(t *testing.T) {
	for _, protocol := range []int{0, 2, 3, 4, tasksProtocol - 1, tasksProtocol + 1} {
		t.Run(fmt.Sprint(protocol), func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "calls")
			script := fmt.Sprintf("#!/bin/sh\nfor arg do last_arg=$arg; done\nprintf '%%s\\n' \"$last_arg\" >> '%s'\nprintf '%%s\\n' '{\"tasks_protocol\":%d,\"host\":\"grace\",\"account\":\"agent\"}'\n", marker, protocol)
			if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			p, _ := fixtureRole("xps")
			p.CodexHome = t.TempDir()
			o := opts("message", storedTask().ID)
			o.Host, o.Message, o.JSON = "grace", "Do not send", true
			var out, stderr strings.Builder
			if code := run(context.Background(), p, o, &out, &stderr); code != 1 {
				t.Fatalf("expected failure, got %d: %s", code, out.String())
			}
			var r Result
			if err := json.Unmarshal([]byte(out.String()), &r); err != nil {
				t.Fatal(err)
			}
			if r.Outcome != "failed" || r.ErrorCategory != "remote_incompatible" || r.InputAccepted || !strings.Contains(r.Error, "protocol mismatch") {
				t.Fatalf("unexpected result: %+v", r)
			}
			if b, _ := os.ReadFile(marker); string(b) != "_capabilities\n" {
				t.Fatalf("dispatched after mismatch: %s", b)
			}
		})
	}
}

func TestRemoteRejectsOldRequestProtocol(t *testing.T) {
	p, _ := fixtureRole("xps")
	o := opts("message", storedTask().ID)
	o.Message = "Do not send"
	b, _ := json.Marshal(remoteRequest{Version: 2, Account: p.Account, Options: o})
	var out strings.Builder
	if code := runRemote(context.Background(), p, nil, strings.NewReader(string(b)), &out); code != 2 || out.Len() != 0 {
		t.Fatalf("old request was executed: %d %s", code, out.String())
	}
}
