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
	"github.com/adenta/codex-tasks/internal/taskstate"
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
	LastID string `json:"last_id,omitempty"`
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

// The runtime list hides some indexed tasks (including new forks). Discovery
// reads the canonical index with the existing WAL-aware, contained, read-only
// resolver. Task bodies and all controls still go through the existing runtime.
// Stable ID keysets avoid offset skips when titles or update times change.
func (s *service) find(ctx context.Context, o Options, r *Result) error {
	pos := findPosition{}
	if o.Cursor != "" {
		if err := decodeCursor(o.Cursor, &pos); err != nil {
			return err
		}
		if _, err := taskID(pos.LastID); err != nil {
			return fmt.Errorf("invalid search position")
		}
	}
	db, err := taskstate.OpenReadOnly(ctx, s.home)
	if err != nil {
		return fmt.Errorf("canonical task index unavailable: %w", err)
	}
	defer db.Close()
	query := `SELECT id, COALESCE(NULLIF(name, ''), title, ''), cwd,
  COALESCE(project_id, ''), COALESCE(NULLIF(preview, ''), first_user_message, ''), archived
  FROM threads WHERE (? = '' OR id < ?)`
	args := []any{pos.LastID, pos.LastID}
	switch o.Archive {
	case "active":
		query += " AND archived = 0"
	case "archived":
		query += " AND archived = 1"
	}
	id, idErr := taskID(o.Query)
	if idErr == nil {
		query += " AND id = ?"
		args = append(args, id)
	}
	query += " ORDER BY id DESC LIMIT 1001"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("canonical task index schema is incompatible: %w", err)
	}
	defer rows.Close()
	words := strings.Fields(strings.ToLower(o.Query))
	scanned := 0
	for rows.Next() {
		// Fetch one lookahead row to distinguish completion from remaining work.
		if scanned >= 1000 || len(r.Tasks) >= o.Limit {
			r.NextCursor = encodeCursor(pos)
			return nil
		}
		var task Task
		if err := rows.Scan(&task.ID, &task.Name, &task.CWD, &task.ProjectID, &task.Preview, &task.Archived); err != nil {
			return err
		}
		pos.LastID = task.ID
		scanned++
		match := true
		if idErr != nil {
			text := strings.ToLower(task.Name + "\n" + task.Preview)
			for _, word := range words {
				if !strings.Contains(text, word) {
					match = false
					break
				}
			}
		}
		if match {
			r.Tasks = append(r.Tasks, task)
		}
	}
	return rows.Err()
}

type executor func(context.Context, endpoints.Config, Options, *Result) error

func discover(ctx context.Context, p endpoints.Config, o Options, r *Result, execute executor) error {
	o = localTarget(p, o)
	encoded, _ := json.Marshal(p)
	scope := fmt.Sprintf("%s/%s|%s|%s|%x", p.Host, p.Account, o.Host, o.Target, sha256.Sum256(encoded))
	state := findCursor{Version: 1, Query: o.Query, Archive: o.Archive, Scope: scope, Sources: map[string]string{}}
	if o.Cursor != "" {
		if err := decodeCursor(o.Cursor, &state); err != nil {
			return err
		}
		if state.Version != 1 || state.Query != o.Query || state.Archive != o.Archive || state.Scope != scope || state.Sources == nil || state.Matches < 0 {
			return fmt.Errorf("cursor belongs to another find query, filter, or account")
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
