package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

// Explicit kinds avoid the upstream interactive-only default; empty providers
// includes historical tasks without changing their provider identity.
var allTaskSources = []string{"cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown"}

type Coverage struct {
	Target string `json:"target"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type findPosition struct {
	Version  int    `json:"version"`
	Cursor   string `json:"cursor"`
	Archived bool   `json:"archived"`
	Query    string `json:"query"`
	Archive  string `json:"archive"`
}
type findCursor struct {
	Version int               `json:"version"`
	Query   string            `json:"query"`
	Archive string            `json:"archive"`
	Scope   string            `json:"scope"`
	Sources map[string]string `json:"sources"`
	Matches int               `json:"matches"`
}

func encodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(s string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, v) != nil {
		return fmt.Errorf("invalid cursor; repeat the original query without --cursor")
	}
	return nil
}

// Search uses only stock task methods. Coverage is limited to the server's view.
func (s *service) find(ctx context.Context, o Options, r *Result) error {
	pos := findPosition{Version: 2, Archived: o.Archive == "archived", Query: o.Query, Archive: o.Archive}
	if o.Cursor != "" {
		pos = findPosition{}
		if err := decodeCursor(o.Cursor, &pos); err != nil {
			return err
		}
		if pos.Version != 2 || pos.Query != o.Query || pos.Archive != o.Archive {
			return fmt.Errorf("search cursor is obsolete or belongs to another query; restart the search without --cursor")
		}
	}
	id, idErr := taskID(o.Query)
	if idErr == nil && o.Cursor == "" {
		task, err := s.thread(ctx, id)
		if err != nil {
			return err
		}
		if o.Archive == "" || o.Archive == "all" || task.Path != "" {
			if o.Archive == "" || o.Archive == "all" || (o.Archive == "archived") == task.Archived {
				r.Tasks = append(r.Tasks, task)
			}
			return nil
		}
		// thread/read does not report archive membership. Resolve explicit archive
		// filters through the corresponding thread/list scope.
	}
	words := strings.Fields(strings.ToLower(o.Query))
	for scanned, pages := 0, 0; scanned < 1000 && pages < 1000; pages++ {
		remaining := o.Limit - len(r.Tasks)
		if remaining <= 0 {
			r.NextCursor = encodeCursor(pos)
			return nil
		}
		if remaining > 100 {
			remaining = 100
		}
		if remaining > 1000-scanned {
			remaining = 1000 - scanned
		}
		var page struct {
			Data       []Task `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		err := s.call(ctx, "thread/list", map[string]any{"limit": remaining, "cursor": nullable(pos.Cursor), "archived": pos.Archived, "sortKey": "created_at", "sourceKinds": allTaskSources, "modelProviders": []string{}, "useStateDbOnly": true}, &page, false)
		if err != nil {
			return err
		}
		for _, task := range page.Data {
			scanned++
			match := true
			if idErr == nil {
				match = task.ID == id
			} else {
				value := strings.ToLower(task.Name + "\n" + task.Preview)
				for _, word := range words {
					if !strings.Contains(value, word) {
						match = false
						break
					}
				}
			}
			if match {
				task.Archived = pos.Archived
				r.Tasks = append(r.Tasks, task)
			}
		}
		if page.NextCursor != "" && page.NextCursor == pos.Cursor {
			return fmt.Errorf("task pagination did not advance")
		}
		pos.Cursor = page.NextCursor
		if page.NextCursor == "" {
			if !pos.Archived && (o.Archive == "" || o.Archive == "all") {
				pos.Archived = true
			} else {
				return nil
			}
		}
	}
	r.NextCursor = encodeCursor(pos)
	return nil
}

type executor func(context.Context, endpoints.Config, Options, *Result) error

func discover(ctx context.Context, p endpoints.Config, o Options, r *Result, execute executor) error {
	o = localTarget(p, o)
	encoded, _ := json.Marshal(p)
	scope := fmt.Sprintf("%s/%s|%s|%s|%x", p.Host, p.Account, o.Host, o.Target, sha256.Sum256(encoded))
	state := findCursor{Version: 2, Query: o.Query, Archive: o.Archive, Scope: scope, Sources: map[string]string{}}
	if o.Cursor != "" {
		state = findCursor{}
		if err := decodeCursor(o.Cursor, &state); err != nil {
			return err
		}
		if state.Version != 2 || state.Query != o.Query || state.Archive != o.Archive || state.Scope != scope || state.Sources == nil || state.Matches < 0 {
			return fmt.Errorf("search cursor is obsolete or belongs to another query, filter, or account; restart the search without --cursor")
		}
	}
	var sources []Coverage
	for _, t := range p.Sources() {
		target := t.Host + "/" + t.Account
		if o.Host != "" && !strings.EqualFold(o.Host, t.Host) {
			continue
		}
		if o.Target != "" && o.Target != target {
			continue
		}
		sources = append(sources, Coverage{Target: target})
	}
	if len(sources) == 0 {
		return fmt.Errorf("selected account has no configured task endpoint")
	}
	results := make([]Result, len(sources))
	var wg sync.WaitGroup
	for i := range sources {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := &sources[i]
			if cursor, ok := state.Sources[c.Target]; ok && cursor == "done" {
				c.Status = "complete"
				return
			}
			query := o
			query.Host, query.Target, query.Cursor = "", c.Target, state.Sources[c.Target]
			child, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			result := &results[i]
			result.Outcome = "ok"
			err := execute(child, p, query, result)
			setError(result, err)
			if err != nil || result.Error != "" {
				c.Status, c.Detail = "unavailable", result.Error
				return
			}
			c.Status = "complete"
			if result.NextCursor != "" {
				c.Status = "more"
			}
		}(i)
	}
	wg.Wait()
	complete, more := true, false
	for i, c := range sources {
		for _, task := range results[i].Tasks {
			task.Host, task.Account, _ = splitTarget(c.Target)
			r.Tasks = append(r.Tasks, task)
		}
		if c.Status != "complete" {
			complete = false
		}
		if c.Status == "more" {
			more = true
			state.Sources[c.Target] = results[i].NextCursor
		}
		if c.Status == "complete" {
			state.Sources[c.Target] = "done"
		}
	}
	state.Matches += len(r.Tasks)
	r.Coverage, r.SearchComplete = sources, &complete
	switch {
	case state.Matches > 1:
		r.Outcome = "ambiguous"
	case state.Matches == 1:
		r.Outcome = "found"
	case complete:
		r.Outcome = "not_found"
	default:
		r.Outcome = "incomplete"
	}
	if more {
		r.NextCursor = encodeCursor(state)
	}
	return nil
}
