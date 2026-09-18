package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Exercises the installed stock protocol with two clients, persistent history,
// a daemon restart and a fake model. No live Codex home or credential is inherited.
func TestStockTaskLifecycle(t *testing.T) {
	t.Run("default-provider", func(t *testing.T) { testStockTaskLifecycle(t, "", false) })
	t.Run("explicit-command-provider", func(t *testing.T) { testStockTaskLifecycle(t, "command_fixture", false) })
	t.Run("openrouter-preset", func(t *testing.T) { testStockTaskLifecycle(t, "openrouter", false) })
	t.Run("openrouter-remote", func(t *testing.T) { testStockTaskLifecycle(t, "openrouter", true) })
}
func testStockTaskLifecycle(t *testing.T, provider string, remote bool) {
	custom := provider != ""
	expectedModel := "openai/gpt-5.6-sol"
	if provider == "openrouter" {
		expectedModel += "@preset/codex-tasks"
	}
	binary := os.Getenv("CODEX_TASKS_TEST_CODEX")
	if !filepath.IsAbs(binary) {
		t.Skip("set CODEX_TASKS_TEST_CODEX to an absolute stock Codex binary")
	}
	root, err := os.MkdirTemp("/tmp", "tasks-stock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Cleanup(func() {
		if t.Failed() {
			files, _ := filepath.Glob(filepath.Join(root, "server-*.log"))
			for _, f := range files {
				b, _ := os.ReadFile(f)
				if len(b) > 12000 {
					b = b[len(b)-12000:]
				}
				t.Logf("disposable daemon log: %s", b)
			}
		}
	})
	home := filepath.Join(root, ".codex")
	workspace := filepath.Join(root, "project")
	for _, p := range []string{home, workspace} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var requests []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		expectedKey := "Bearer fake-key"
		if custom && strings.Contains(r.URL.Path, "/command/") {
			expectedKey = "Bearer fake-command-key"
		}
		if r.Header.Get("Authorization") != expectedKey {
			t.Errorf("wrong authentication for selected provider")
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		mu.Lock()
		requests = append(requests, string(body))
		toolCall := provider == "openrouter" && len(requests) == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		item := map[string]any{"id": "msg-test", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "done"}}}
		if toolCall {
			item = map[string]any{"id": "tool-test", "type": "function_call", "call_id": "call-test", "name": "update_plan", "arguments": `{"plan":[{"step":"Mock continuation","status":"completed"}]}`}
		}
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": "resp-test"}},
			map[string]any{"type": "response.output_item.done", "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp-test", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
	}))
	defer upstream.Close()
	config := fmt.Sprintf(`model = "openai/gpt-5.6-sol"
model_provider = "fixture"
model_reasoning_effort = "high"
approval_policy = "on-request"
sandbox_mode = "read-only"
cli_auth_credentials_store = "file"
[model_providers.fixture]
name = "Task lifecycle fixture"
base_url = %q
env_key = "TASKS_TEST_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
`, upstream.URL+"/v1")
	initialConfig := config
	if custom {
		config += fmt.Sprintf(`
[model_providers.%s]
name = "Command fixture"
base_url = %q
wire_api = "responses"
supports_websockets = false
[model_providers.%s.auth]
command = "/bin/sh"
args = ["-c", "printf fake-command-key"]
`, provider, upstream.URL+"/command/v1", provider)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(initialConfig), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	socket := filepath.Join(home, "app-server-control/app-server-control.sock")
	start := func() func() {
		cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "unix://")
		cmd.Dir = root
		cmd.Env = []string{"HOME=" + root, "CODEX_HOME=" + home, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "TASKS_TEST_KEY=fake-key", "CODEX_SSH_SKIP_APP_SERVER_BOOT=true", "HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "ALL_PROXY=http://127.0.0.1:9", "NO_PROXY=127.0.0.1,localhost"}
		log, err := os.Create(filepath.Join(root, fmt.Sprintf("server-%d.log", time.Now().UnixNano())))
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = log.Close() }) }
		t.Cleanup(stop)
		for {
			if info, err := os.Stat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("stock daemon did not start")
			case <-time.After(20 * time.Millisecond):
			}
		}
		return stop
	}
	stop := start()
	// New providers must be picked up without restarting an existing server.
	if custom {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	s := service{rpc: client, client: client, home: home}
	o := opts("create", "")
	o.CWD = workspace
	o.Title = "Persisted lifecycle fixture"
	o.Mode = "plan"
	o.Model = "" // Use the effective configured model and reasoning.
	if custom {
		o.ModelProvider = provider
		o.Model = "openai/gpt-5.6-sol"
		o.ContextWindow = 32000
	}
	o.Images = []string{testPNG(t, root)}
	o.WaitHistory = true
	o.Wait = 10 * time.Second
	created := Result{Outcome: "ok"}
	create := func() error {
		if !remote {
			return s.execute(ctx, o, &created)
		}
		paths, _ := fixtureAccount("grace", "agent", root)
		paths.CodexHome = home
		request, _ := json.Marshal(remoteRequest{Version: tasksProtocol, Account: paths.Account, Options: o})
		var output strings.Builder
		if code := runRemote(ctx, paths, nil, strings.NewReader(string(request)), &output); code != 0 {
			return fmt.Errorf("remote creation exit %d: %s", code, output.String())
		}
		if err := json.Unmarshal([]byte(output.String()), &created); err != nil {
			return err
		}
		if created.Error != "" {
			return fmt.Errorf("remote creation: %s", created.Error)
		}
		return nil
	}
	if err := create(); err != nil {
		client.Close()
		t.Fatalf("create: %v %+v", err, created)
	}
	if created.Task == nil || created.Task.ProjectID == "" || created.Task.Name != o.Title {
		client.Close()
		t.Fatalf("project/title missing: %+v", created)
	}
	if !created.HistoryReady {
		t.Fatal("image-only user history was not readable")
	}
	// Completed ingestion must persist the image across cold resume even when
	// the original local file no longer exists.
	if err := os.Remove(o.Images[0]); err != nil {
		t.Fatal(err)
	}
	if custom && created.Task.ModelProvider != provider {
		t.Fatalf("provider missing: %+v", created.Task)
	}
	if created.Task.Model != expectedModel {
		t.Fatalf("wrong created model: %+v", created.Task)
	}
	id := created.Task.ID
	message := opts("message", id)
	message.Message = "Reply exactly done. Do not call tools."
	message.Wait = 10 * time.Second
	first := created
	if first.Outcome != "completed" {
		var detail any
		_ = s.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 3, "itemsView": "full"}, &detail, false)
		t.Logf("turn detail: %#v", detail)
		mu.Lock()
		t.Logf("fake requests: %d", len(requests))
		mu.Unlock()
		client.Close()
		t.Fatalf("turn did not complete: %+v", first)
	}
	client.Close()
	// Closing a client must leave the shared daemon and other clients usable.
	other, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	reader := service{rpc: other, client: other, home: home}
	read := Result{Outcome: "ok"}
	if err := reader.execute(ctx, opts("read", id), &read); err != nil {
		other.Close()
		t.Fatalf("read from another client: %v", err)
	}
	if len(read.Items) < 2 {
		other.Close()
		t.Fatalf("history is missing: %+v", read)
	}
	other.Close()
	stop()
	_ = os.Remove(socket)
	stop = start()
	defer stop()
	client, err = Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	s = service{rpc: client, client: client, home: home}
	before, err := s.thread(ctx, id)
	if err != nil || before.Status.Type != "notLoaded" {
		t.Fatalf("not persisted/unloaded: %+v %v", before, err)
	}
	second := Result{Outcome: "ok"}
	message.OperationID = uuidForTest()
	if err := s.execute(ctx, message, &second); err != nil {
		t.Fatalf("cold resume: %v %+v", err, second)
	}
	if second.Outcome != "completed" || second.Task.Model != expectedModel || second.Task.ReasoningEffort == nil || *second.Task.ReasoningEffort != "high" {
		t.Fatalf("cold settings/outcome: %+v", second)
	}
	if custom && second.Task.ModelProvider != provider {
		t.Fatalf("cold resume lost provider: %+v", second.Task)
	}
	mu.Lock()
	captured := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := 2
	if provider == "openrouter" {
		wantRequests++
		if len(captured) < 2 || !strings.Contains(captured[1], "function_call_output") {
			t.Fatal("missing mock tool continuation")
		}
	}
	if len(captured) != wantRequests {
		t.Fatalf("wanted exactly %d fake model requests, got %d", wantRequests, len(captured))
	}
	for i, payload := range captured {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(payload), &request); err != nil || request.Model != expectedModel {
			t.Fatalf("request %d lost preset/model: %s", i, payload)
		}
		if !strings.Contains(payload, "Plan Mode") {
			t.Fatalf("request %d lost Plan mode", i)
		}
		if !strings.Contains(payload, "input_image") || !strings.Contains(payload, "data:image/png;base64,") {
			t.Fatalf("request %d lost the file image across history/resume", i)
		}
	}
	fork := Result{Outcome: "ok"}
	if err := s.execute(ctx, opts("fork", id), &fork); err != nil {
		t.Fatalf("fork: %v", err)
	}
	if fork.Task.ID == id || fork.Task.ProjectID != created.Task.ProjectID || fork.Task.Model != expectedModel {
		t.Fatalf("fork lost identity/project: %+v", fork)
	}
	if provider == "openrouter" {
		mode := opts("mode", fork.Task.ID)
		mode.Mode = "default"
		var changed Result
		if err := s.execute(ctx, mode, &changed); err != nil {
			t.Fatalf("fork mode change: %v", err)
		}
		followup := opts("message", fork.Task.ID)
		followup.Message, followup.Wait = "Continue the fork.", 10*time.Second
		var continued Result
		if err := s.execute(ctx, followup, &continued); err != nil || continued.Outcome != "completed" {
			t.Fatalf("fork continuation: %v %+v", err, continued)
		}
		mu.Lock()
		payload := requests[len(requests)-1]
		mu.Unlock()
		var request struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(payload), &request); err != nil || request.Model != expectedModel {
			t.Fatalf("fork/mode change lost model: %s", payload)
		}
	}
	for _, action := range []string{"archive", "unarchive"} {
		r := Result{Outcome: "ok"}
		if err := s.execute(ctx, opts(action, fork.Task.ID), &r); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		search := opts("find", "")
		search.Query = "codex://threads/" + fork.Task.ID
		found := Result{}
		if err := s.execute(ctx, search, &found); err != nil || len(found.Tasks) != 1 || found.Tasks[0].ID != fork.Task.ID || found.Tasks[0].Archived != (action == "archive") {
			data, _ := exec.Command("sqlite3", filepath.Join(home, "state_5.sqlite"), "select id,title,source,model_provider,archived,has_user_event from threads;").CombinedOutput()
			t.Logf("disposable state: %s; wanted %s", data, fork.Task.ID)
			var listed any
			_ = s.call(ctx, "thread/list", map[string]any{"archived": action == "archive", "sourceKinds": allTaskSources, "modelProviders": []string{}, "useStateDbOnly": true}, &listed, false)
			t.Logf("raw listing: %#v", listed)
			t.Fatalf("find after %s: %+v %v", action, found, err)
		}

	}
	paths, _ := fixtureAccount("grace", "agent", root)
	paths.CodexHome = home
	var output, stderr strings.Builder
	progress := opts("progress", id)
	progress.JSON = true
	if code := run(ctx, paths, progress, &output, &stderr); code != 0 {
		t.Fatalf("CLI exit %d: %s %s", code, output.String(), stderr.String())
	}
	var result Result
	if json.Unmarshal([]byte(output.String()), &result) != nil || result.Outcome != "completed" || result.ActivityStatus != "ok" {
		t.Fatalf("CLI result: %s", output.String())
	}
	activity, err := (activityLog{home: home}).read(ActivityFilter{Since: time.Now().Add(-time.Hour)})
	if err != nil || activity.Operations != 1 || len(activity.Events) != 2 {
		t.Fatalf("CLI activity: %+v %v", activity, err)
	}
	output.Reset()
	stderr.Reset()
	detached := opts("message", id)
	detached.Message = "Finish after the sending client disconnects."
	detached.JSON = true
	if code := run(ctx, paths, detached, &output, &stderr); code != 0 {
		t.Fatalf("detached send: %d %s", code, output.String())
	}
	if json.Unmarshal([]byte(output.String()), &result) != nil || result.TurnID == "" {
		t.Fatal(output.String())
	}
	progress.TurnID = result.TurnID
	progress.Wait = 10 * time.Second
	output.Reset()
	if code := run(ctx, paths, progress, &output, &stderr); code != 0 {
		t.Fatalf("detached progress: %d %s", code, output.String())
	}
	if json.Unmarshal([]byte(output.String()), &result) != nil || result.Outcome != "completed" {
		t.Fatalf("closing sender stopped the task: %s", output.String())
	}
	blank := opts("create", "")
	t.Setenv("HOME", root)
	blank.CWD = ""
	blank.WaitHistory = true
	blank.Projectless = true
	blank.Title = "First-message projectless fixture"
	blank.Model = "openai/gpt-5.6-sol"
	blank.Message = "Reply done without tools."
	blank.Wait = 10 * time.Second
	empty := Result{Outcome: "ok"}
	if err := s.execute(ctx, blank, &empty); err != nil {
		t.Fatal(err)
	}
	var ignored any
	if err := client.Call(ctx, "thread/unsubscribe", map[string]any{"threadId": empty.Task.ID}, &ignored); err != nil {
		t.Fatal(err)
	}
	if _, err := s.thread(ctx, empty.Task.ID); err != nil {
		t.Fatalf("first-message task was not persisted: %v", err)
	}
	client.Close()
	stop()
	_ = os.Remove(socket)
	stop = start()
	defer stop()
	client, err = Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	s = service{rpc: client, client: client, home: home}
	blankTask, err := s.thread(ctx, empty.Task.ID)
	if err != nil || blankTask.ProjectID != "" {
		t.Fatalf("first-message projectless task after restart: %+v %v", blankTask, err)
	}
	turns, err := s.latest(ctx, empty.Task.ID)
	if err != nil || turns.ID == "" {
		t.Fatalf("first-message task has no turn: %+v %v", turns, err)
	}
	listed := Result{}
	if err := s.execute(ctx, opts("list", ""), &listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range listed.Tasks {
		if task.ID == empty.Task.ID {
			found = true
		}
	}
	if !found {
		data, _ := exec.Command("sqlite3", filepath.Join(home, "state_5.sqlite"), "select id,title,source,model_provider,has_user_event from threads;").CombinedOutput()
		t.Fatalf("first-message task absent from persistent index: %+v; database: %s", listed.Tasks, data)
	}

}

func uuidForTest() string { return opts("list", "").OperationID }
