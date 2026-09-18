package tasks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestFindStockPaginationArchivesAndExactRead(t *testing.T) {
	ids := []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"}
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m == "thread/read" {
			return map[string]any{"thread": Task{ID: p["threadId"].(string), Name: "Exact"}}, nil
		}
		if m != "thread/list" {
			t.Fatal(m)
		}
		if p["useStateDbOnly"] != true {
			t.Fatal("unexpected scan behavior")
		}
		rows := []Task{{ID: ids[0], Name: "Review", Preview: "permissions"}, {ID: ids[1], Name: "unmatched"}}
		if p["archived"] == true {
			rows = []Task{{ID: ids[2], Name: "Review", Preview: "permissions"}}
		}
		index := 0
		if p["cursor"] != nil {
			index, _ = strconv.Atoi(p["cursor"].(string))
		}
		next := ""
		if index+1 < len(rows) {
			next = strconv.Itoa(index + 1)
		}
		return map[string]any{"data": rows[index : index+1], "nextCursor": next}, nil
	}}
	s := service{rpc: f}
	o := opts("find", "")
	o.Limit = 1
	o.Query = "review permissions"
	var all []Task
	for pages := 0; pages < 5; pages++ {
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
	if len(all) != 2 || all[0].ID != ids[0] || !all[1].Archived {
		t.Fatal(all)
	}
	o.Query = "codex://threads/" + ids[2]
	o.Cursor = ""
	r := Result{}
	f.methods = nil
	if err := s.find(context.Background(), o, &r); err != nil || len(r.Tasks) != 1 || r.Tasks[0].Name != "Exact" || fmt.Sprint(f.methods) != "[thread/read]" {
		t.Fatal(r, err, f.methods)
	}
	o.Archive = "active"
	r = Result{}
	if err := s.find(context.Background(), o, &r); err != nil || len(r.Tasks) != 0 {
		t.Fatal(r, err)
	}
	o.Cursor = encodeCursor(map[string]any{"last_id": ids[0]})
	if err := s.find(context.Background(), o, &Result{}); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatal("accepted legacy cursor", err)
	}
}
func TestFindSparseBoundAndUnsupportedServer(t *testing.T) {
	calls := 0
	s := service{rpc: &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		calls++
		return map[string]any{"data": []Task{{Name: "other"}}, "nextCursor": fmt.Sprint(calls)}, nil
	}}}
	o := opts("find", "")
	o.Query = "missing"
	r := Result{}
	if err := s.find(context.Background(), o, &r); err != nil || calls != 1000 || r.NextCursor == "" {
		t.Fatal(r, err, calls)
	}
	s.rpc = &fakeRPC{handle: func(string, map[string]any) (any, error) {
		return nil, &RPCError{Method: "thread/list", Code: -32601, Message: "unsupported"}
	}}
	if err := s.find(context.Background(), o, &Result{}); err == nil {
		t.Fatal("unsupported server treated as empty")
	}
}
