package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRPC struct {
	methods []string
	handle  func(string, map[string]any) (any, error)
}

func (f *fakeRPC) Call(_ context.Context, m string, p, out any) error {
	f.methods = append(f.methods, m)
	v, err := f.handle(m, p.(map[string]any))
	if err != nil {
		return err
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}
func storedTask() Task {
	t := Task{ID: uuid.NewString(), Name: "Preserve this title", Model: "configured-model"}
	t.Status.Type = "notLoaded"
	effort := "high"
	t.ReasoningEffort = &effort
	return t
}
func opts(action, id string) Options {
	return Options{Action: action, TaskID: id, Limit: 20, OperationID: uuid.NewString()}
}

func TestMessageResumesUnloadedTaskAndPreservesSettings(t *testing.T) {
	task := storedTask()
	turn := Turn{ID: uuid.NewString(), Status: "inProgress"}
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		switch m {
		case "thread/read":
			return map[string]any{"thread": task}, nil
		case "thread/resume":
			if !reflect.DeepEqual(p, map[string]any{"threadId": task.ID, "excludeTurns": true}) {
				t.Fatalf("resume overrides settings: %#v", p)
			}
			task.Status.Type = "idle"
			yes := true
			task.CanAcceptDirectInput = &yes
			return map[string]any{"thread": task}, nil
		case "turn/start":
			if len(p) != 3 || p["threadId"] != task.ID {
				t.Fatalf("turn overrides settings: %#v", p)
			}
			return map[string]any{"turn": turn}, nil
		default:
			return nil, fmt.Errorf("unexpected %s", m)
		}
	}}
	s := service{rpc: f}
	o := opts("message", task.ID)
	o.Message = "do the authorized review"
	r := Result{Outcome: "ok"}
	if err := s.execute(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "started" || r.TurnID != turn.ID || !reflect.DeepEqual(f.methods, []string{"thread/read", "thread/resume", "turn/start"}) {
		t.Fatalf("did not start: %+v %v", r, f.methods)
	}
}

func TestActiveMessageUsesExpectedTurnAndDoesNotClaimConsumption(t *testing.T) {
	task := storedTask()
	task.Status.Type = "active"
	yes := true
	task.CanAcceptDirectInput = &yes
	turn := Turn{ID: uuid.NewString(), Status: "inProgress"}
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		switch m {
		case "thread/read":
			return map[string]any{"thread": task}, nil
		case "thread/turns/list":
			return map[string]any{"data": []Turn{turn}}, nil
		case "turn/steer":
			if p["expectedTurnId"] != turn.ID || len(p) != 4 {
				t.Fatalf("bad steer: %#v", p)
			}
			return map[string]any{"turnId": turn.ID}, nil
		default:
			return nil, fmt.Errorf("unexpected %s", m)
		}
	}}
	s := service{rpc: f}
	o := opts("message", task.ID)
	o.Message = "steering"
	r := Result{}
	if err := s.execute(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "accepted" {
		t.Fatalf("claimed execution of steered input: %+v", r)
	}
}

func TestMutationUncertaintyIsNotRetried(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			task := storedTask()
			task.Status.Type = "idle"
			yes := true
			task.CanAcceptDirectInput = &yes
			f := &fakeRPC{handle: func(m string, _ map[string]any) (any, error) {
				if m == "thread/read" {
					return map[string]any{"thread": task}, nil
				}
				if explicit {
					return nil, &RPCError{m, -32000, "active turn changed"}
				}
				return nil, errors.New("disconnected")
			}}
			s := service{rpc: f}
			o := opts("message", task.ID)
			o.Message = "do not duplicate"
			r := Result{}
			if err := s.execute(context.Background(), o, &r); err == nil {
				t.Fatal("missing failure")
			}
			if s.uncertain == explicit || len(f.methods) != 2 {
				t.Fatalf("wrong retry/uncertainty: %v %v", s.uncertain, f.methods)
			}
		})
	}
}

func TestPendingApprovalIsSurfacedWithoutInput(t *testing.T) {
	task := storedTask()
	task.Status.Type = "active"
	task.Status.ActiveFlags = []string{"waitingOnApproval"}
	f := &fakeRPC{handle: func(m string, _ map[string]any) (any, error) { return map[string]any{"thread": task}, nil }}
	s := service{rpc: f}
	o := opts("message", task.ID)
	o.Message = "review"
	r := Result{}
	if err := s.execute(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if len(f.methods) != 1 || r.Outcome != "needs_attention" {
		t.Fatalf("approval bypassed: %+v %v", r, f.methods)
	}
}

func TestModeChangePreservesModelEffortAndDoesNotInterrupt(t *testing.T) {
	task := storedTask()
	task.Status.Type = "active"
	yes := true
	task.CanAcceptDirectInput = &yes
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m == "thread/read" {
			return map[string]any{"thread": task}, nil
		}
		if m != "thread/settings/update" {
			t.Fatalf("unexpected %s", m)
		}
		mode := p["collaborationMode"].(map[string]any)
		settings := mode["settings"].(map[string]any)
		if len(p) != 2 || mode["mode"] != "plan" || settings["model"] != task.Model || !reflect.DeepEqual(settings["reasoning_effort"], task.ReasoningEffort) || settings["developer_instructions"] != nil {
			t.Fatalf("settings changed: %#v", p)
		}
		return map[string]any{}, nil
	}}
	s := service{rpc: f}
	o := opts("mode", task.ID)
	o.Mode = "plan"
	r := Result{}
	if err := s.execute(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if r.Mode != "plan" || r.Outcome != "settings_updated" {
		t.Fatal(r)
	}
}

func TestProgressReportsSemanticFailure(t *testing.T) {
	task := storedTask()
	task.Status.Type = "idle"
	turn := Turn{ID: uuid.NewString(), Status: "failed"}
	f := &fakeRPC{handle: func(m string, _ map[string]any) (any, error) {
		if m == "thread/read" {
			return map[string]any{"thread": task}, nil
		}
		return map[string]any{"data": []Turn{turn}}, nil
	}}
	s := service{rpc: f}
	o := opts("progress", task.ID)
	r := Result{Outcome: "ok"}
	if err := s.execute(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "failed" || r.ErrorCategory != "turn_failed" {
		t.Fatalf("transport success masked failure: %+v", r)
	}
}

func TestProgressDoesNotMistakeQueuedStartForInterruptedHistory(t *testing.T) {
	task := storedTask()
	task.Status.Type = "idle"
	turn := Turn{ID: uuid.NewString(), Status: "interrupted"}
	calls := 0
	f := &fakeRPC{handle: func(m string, _ map[string]any) (any, error) {
		if m == "thread/read" {
			return map[string]any{"thread": task}, nil
		}
		calls++
		if calls > 1 {
			turn.Status = "completed"
		}
		return map[string]any{"data": []Turn{turn}}, nil
	}}
	s := service{rpc: f}
	o := opts("progress", task.ID)
	o.Wait = 2 * time.Second
	o.TurnID = turn.ID
	r := Result{Task: &task, TurnID: turn.ID, TurnStatus: "inProgress", Outcome: "started"}
	if err := s.progress(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "completed" || calls != 2 {
		t.Fatalf("queued-start race: %+v %d", r, calls)
	}
}

func TestCLIValidationAndHostInventory(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	id := uuid.NewString()
	o, err := parse([]string{"message", "codex://threads/" + id, "--message-file", "-", "--json"}, strings.NewReader("quoted `message` $(literal)"))
	if err != nil || !o.JSON || o.Message != "quoted `message` $(literal)" {
		t.Fatalf("%+v %v", o, err)
	}
	for _, args := range [][]string{{"message", id}, {"list", "--mode", "plan"}, {"create", "--cwd", "relative"}, {"progress", id, "--wait", "61s"}, {"read", "not-a-task"}, {"mode", id}, {"message", id, "--message-file", "-", "--model", "changed"}} {
		if _, err := parse(args, strings.NewReader("body")); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	xps, _ := fixtureRole("xps")
	alias, account, err := connection(xps, "grace")
	if err != nil || alias != "grace" || account != "agent" {
		t.Fatalf("%s %s %v", alias, account, err)
	}
	grace, _ := fixtureAccount("grace", "agent", "/home/agent")
	if _, _, err = connection(grace, "missing"); err == nil {
		t.Fatal("unconfigured route accepted")
	}
	if _, _, err := connection(grace, "love"); err == nil {
		t.Fatal("used the desktop account's connection for the agent account")
	}
}

func TestWorktreeIsolationAndMissingDefaultRef(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial"}} {
		if _, err := git(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "uncommitted"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	s := service{home: home}
	o := opts("create", "")
	o.CWD = repo
	if _, _, err := s.workspace(ctx, o); err == nil {
		t.Fatal("guessed missing default ref")
	}
	if _, err := git(ctx, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	path, owned, err := s.workspace(ctx, o)
	if err != nil || !owned {
		t.Fatalf("%s %v", path, err)
	}
	if _, err := os.Stat(filepath.Join(path, "uncommitted")); !os.IsNotExist(err) {
		t.Fatal("copied working tree implicitly")
	}
	if b, err := os.ReadFile(filepath.Join(repo, "uncommitted")); err != nil || string(b) != "keep" {
		t.Fatal("modified source checkout")
	}
	if _, err := git(ctx, path, "symbolic-ref", "HEAD"); err == nil {
		t.Fatal("worktree should be detached")
	}
	_, _ = git(ctx, repo, "worktree", "remove", path)
	o.Checkout = true
	path, owned, err = s.workspace(ctx, o)
	if err != nil || owned || path != repo {
		t.Fatal(path, owned, err)
	}
}

func TestActivityFailureDoesNotChangeOperationOutcome(t *testing.T) {
	p, _ := fixtureRole("xps")
	p.CodexHome = filepath.Join(t.TempDir(), "missing")
	o := opts("list", "")
	o.JSON = true
	var out, errs strings.Builder
	code := run(context.Background(), p, o, &out, &errs)
	var r Result
	_ = json.Unmarshal([]byte(out.String()), &r)
	if code != 1 || r.ErrorCategory != "transport_unavailable" || r.ActivityStatus != "unavailable" || !strings.Contains(errs.String(), "Warning") {
		t.Fatalf("%d %+v %s", code, r, errs.String())
	}
}
