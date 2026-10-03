package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

const batchID1 = "00000000-0000-4000-8000-000000000001"
const batchID2 = "00000000-0000-4000-8000-000000000002"
const batchID3 = "00000000-0000-4000-8000-000000000003"

func TestArchiveSearchParsing(t *testing.T) {
	o, err := parse([]string{"find", "--archived=false", "--updated-before", "2026-10-01T08:00:00-04:00"}, strings.NewReader(""))
	if err != nil || o.Archive != "active" || o.UpdatedBefore != "2026-10-01T12:00:00Z" {
		t.Fatal(o, err)
	}
	for _, args := range [][]string{
		{"find", "--updated-before", "24h"},
		{"find", "--updated-before", "2026-10-01"},
		{"find", "--query", "hi", "--archived=false", "--archive", "all"},
		{"archive", batchID1, "--tasks-file", "-"},
		{"archive", "--tasks-file", "-", "--target", "local"},
		{"archive", batchID1, "invalid"},
		{"read", batchID1, batchID2},
	} {
		if _, err := parse(args, strings.NewReader("[]")); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"archive", batchID1, batchID2, "--json"}, {"unarchive", "--json", batchID1, batchID2}, {"archive", batchID1, "codex://threads/" + batchID1}} {
		o, err := parse(args, strings.NewReader(""))
		if err != nil || !o.Batch {
			t.Fatal(o, err)
		}
	}
	o, err = parse([]string{"archive", batchID1, "codex://threads/" + batchID1}, strings.NewReader(""))
	if err != nil || len(o.BatchTasks) != 1 {
		t.Fatal(o, err)
	}
	o, err = parse([]string{"archive", batchID1}, strings.NewReader(""))
	if err != nil || o.Batch || o.TaskID != batchID1 {
		t.Fatal(o, err)
	}
}

func TestBatchJSONValidation(t *testing.T) {
	for _, input := range []string{"[]", `{"tasks":[]}`, `{"action":"find","outcome":"not_found"}`, fmt.Sprintf(`[{"id":%q,"host":"server","account":"agent","updatedAt":"ignored"}]`, batchID1)} {
		if _, err := parse([]string{"archive", "--tasks-file", "-"}, strings.NewReader(input)); err != nil {
			t.Fatal(input, err)
		}
	}
	for _, input := range []string{"", "{}", "null", "[null]", "[] []", `{"tasks":{}}`, fmt.Sprintf(`[{"id":%q}]`, batchID1), strings.Repeat(" ", maxMessage+1)} {
		if _, err := parse([]string{"archive", "--tasks-file", "-"}, strings.NewReader(input)); err == nil {
			t.Fatalf("accepted invalid batch %.100s", input)
		}
	}
	tasks := make([]Task, 1001)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i)}
	}
	if _, err := normalizeBatch(tasks, false); err == nil {
		t.Fatal("accepted oversized batch")
	}
}

func TestUpdatedBeforeFilteringAndCursorBinding(t *testing.T) {
	cutoff := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Unix()
	old, equal, newer := cutoff-1, cutoff, cutoff+1
	for _, tc := range []struct {
		stamp *int64
		want  bool
	}{{&old, true}, {&equal, false}, {&newer, false}, {nil, false}} {
		if got := matchesUpdatedBefore(Task{UpdatedAt: tc.stamp}, "2026-10-01T12:00:00Z"); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	rows := []Task{{ID: batchID1, UpdatedAt: &old}, {ID: batchID2, UpdatedAt: &equal}, {ID: batchID3}}
	s := service{rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m == "thread/read" {
			return map[string]any{"thread": rows[1]}, nil
		}
		if p["archived"] != false {
			t.Fatal("wrong archive scope", p)
		}
		return map[string]any{"data": rows, "nextCursor": "more"}, nil
	}}}
	o := opts("find", "")
	o.Archive = "active"
	o.UpdatedBefore = "2026-10-01T12:00:00Z"
	o.Limit = 1
	r := Result{}
	if err := s.find(context.Background(), o, &r); err != nil || len(r.Tasks) != 1 || r.Tasks[0].ID != batchID1 || r.NextCursor == "" {
		t.Fatal(r, err)
	}
	o.Cursor = r.NextCursor
	o.UpdatedBefore = "2026-10-02T12:00:00Z"
	if err := s.find(context.Background(), o, &Result{}); err == nil {
		t.Fatal("cursor accepted changed cutoff")
	}
	o.Cursor = ""
	o.Query = batchID2
	o.Archive = "all"
	o.UpdatedBefore = "2026-10-01T12:00:00Z"
	r = Result{}
	if err := s.find(context.Background(), o, &r); err != nil || len(r.Tasks) != 0 {
		t.Fatal(r, err)
	}
}

func TestBatchContinuesRejectionsStopsUncertain(t *testing.T) {
	for _, action := range []string{"archive", "unarchive"} {
		calls := 0
		s := service{rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
			if m == "thread/read" {
				return map[string]any{"thread": Task{ID: p["threadId"].(string)}}, nil
			}
			calls++
			if p["threadId"] == batchID1 {
				return nil, &RPCError{Method: m, Code: -1, Message: "rejected"}
			}
			if p["threadId"] == batchID2 {
				return nil, errors.New("lost acknowledgement")
			}
			return map[string]any{}, nil
		}}}
		rows := s.executeBatch(context.Background(), opts(action, ""), []Task{{ID: batchID1}, {ID: batchID2}, {ID: batchID3}})
		if calls != 2 || rows[0].Outcome != "failed" || rows[1].Outcome != "unknown" || rows[2].Outcome != "unattempted" {
			t.Fatal(rows, calls)
		}
	}
}

func TestBatchActiveTaskAndCancellation(t *testing.T) {
	mutations := 0
	s := service{rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m == "thread/read" {
			task := Task{ID: p["threadId"].(string)}
			if task.ID == batchID1 {
				task.Status.Type = "active"
			}
			return map[string]any{"thread": task}, nil
		}
		mutations++
		return map[string]any{}, nil
	}}}
	rows := s.executeBatch(context.Background(), opts("archive", ""), []Task{{ID: batchID1}, {ID: batchID2}})
	if mutations != 1 || rows[0].Outcome != "failed" || rows[1].Outcome != "archived" {
		t.Fatal(rows, mutations)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows = s.executeBatch(ctx, opts("archive", ""), []Task{{ID: batchID1}, {ID: batchID2}})
	if mutations != 1 || rows[0].Outcome != "unattempted" || rows[1].Outcome != "unattempted" {
		t.Fatal(rows, mutations)
	}
}

func TestBatchRoutingPreflightAndResults(t *testing.T) {
	p, _ := fixtureAccount("grace", "agent", t.TempDir())
	p.Targets = []endpoints.Target{{Host: "server", Account: "agent", Alias: "server"}}
	o := opts("archive", "")
	o.Batch = true
	o.JSON = true
	o.BatchTasks = []Task{{ID: batchID1, Host: "grace", Account: "agent"}, {ID: batchID2, Host: "missing", Account: "agent"}}
	var out strings.Builder
	calls := 0
	execute := func(_ context.Context, _ endpoints.Config, o Options, tasks []Task) []Result {
		calls++
		results := []Result{}
		for _, task := range tasks {
			outcome := "archived"
			if task.Host == "server" {
				outcome = "unknown"
			}
			results = append(results, Result{Host: task.Host, Account: task.Account, Task: &Task{ID: task.ID}, Outcome: outcome})
		}
		return results
	}
	if code := runBatch(context.Background(), p, o, &out, execute); code != 2 || calls != 0 {
		t.Fatal(code, calls, out.String())
	}
	o.BatchTasks[1].Host = "server"
	out.Reset()
	if code := runBatch(context.Background(), p, o, &out, execute); code != 3 || calls != 2 {
		t.Fatal(code, calls, out.String())
	}
	var result Result
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil || result.Summary.Succeeded != 1 || result.Summary.Unknown != 1 {
		t.Fatal(result, err)
	}
	o.BatchTasks = nil
	out.Reset()
	if code := runBatch(context.Background(), p, o, &out, execute); code != 0 || calls != 2 {
		t.Fatal(code, calls)
	}
}

func TestAgeDiscoveryAcrossSourcesBindsCutoff(t *testing.T) {
	p, _ := fixtureAccount("grace", "agent", t.TempDir())
	p.Targets = []endpoints.Target{{Host: "server", Account: "agent", Alias: "server"}}
	o := opts("find", "")
	o.Archive = "active"
	o.UpdatedBefore = "2026-10-01T12:00:00Z"
	execute := func(_ context.Context, _ endpoints.Config, selected Options, r *Result) error {
		if selected.UpdatedBefore != o.UpdatedBefore || selected.Archive != "active" {
			return fmt.Errorf("filters lost")
		}
		r.Tasks = []Task{{ID: batchID1}}
		r.NextCursor = "next"
		return nil
	}
	r := Result{}
	if err := discover(context.Background(), p, o, &r, execute); err != nil || len(r.Tasks) != 2 || r.NextCursor == "" || r.Tasks[0].Host == r.Tasks[1].Host {
		t.Fatal(r, err)
	}
	o.Cursor = r.NextCursor
	o.UpdatedBefore = "2026-10-02T12:00:00Z"
	if err := discover(context.Background(), p, o, &Result{}, execute); err == nil {
		t.Fatal("aggregate cursor accepted a changed cutoff")
	}
}
