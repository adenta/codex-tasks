package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

type BatchSummary struct {
	Succeeded   int `json:"succeeded"`
	Failed      int `json:"failed"`
	Unknown     int `json:"unknown"`
	Unattempted int `json:"unattempted"`
}

func readBatchTasks(reader io.Reader) ([]Task, error) {
	b, err := io.ReadAll(io.LimitReader(reader, maxMessage+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMessage {
		return nil, fmt.Errorf("task input exceeds 1 MiB")
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, fmt.Errorf("expected a JSON task array or discovery result")
	}
	var raw []json.RawMessage
	if b[0] == '[' {
		err = json.Unmarshal(b, &raw)
	} else {
		var result struct {
			Action string          `json:"action"`
			Tasks  json.RawMessage `json:"tasks"`
		}
		err = json.Unmarshal(b, &result)
		if err == nil {
			// Empty find results omit tasks under the existing JSON contract.
			if result.Action == "find" && len(result.Tasks) == 0 {
				return []Task{}, nil
			}
			if len(result.Tasks) == 0 || bytes.Equal(bytes.TrimSpace(result.Tasks), []byte("null")) {
				return nil, fmt.Errorf("discovery result must contain a tasks array; use [] for an empty batch")
			}
			err = json.Unmarshal(result.Tasks, &raw)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("invalid JSON task input: %w", err)
	}
	tasks := make([]Task, 0, len(raw))
	for _, entry := range raw {
		// Discovery metadata is deliberately ignored; input only authorizes identities.
		var identity struct {
			ID      string `json:"id"`
			Host    string `json:"host"`
			Account string `json:"account"`
		}
		if err := json.Unmarshal(entry, &identity); err != nil {
			return nil, fmt.Errorf("invalid task identity: %w", err)
		}
		tasks = append(tasks, Task{ID: identity.ID, Host: identity.Host, Account: identity.Account})
	}
	return tasks, nil
}

func normalizeBatch(tasks []Task, requireTarget bool) ([]Task, error) {
	normalized := make([]Task, 0, len(tasks))
	seen := map[string]bool{}
	for _, t := range tasks {
		id, err := taskID(t.ID)
		if err != nil {
			return nil, err
		}
		t.ID = id
		if requireTarget || t.Host != "" || t.Account != "" {
			t.Host, t.Account, err = splitTarget(t.Host + "/" + t.Account)
			if err != nil {
				return nil, fmt.Errorf("each JSON task requires a valid host and account: %w", err)
			}
		}
		key := t.Host + "/" + t.Account + "/" + t.ID
		if !seen[key] {
			seen[key] = true
			normalized = append(normalized, t)
		}
		if len(normalized) > 1000 {
			return nil, fmt.Errorf("batch exceeds 1000 distinct tasks")
		}
	}
	return normalized, nil
}

type batchExecutor func(context.Context, endpoints.Config, Options, []Task) []Result

func runBatch(ctx context.Context, p endpoints.Config, o Options, w io.Writer, execute batchExecutor) int {
	r := Result{Action: o.Action, OperationID: o.OperationID, Host: p.Host, Account: p.Account, Outcome: "ok", Summary: &BatchSummary{}}
	groups := map[string][]Task{}
	var order []string
	// Resolve every identity before opening any connection or sending a mutation.
	for _, t := range o.BatchTasks {
		route := o
		if t.Host != "" {
			route.Host = ""
			route.Target = t.Host + "/" + t.Account
		}
		host, account, _, err := resolveRoute(p, route)
		if err != nil {
			setError(&r, err)
			r.ErrorCategory = "route_unavailable"
			render(w, r, o.JSON)
			return 2
		}
		t.Host, t.Account = host, account
		key := host + "/" + account
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], t)
	}
	for _, target := range order {
		selected := o
		selected.Host = ""
		selected.Target = target
		r.Results = append(r.Results, execute(ctx, p, selected, groups[target])...)
	}
	for _, item := range r.Results {
		switch item.Outcome {
		case "archived", "unarchived":
			r.Summary.Succeeded++
		case "unknown":
			r.Summary.Unknown++
		case "unattempted":
			r.Summary.Unattempted++
		default:
			r.Summary.Failed++
		}
	}
	code := 0
	if r.Summary.Failed+r.Summary.Unattempted > 0 {
		r.Outcome = "partial"
		code = 1
		if r.Summary.Succeeded == 0 {
			r.Outcome = "failed"
		}
	}
	if r.Summary.Unknown > 0 {
		r.Outcome = "unknown"
		code = 3
	}
	render(w, r, o.JSON)
	return code
}

func executeBatchAt(ctx context.Context, p endpoints.Config, o Options, tasks []Task) []Result {
	connectionResult := Result{}
	// SSH lives for the entire batch; only connection setup has this timer.
	// Canceling a setup-only context on success would kill the reusable proxy.
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := time.AfterFunc(30*time.Second, cancel)
	s, err := connectAt(connectionCtx, p, o, &connectionResult)
	timer.Stop()
	if err != nil {
		results := make([]Result, 0, len(tasks))
		for _, t := range tasks {
			results = append(results, Result{Action: o.Action, OperationID: o.OperationID, Host: t.Host, Account: t.Account, Task: &Task{ID: t.ID}, Outcome: "unattempted", ErrorCategory: connectionResult.ErrorCategory, Error: clean(err.Error(), 1200)})
		}
		return results
	}
	defer s.client.Close()
	return s.executeBatch(ctx, o, tasks)
}

func (s *service) executeBatch(ctx context.Context, o Options, tasks []Task) []Result {
	results := make([]Result, 0, len(tasks))
	stopped := ""
	for _, t := range tasks {
		r := Result{Action: o.Action, OperationID: o.OperationID, Host: t.Host, Account: t.Account, Task: &Task{ID: t.ID}, Outcome: "ok"}
		if ctx.Err() != nil {
			stopped = ctx.Err().Error()
		}
		if stopped != "" {
			r.Outcome = "unattempted"
			r.Error = clean(stopped, 1200)
			results = append(results, r)
			continue
		}
		selected := o
		selected.TaskID = t.ID
		taskCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.uncertain = false
		err := s.execute(taskCtx, selected, &r)
		cancel()
		setError(&r, err)
		if s.uncertain {
			r.Outcome = "unknown"
			r.ErrorCategory = "transport_uncertain"
			stopped = "an earlier mutation on this target has an uncertain outcome; inspect before retrying"
		}
		// An RPC rejection is definitive. Transport errors stop this connection;
		// local validation refusals (such as an active task) permit the next item.
		var rejected *RPCError
		if err != nil && !errors.As(err, &rejected) && (s.client != nil && s.client.connectionFailed() || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
			stopped = "connection interrupted on this target; remaining tasks were not attempted"
		}
		results = append(results, r)
	}
	return results
}
