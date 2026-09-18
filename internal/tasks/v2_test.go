package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

func historyFixture(t *testing.T, entries []map[string]any) *service {
	t.Helper()
	return &service{rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m != "thread/items/list" || p["limit"] != 1 {
			t.Fatalf("unexpected call %s %#v", m, p)
		}
		index := 0
		if p["cursor"] != nil {
			index, _ = strconv.Atoi(p["cursor"].(string))
		}
		next := ""
		if index+1 < len(entries) {
			next = strconv.Itoa(index + 1)
		}
		data := []any{}
		if index < len(entries) {
			data = append(data, map[string]any{"turnId": "turn", "item": entries[index]})
		}
		return map[string]any{"data": data, "nextCursor": next}, nil
	}}}
}

func TestHistoryMessagesPlansFormattingAndContinuation(t *testing.T) {
	entries := []map[string]any{
		{"id": "tool", "type": "mcpToolCall", "content": map[string]any{"private": "dump"}},
		{"id": "user", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "Hi\r\n\tthere\x1b[31m!\x1b[0m"}}},
		{"id": "plan", "type": "plan", "text": "αβγδε"},
		{"id": "assistant", "type": "agentMessage", "text": "Finished"},
	}
	s := historyFixture(t, entries)
	o := opts("read", storedTask().ID)
	o.Limit = 2
	o.MaxChars = 20
	r := Result{}
	if err := s.items(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != 2 || r.Items[0].Text != "Hi\n\tthere!" || r.Items[1].Type != "plan" || r.OmittedItems != 1 || r.NextCursor == "" {
		t.Fatalf("%+v", r)
	}
	o.Cursor = r.NextCursor
	r = Result{}
	if err := s.items(context.Background(), o, &r); err != nil || len(r.Items) != 1 || r.Items[0].ID != "assistant" || r.NextCursor != "" {
		t.Fatalf("%+v %v", r, err)
	}
	o.Cursor = ""
	o.ItemID = "plan"
	o.MaxChars = 3
	r = Result{}
	if err := s.items(context.Background(), o, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != 1 || r.Items[0].Text != "αβγ" || !r.Items[0].Truncated || r.Items[0].NextOffset != 3 {
		t.Fatal(r)
	}
	o.Cursor = r.Items[0].ContinuationCursor
	o.Offset = 3
	r = Result{}
	if err := s.items(context.Background(), o, &r); err != nil || r.Items[0].Text != "δε" || r.Items[0].Truncated {
		t.Fatalf("%+v %v", r, err)
	}
	o.TaskID = storedTask().ID
	if err := s.items(context.Background(), o, &Result{}); err == nil {
		t.Fatal("accepted another task's cursor")
	}
}

func TestHistoryDiagnosticsBoundedAndSparsePages(t *testing.T) {
	entries := []map[string]any{}
	for i := 0; i < 201; i++ {
		entries = append(entries, map[string]any{"id": fmt.Sprint(i), "type": "reasoning", "content": "private"})
	}
	s := historyFixture(t, entries)
	o := opts("read", storedTask().ID)
	r := Result{}
	if err := s.items(context.Background(), o, &r); err != nil || r.OmittedItems != 200 || r.NextCursor == "" || len(r.Items) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	s = historyFixture(t, []map[string]any{{"id": "tool", "type": "commandExecution", "content": "private"}})
	o.IncludeOutputs = true
	o.MaxChars = 10
	r = Result{}
	if err := s.items(context.Background(), o, &r); err != nil || len(r.Items) != 1 || !r.Items[0].Truncated || len(r.Items[0].Text) != 10 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestFindCanonicalIndexArchivesPaginationAndMissingRoots(t *testing.T) {
	home := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Keep WAL uncheckpointed: discovery must include committed WAL rows.
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", `CREATE TABLE threads (id TEXT PRIMARY KEY, name TEXT, title TEXT, cwd TEXT, project_id TEXT, preview TEXT, first_user_message TEXT, archived INTEGER)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001"}
	for i, id := range ids {
		if _, err := db.Exec(`INSERT INTO threads VALUES (?, 'Review', 'old preview', '/missing/workspace', 'project', 'permissions', '', ?)`, id, i%2); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := fixtureAccount("grace", "agent", "/home/agent")
	p.CodexHome = home
	local := opts("find", "")
	local.Query = ids[0]
	result := Result{}
	if err := executeLocal(context.Background(), p, local, &result); err != nil || len(result.Tasks) != 1 {
		t.Fatalf("discovery required a running daemon: %+v %v", result, err)
	}
	s := service{home: home}
	o := opts("find", "")
	o.Query = "review permissions"
	o.Limit = 1
	var all []Task
	for pages := 0; pages < 4; pages++ {
		r := Result{}
		if err := s.find(context.Background(), o, &r); err != nil {
			t.Fatal(err)
		}
		all = append(all, r.Tasks...)
		o.Cursor = r.NextCursor
		if o.Cursor == "" {
			break
		}
	}
	if len(all) != 3 || all[0].ID != ids[0] || !all[1].Archived || all[0].Name != "Review" || all[0].CWD != "/missing/workspace" {
		t.Fatal(all)
	}
	o.Cursor = ""
	o.Query = "codex://threads/" + ids[1]
	r := Result{}
	if err := s.find(context.Background(), o, &r); err != nil || len(r.Tasks) != 1 || !r.Tasks[0].Archived {
		t.Fatal(r, err)
	}
	o.Archive = "active"
	r = Result{}
	if err := s.find(context.Background(), o, &r); err != nil || len(r.Tasks) != 0 {
		t.Fatal(r, err)
	}
	// Discovery must never create an absent database or follow one outside home.
	s.home = t.TempDir()
	if err := s.find(context.Background(), o, &Result{}); err == nil {
		t.Fatal("missing index treated as absence")
	}
	if _, err := os.Stat(filepath.Join(s.home, "state_5.sqlite")); !os.IsNotExist(err) {
		t.Fatal("created index")
	}
	if err := os.Symlink(filepath.Join(home, "state_5.sqlite"), filepath.Join(s.home, "state_5.sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := s.find(context.Background(), o, &Result{}); err == nil {
		t.Fatal("followed index outside home")
	}
}

func TestDiscoveryCoverageAndDuplicateLocations(t *testing.T) {
	p, _ := fixtureAccount("grace", "agent", "/home/agent")
	o := opts("find", "")
	o.Query = "Review"
	execute := func(_ context.Context, _ endpoints.Config, o Options, r *Result) error {
		r.Tasks = []Task{{ID: storedTask().ID, Name: "Review"}}
		return nil
	}
	r := Result{}
	if err := discover(context.Background(), p, o, &r, execute); err != nil {
		t.Fatal(err)
	}
	if r.Outcome != "ambiguous" || r.SearchComplete == nil || !*r.SearchComplete {
		t.Fatal(r)
	}
	for _, task := range r.Tasks {
		if task.Host == "" || (task.Account != "agent" && task.Account != "andre") || task.Name != "Review" {
			t.Fatal(task)
		}
	}
	r = Result{}
	partiallyOffline := func(ctx context.Context, p endpoints.Config, o Options, r *Result) error {
		if o.Target == "love/agent" || o.Target == "xps/andre" {
			return errors.New("host offline")
		}
		return execute(ctx, p, o, r)
	}
	if err := discover(context.Background(), p, o, &r, partiallyOffline); err != nil || r.Outcome != "found" || r.SearchComplete == nil || *r.SearchComplete {
		t.Fatal(r, err)
	}
	offline := 0
	for _, c := range r.Coverage {
		if c.Status == "unavailable" && c.Detail == "host offline" {
			offline++
		}
	}
	if offline != 2 {
		t.Fatal(r.Coverage)
	}
	o.Target = "grace/agent"
	r = Result{}
	empty := func(context.Context, endpoints.Config, Options, *Result) error { return nil }
	if err := discover(context.Background(), p, o, &r, empty); err != nil || r.Outcome != "not_found" || !*r.SearchComplete {
		t.Fatal(r, err)
	}
}

func TestDiscoveryContinuationBoundToQueryAndSource(t *testing.T) {
	p, _ := fixtureAccount("grace", "agent", "/home/agent")
	o := opts("find", "")
	o.Query = "review"
	o.Target = "grace/agent"
	execute := func(_ context.Context, _ endpoints.Config, o Options, r *Result) error {
		r.Tasks = []Task{storedTask()}
		if o.Cursor == "" {
			r.NextCursor = "page2"
		}
		return nil
	}
	r := Result{}
	if err := discover(context.Background(), p, o, &r, execute); err != nil {
		t.Fatal(err)
	}
	if r.NextCursor == "" || *r.SearchComplete {
		t.Fatal(r)
	}
	o.Cursor = r.NextCursor
	r = Result{}
	if err := discover(context.Background(), p, o, &r, execute); err != nil || r.Outcome != "ambiguous" || !*r.SearchComplete {
		t.Fatal(r, err)
	}
	o.Query = "different"
	if err := discover(context.Background(), p, o, &Result{}, execute); err == nil {
		t.Fatal("cursor ignored query")
	}
}

func TestV2SelectorsAndDestinationRejection(t *testing.T) {
	id := storedTask().ID
	for _, args := range [][]string{{"read", id, "--host", "grace", "--target", "grace/agent"}, {"read", id, "--target", "grace/invalid/account"}, {"read", id, "--offset", "1"}, {"find", "--query", ""}, {"find", "--query", "a", "--archive", "wrong"}} {
		if _, err := parse(args, strings.NewReader("")); err == nil {
			t.Fatal(args)
		}
	}
	p, _ := fixtureAccount("grace", "agent", "/home/agent")
	o := opts("read", id)
	o.Target = "xps/andre"
	if host, account, alias, err := resolveRoute(p, o); err != nil || host != "xps" || account != "andre" || alias != "xps" {
		t.Fatal(host, account, alias, err)
	}
	o.Target = "love/agent"
	r := Result{}
	if err := executeAt(context.Background(), p, o, &r); err == nil || r.ErrorCategory != "route_unavailable" {
		t.Fatal(r, err)
	}
	o.Target = ""
	o.Host = "grace"
	b, _ := json.Marshal(remoteRequest{Version: tasksProtocol, Account: "andre", Options: o})
	var out strings.Builder
	if code := runRemote(context.Background(), p, nil, strings.NewReader(string(b)), &out); code != 2 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
}

func TestDestinationHandshakeBlocksMutation(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "mutation")
	script := "#!/bin/sh\nfor arg do last_arg=$arg; done\nif [ \"$last_arg\" = _capabilities ]; then printf '%s\\n' '{\"tasks_protocol\":4,\"host\":\"love\",\"account\":\"wrong\"}'; exit 0; fi\nprintf x > '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	o := opts("message", storedTask().ID)
	o.Host = "love"
	r := Result{Account: "agent"}
	if err := dispatch(context.Background(), "love", o, &r); err == nil || r.ErrorCategory != "destination_mismatch" {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("sent mutation")
	}
}

func TestPartialCreationEnglishAndDiagnostics(t *testing.T) {
	task := storedTask()
	r := Result{Action: "create", Created: true, Task: &task, Host: "grace", Account: "agent"}
	setError(&r, errors.New("title rejected"))
	var out strings.Builder
	render(&out, r, false)
	if r.Outcome != "partial" || !strings.Contains(out.String(), "Task created; setup is incomplete") || !strings.Contains(out.String(), task.ID) {
		t.Fatal(r, out.String())
	}
	var b diagnosticBuffer
	b.Write([]byte(strings.Repeat("SECRET", 1000) + "\x1b[31m"))
	if len(b.data) != 2048 || strings.Contains(b.message(errors.New("exit status 255")), "SECRET") {
		t.Fatal("unsafe diagnostic")
	}
}

func TestProjectsKeepUnavailableAndInspectEveryRoot(t *testing.T) {
	root := t.TempDir()
	var projects []Project
	raw := fmt.Sprintf(`[{"id":"available","roots":[{"path":"/missing-v2-project"},{"path":%q}]},{"id":"missing","roots":[]}]`, root)
	if err := json.Unmarshal([]byte(raw), &projects); err != nil {
		t.Fatal(err)
	}
	s := service{rpc: &fakeRPC{handle: func(string, map[string]any) (any, error) { return map[string]any{"data": projects}, nil }}}
	r := Result{}
	if err := s.execute(context.Background(), opts("projects", ""), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Projects) != 2 || r.Projects[0].Unavailable || !r.Projects[1].Unavailable || r.Projects[1].Reason == "" {
		t.Fatal(r)
	}
}

func TestPackagedSkillCommandExamples(t *testing.T) {
	b, err := os.ReadFile("../../skills/codex-tasks/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	// Parse actual packaged examples, replacing documented placeholders. This
	// checks the CLI/skill contract without making model calls or live mutations.
	text := strings.ReplaceAll(string(b), "\\\n", "")
	id := storedTask().ID
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "codex-tasks ") || strings.Contains(line, "--help") {
			continue
		}
		line = strings.ReplaceAll(line, "TASK_ID", id)
		// Shell example lexer: single quotes group a literal, backslashes above join lines.
		var args []string
		var word strings.Builder
		quoted := false
		for _, c := range strings.TrimPrefix(line, "codex-tasks ") {
			if c == '\'' {
				quoted = !quoted
				continue
			}
			if (c == ' ' || c == '\t') && !quoted {
				if word.Len() > 0 {
					args = append(args, word.String())
					word.Reset()
				}
			} else {
				word.WriteRune(c)
			}
		}
		if word.Len() > 0 {
			args = append(args, word.String())
		}
		for i := range args {
			if i > 0 && args[i-1] == "--message-file" {
				args[i] = "-"
			}
		}
		if quoted {
			t.Fatalf("unclosed quote: %s", line)
		}
		if _, err := parse(args, strings.NewReader("authorized fixture message")); err != nil {
			t.Fatalf("packaged example %s: %v", line, err)
		}
	}
}
