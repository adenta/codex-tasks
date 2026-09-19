package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Task struct {
	Path                 string  `json:"path,omitempty"`
	Host                 string  `json:"host,omitempty"`
	Account              string  `json:"account,omitempty"`
	Preview              string  `json:"preview,omitempty"`
	Archived             bool    `json:"archived,omitempty"`
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	CWD                  string  `json:"cwd"`
	ProjectID            string  `json:"projectId"`
	ModelProvider        string  `json:"modelProvider,omitempty"`
	Model                string  `json:"model"`
	ReasoningEffort      *string `json:"reasoningEffort"`
	CanAcceptDirectInput *bool   `json:"canAcceptDirectInput"`
	Status               struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
}
type Turn struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	CompletedAt *int64 `json:"completedAt,omitempty"`
}
type Item struct {
	OmittedParts       int    `json:"omitted_parts,omitempty"`
	ID                 string `json:"id"`
	TurnID             string `json:"turn_id"`
	Type               string `json:"type"`
	Text               string `json:"text,omitempty"`
	Truncated          bool   `json:"truncated,omitempty"`
	NextOffset         int    `json:"next_offset,omitempty"`
	ContinuationCursor string `json:"continuation_cursor,omitempty"`
}
type Result struct {
	AttachmentDirectory string        `json:"attachment_directory,omitempty"`
	Environments        []Environment `json:"environments,omitempty"`
	EnvironmentGit      *bool         `json:"environment_git,omitempty"`
	SetupStatus         string        `json:"setup_status,omitempty"`
	SetupOutput         string        `json:"setup_output,omitempty"`
	SetupExitCode       *int          `json:"setup_exit_code,omitempty"`
	SetupLogPath        string        `json:"setup_log_path,omitempty"`
	Workspace           string        `json:"workspace,omitempty"`
	Coverage            []Coverage    `json:"coverage,omitempty"`
	SearchComplete      *bool         `json:"search_complete,omitempty"`
	OmittedItems        int           `json:"omitted_items,omitempty"`
	ItemFound           *bool         `json:"item_found,omitempty"`
	Created             bool          `json:"created,omitempty"`
	OperationID         string        `json:"operation_id"`
	Host                string        `json:"host"`
	Account             string        `json:"account"`
	Action              string        `json:"action"`
	Outcome             string        `json:"outcome"`
	HistoryReady        bool          `json:"history_ready,omitempty"`
	InputAccepted       bool          `json:"input_accepted,omitempty"`
	ErrorCategory       string        `json:"error_category,omitempty"`
	Error               string        `json:"error,omitempty"`
	ActivityStatus      string        `json:"activity_status,omitempty"`
	Task                *Task         `json:"task,omitempty"`
	Tasks               []Task        `json:"tasks,omitempty"`
	Projects            []Project     `json:"projects,omitempty"`
	Items               []Item        `json:"items,omitempty"`
	NextCursor          string        `json:"next_cursor,omitempty"`
	TurnID              string        `json:"turn_id,omitempty"`
	TurnStatus          string        `json:"turn_status,omitempty"`
	Attention           string        `json:"attention,omitempty"`
	Worktree            string        `json:"worktree,omitempty"`
	ProjectID           string        `json:"project_id,omitempty"`
	Mode                string        `json:"mode,omitempty"`
}

type rpc interface {
	Call(context.Context, string, any, any) error
	CommandExec(context.Context, map[string]any, *commandResult, func([]byte)) error
}
type service struct {
	rpc       rpc
	client    *Client
	home      string
	localHome string
	uncertain bool
}

func (s *service) call(ctx context.Context, method string, params, result any, mutation bool) error {
	err := s.rpc.Call(ctx, method, params, result)
	var rejected *RPCError
	if mutation && err != nil && !errors.As(err, &rejected) {
		s.uncertain = true
	}
	return err
}
func (s *service) thread(ctx context.Context, id string) (Task, error) {
	var reply struct {
		Thread Task `json:"thread"`
	}
	err := s.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &reply, false)
	if err == nil && reply.Thread.ID != id {
		err = fmt.Errorf("app-server returned an unexpected task")
	}
	if err == nil && reply.Thread.Path != "" {
		rel, e := filepath.Rel(filepath.Join(s.home, "archived_sessions"), reply.Thread.Path)
		reply.Thread.Archived = e == nil && rel != ".." && !strings.HasPrefix(rel, "../")
	}
	return reply.Thread, err
}
func (s *service) resume(ctx context.Context, t Task) (Task, error) {
	var reply struct {
		Thread   Task    `json:"thread"`
		Model    string  `json:"model"`
		Provider string  `json:"modelProvider"`
		Effort   *string `json:"reasoningEffort"`
	}
	err := s.call(ctx, "thread/resume", map[string]any{"threadId": t.ID, "excludeTurns": true}, &reply, true)
	if err != nil {
		return t, err
	}
	if reply.Thread.ID != t.ID {
		return t, fmt.Errorf("app-server resumed an unexpected task")
	}
	t = reply.Thread
	if reply.Provider != "" {
		t.ModelProvider = reply.Provider
	}
	if t.Model == "" {
		t.Model = reply.Model
	}
	if t.ReasoningEffort == nil {
		t.ReasoningEffort = reply.Effort
	}
	return t, nil
}

func (s *service) latest(ctx context.Context, id string) (Turn, error) {
	var page struct {
		Data []Turn `json:"data"`
	}
	err := s.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 1, "sortDirection": "desc", "itemsView": "summary"}, &page, false)
	if err != nil || len(page.Data) == 0 {
		return Turn{}, err
	}
	return page.Data[0], nil
}

func attention(t Task, r *Result) bool {
	if len(t.Status.ActiveFlags) == 0 {
		return false
	}
	r.Outcome, r.Attention = "needs_attention", t.Status.ActiveFlags[0]
	return true
}

func (s *service) mode(ctx context.Context, t Task, mode string, r *Result) error {
	if t.Model == "" {
		return fmt.Errorf("current model is unavailable; mode change would not preserve settings")
	}
	var reply json.RawMessage
	err := s.call(ctx, "thread/settings/update", map[string]any{
		"threadId": t.ID,
		"collaborationMode": map[string]any{"mode": mode, "settings": map[string]any{
			"model": t.Model, "reasoning_effort": t.ReasoningEffort, "developer_instructions": nil,
		}},
	}, &reply, true)
	if err == nil {
		r.Mode = mode
		r.Outcome = "settings_updated"
	}
	return err
}

func (s *service) execute(ctx context.Context, o Options, r *Result) error {
	switch o.Action {
	case "find":
		return s.find(ctx, o, r)
	case "environments":
		return s.listEnvironments(ctx, o.CWD, r)
	case "projects":
		var page struct {
			Data       []Project `json:"data"`
			NextCursor string    `json:"nextCursor"`
		}
		err := s.call(ctx, "project/list", map[string]any{"limit": o.Limit, "cursor": nullable(o.Cursor)}, &page, false)
		for i := range page.Data {
			s.projectAvailability(ctx, &page.Data[i])
		}
		r.Projects, r.NextCursor = page.Data, page.NextCursor
		return err
	case "list":
		params := map[string]any{"limit": o.Limit, "cursor": nullable(o.Cursor), "archived": o.Archived, "sortKey": "updated_at", "useStateDbOnly": true, "sourceKinds": allTaskSources, "modelProviders": []string{}}
		if o.Project != "" {
			params["projectId"] = o.Project
		}
		var page struct {
			Data       []Task `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		err := s.call(ctx, "thread/list", params, &page, false)
		r.Tasks, r.NextCursor = page.Data, page.NextCursor
		return err
	case "create":
		return s.create(ctx, o, r)
	}
	t, err := s.thread(ctx, o.TaskID)
	if err != nil {
		return err
	}
	r.Task = &t
	switch o.Action {
	case "read":
		return s.items(ctx, o, r)
	case "progress":
		return s.progress(ctx, o, r)
	case "archive", "unarchive":
		if o.Action == "archive" && t.Status.Type == "active" {
			return fmt.Errorf("task is active; wait for it to finish before archiving")
		}
		var reply json.RawMessage
		err := s.call(ctx, "thread/"+o.Action, map[string]any{"threadId": t.ID}, &reply, true)
		if err == nil {
			r.Outcome = o.Action + "d"
		}
		return err
	case "fork":
		params := map[string]any{"threadId": t.ID, "excludeTurns": true, "deferGoalContinuation": true, "threadSource": "agent_created_thread"}
		// Fork defaults may come from host configuration rather than the parent.
		// Preserve the parent's explicit provider and model.
		if t.ModelProvider != "" && t.Model != "" {
			params["model"] = t.Model
			params["modelProvider"] = t.ModelProvider
		}
		if t.Status.Type == "active" {
			turn, err := s.latest(ctx, t.ID)
			if err != nil {
				return err
			}
			if turn.Status != "inProgress" || turn.ID == "" {
				return fmt.Errorf("active turn is not yet available; inspect progress before forking")
			}
			params["beforeTurnId"] = turn.ID
		}
		var reply struct {
			Thread   Task   `json:"thread"`
			Model    string `json:"model"`
			Provider string `json:"modelProvider"`
		}
		err := s.call(ctx, "thread/fork", params, &reply, true)
		if err != nil {
			return err
		}
		if reply.Thread.ID == "" {
			s.uncertain = true
			return fmt.Errorf("fork response is missing its task ID")
		}
		r.Task, r.Outcome, r.Created = &reply.Thread, "created", true
		if reply.Model != "" {
			r.Task.Model = reply.Model
		}
		if params["model"] != nil && (reply.Model != t.Model || reply.Provider != t.ModelProvider) {
			return fmt.Errorf("fork did not retain requested model and provider; no message sent")
		}
		if o.Title != "" {
			if err := s.name(ctx, r.Task.ID, o.Title); err != nil {
				return err
			}
			r.Task.Name = o.Title
		}
		if o.Mode != "" {
			return s.mode(ctx, *r.Task, o.Mode, r)
		}
		return nil
	case "message", "mode":
		if attention(t, r) {
			return nil
		}
		if t.Status.Type == "notLoaded" || t.CanAcceptDirectInput == nil {
			t, err = s.resume(ctx, t)
			r.Task = &t
			if err != nil {
				return err
			}
		}
		if attention(t, r) {
			return nil
		}
		if o.Action == "mode" {
			return s.mode(ctx, t, o.Mode, r)
		}
		if t.CanAcceptDirectInput != nil && !*t.CanAcceptDirectInput {
			return fmt.Errorf("target does not accept direct input")
		}
		return s.message(ctx, o, r)
	}
	return fmt.Errorf("unsupported task operation")
}

func (s *service) message(ctx context.Context, o Options, r *Result) error {
	t := *r.Task
	input := []any{}
	if o.Message != "" {
		input = append(input, map[string]any{"type": "text", "text": o.Message})
	}
	for _, path := range o.Images {
		input = append(input, map[string]any{"type": "localImage", "path": path})
	}
	params := map[string]any{"threadId": t.ID, "input": input, "clientUserMessageId": o.OperationID}
	if t.Status.Type == "active" {
		turn, err := s.latest(ctx, t.ID)
		if err != nil {
			return err
		}
		if turn.Status != "inProgress" || turn.ID == "" {
			return fmt.Errorf("active turn changed; inspect progress before sending again")
		}
		params["expectedTurnId"] = turn.ID
		var reply struct {
			TurnID string `json:"turnId"`
		}
		if err := s.call(ctx, "turn/steer", params, &reply, true); err != nil {
			return err
		}
		r.TurnID, r.TurnStatus, r.Outcome = reply.TurnID, "inProgress", "accepted"
	} else {
		var reply struct {
			Turn Turn `json:"turn"`
		}
		if err := s.call(ctx, "turn/start", params, &reply, true); err != nil {
			return err
		}
		r.TurnID, r.TurnStatus, r.Outcome = reply.Turn.ID, reply.Turn.Status, "accepted"
		applyTurn(reply.Turn, r)
	}
	if r.TurnID == "" {
		s.uncertain = true
		return fmt.Errorf("message response lacks a turn ID; inspect before retrying")
	}
	r.InputAccepted = true
	if s.client != nil {
		seen := s.client.observe(t.ID)
		if seen.Attention != "" {
			r.Outcome, r.Attention = "needs_attention", seen.Attention
		}
	}
	if o.Wait > 0 && r.Outcome != "needs_attention" {
		o.TurnID = r.TurnID
		return s.progress(ctx, o, r)
	}
	return nil
}

func applyTurn(t Turn, r *Result) {
	r.TurnID, r.TurnStatus = t.ID, t.Status
	switch t.Status {
	case "inProgress":
		r.Outcome = "started"
	case "completed":
		r.Outcome = "completed"
	case "failed":
		r.Outcome, r.ErrorCategory = "failed", "turn_failed"
	case "interrupted":
		r.Outcome = "interrupted"
	}
}

func (s *service) progress(ctx context.Context, o Options, r *Result) error {
	deadline := time.Now().Add(o.Wait)
	for {
		t, err := s.thread(ctx, r.Task.ID)
		if err != nil {
			return err
		}
		r.Task = &t
		if attention(t, r) {
			return nil
		}
		turn, err := s.latest(ctx, t.ID)
		if err != nil {
			return err
		}
		latestID := turn.ID
		if o.TurnID != "" && turn.ID != o.TurnID {
			// Inspect the requested turn directly, without treating a later turn
			// as evidence about the operation the caller is following.
			turn, err = s.findTurn(ctx, t.ID, o.TurnID)
			if err != nil {
				return err
			}
		}
		if turn.ID == "" {
			r.Outcome = t.Status.Type
			return nil
		}
		terminalObserved := false
		if s.client != nil {
			seen := s.client.observe(t.ID)
			if seen.TurnID == turn.ID && seen.TurnStatus != "" {
				turn.Status = seen.TurnStatus
				terminalObserved = seen.Terminal
			}
		}
		// During rollout publication stock Codex can briefly synthesize an
		// interrupted history entry for a still-active turn. Runtime activity
		// and subscribed turn events take precedence over that disk snapshot.
		knownRunning := r.TurnID == turn.ID && r.TurnStatus == "inProgress"
		if !terminalObserved && turn.CompletedAt == nil && turn.Status == "interrupted" &&
			(knownRunning || (t.Status.Type == "active" && turn.ID == latestID)) {
			turn.Status = "inProgress"
		}
		applyTurn(turn, r)
		if turn.Status != "inProgress" || !time.Now().Before(deadline) {
			return nil
		}
		if s.client != nil {
			seen := s.client.observe(t.ID)
			if seen.Attention != "" {
				r.Outcome, r.Attention = "needs_attention", seen.Attention
				return nil
			}
		}
		delay := time.Until(deadline)
		if delay > time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *service) findTurn(ctx context.Context, taskID, turnID string) (Turn, error) {
	cursor := ""
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		var page struct {
			Data       []Turn `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := s.call(ctx, "thread/turns/list", map[string]any{"threadId": taskID, "limit": 100, "cursor": nullable(cursor), "itemsView": "summary", "sortDirection": "desc"}, &page, false); err != nil {
			return Turn{}, err
		}
		for _, t := range page.Data {
			if t.ID == turnID {
				return t, nil
			}
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return Turn{}, fmt.Errorf("requested turn was not found in the most recent 1000 turns")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *service) name(ctx context.Context, id, title string) error {
	var reply json.RawMessage
	return s.call(ctx, "thread/name/set", map[string]any{"threadId": id, "name": title}, &reply, true)
}
