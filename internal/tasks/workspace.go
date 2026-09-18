package tasks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

type Project struct {
	IsGit       bool   `json:"isGitRepository"`
	Unavailable bool   `json:"unavailable,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Roots       []struct {
		Path string `json:"path"`
	} `json:"roots"`
}

func (s *service) projectAvailability(ctx context.Context, project *Project) {
	project.Unavailable = true
	for _, root := range project.Roots {
		if !filepath.IsAbs(root.Path) {
			continue
		}
		if info, err := s.stat(ctx, root.Path, true); err == nil && info.IsDir() {
			_, gitErr := s.git(ctx, root.Path, "rev-parse", "--show-toplevel")
			project.IsGit = gitErr == nil
			project.Unavailable = false
			project.Reason = ""
			return
		}
	}
	project.Reason = "No saved project root is an accessible directory on this machine/account."
}

func (s *service) sameWorkspace(ctx context.Context, a, b string) bool {
	a, ea := s.realpath(ctx, a)
	b, eb := s.realpath(ctx, b)
	if ea != nil || eb != nil {
		return false
	}
	if a == b {
		return true
	}
	ga, ea := s.git(ctx, a, "rev-parse", "--path-format=absolute", "--git-common-dir")
	gb, eb := s.git(ctx, b, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return ea == nil && eb == nil && ga == gb
}

func (s *service) project(ctx context.Context, o Options) (string, error) {
	if o.Projectless {
		return "", nil
	}
	if o.Project != "" {
		var reply struct {
			Project Project `json:"project"`
		}
		if err := s.call(ctx, "project/read", map[string]any{"projectId": o.Project}, &reply, false); err != nil {
			return "", err
		}
		for _, root := range reply.Project.Roots {
			if s.sameWorkspace(ctx, root.Path, o.CWD) {
				return reply.Project.ID, nil
			}
		}
		return "", fmt.Errorf("workspace does not match the selected project's roots")
	}
	cursor, matched := "", ""
	for {
		var page struct {
			Data       []Project `json:"data"`
			NextCursor string    `json:"nextCursor"`
		}
		if err := s.call(ctx, "project/list", map[string]any{"limit": 100, "cursor": nullable(cursor)}, &page, false); err != nil {
			return "", err
		}
		for _, project := range page.Data {
			for _, root := range project.Roots {
				if !s.sameWorkspace(ctx, root.Path, o.CWD) {
					continue
				}
				if matched != "" && matched != project.ID {
					return "", fmt.Errorf("multiple projects match; select one with --project")
				}
				matched = project.ID
			}
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			return "", fmt.Errorf("project pagination did not advance")
		}
		cursor = page.NextCursor
	}
	if matched != "" {
		return matched, nil
	}
	var reply struct {
		Project Project `json:"project"`
	}
	if err := s.call(ctx, "project/create", map[string]any{"idempotencyKey": o.OperationID, "name": filepath.Base(o.CWD), "roots": []any{map[string]any{"path": o.CWD}}}, &reply, true); err != nil {
		return "", err
	}
	if reply.Project.ID == "" {
		s.uncertain = true
		return "", fmt.Errorf("project response is missing its ID")
	}
	return reply.Project.ID, nil
}

func (s *service) workspace(ctx context.Context, o Options) (string, bool, error) {
	info, err := s.stat(ctx, o.CWD, true)
	if err != nil || !info.IsDir() {
		return "", false, fmt.Errorf("workspace must be an existing directory")
	}
	sourceRoot, err := s.git(ctx, o.CWD, "rev-parse", "--show-toplevel")
	if err != nil {
		isGit, e := s.environmentRepository(ctx, o.CWD)
		if e != nil {
			return "", false, e
		}
		if !isGit && o.Ref == "" {
			return o.CWD, false, nil
		}
		return "", false, err
	}

	if o.Checkout {
		return o.CWD, false, nil
	}
	ref := o.Ref
	if ref == "" {
		ref, err = s.git(ctx, o.CWD, "symbolic-ref", "refs/remotes/origin/HEAD")
		if err != nil {
			return "", false, fmt.Errorf("default Git branch is unavailable; specify --ref or explicitly use --checkout")
		}
	}
	commit, err := s.git(ctx, o.CWD, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", false, err
	}
	root := filepath.Join(s.home, "worktrees", "codex-tasks-"+o.OperationID)
	if err := s.mkdir(ctx, root, true); err != nil {
		return "", false, err
	}
	path := filepath.Join(root, filepath.Base(o.CWD))
	if _, err := s.git(ctx, o.CWD, "worktree", "add", "--detach", "--", path, commit); err != nil {
		if s.uncertain {
			return path, true, err
		}
		_ = s.remove(ctx, root, false)
		return "", false, err
	}
	if err := s.copyLocalOverride(ctx, sourceRoot, path); err != nil {
		return path, true, fmt.Errorf("prepare override in retained worktree %s: %w", path, err)
	}
	return path, true, nil
}

func (s *service) create(ctx context.Context, o Options, r *Result) error {
	model, err := creationModel(o.ModelProvider, o.Model)
	if err != nil {
		return err
	}
	o.Model = model
	var environment *environmentConfig
	if o.Environment != "" {
		isGit, err := s.environmentRepository(ctx, o.CWD)
		if err != nil {
			return err
		}
		if !isGit || o.Checkout {
			return fmt.Errorf("environment setup requires a new Git worktree")
		}
		environment, err = s.readEnvironment(ctx, o.CWD, o.Environment)
		if err != nil {
			return err
		}
	}
	generated := ""
	if o.Projectless && o.CWD == "" {
		var err error
		generated, err = s.newProjectlessWorkspace(ctx)
		if err != nil {
			return err
		}
		o.CWD = generated
		r.Workspace = generated
	}
	// Establish project identity first, and preserve any created project ID in
	// the result even if a later operation fails.
	project, err := s.project(ctx, o)
	if err != nil {
		return err
	}
	r.ProjectID = project
	workspace, owned, err := s.workspace(ctx, o)
	if owned {
		r.Worktree = workspace
	}
	if err != nil {
		return err
	}
	if environment != nil {
		if err := s.runEnvironmentSetup(ctx, workspace, environment, r); err != nil {
			return err
		}
	}
	params := map[string]any{"cwd": workspace, "ephemeral": false, "historyMode": "paginated", "threadSource": "agent_created_thread"}
	if generated != "" {
		params["developerInstructions"] = projectlessInstructions(generated)
	}
	if project != "" {
		params["projectId"] = project
	}
	if o.Model != "" {
		params["model"] = o.Model
	}
	if o.ModelProvider != "" {
		params["modelProvider"] = o.ModelProvider
		params["serviceTier"] = "default"
	}
	config := map[string]any{}
	if o.ContextWindow > 0 {
		config["model_context_window"] = o.ContextWindow
	}
	if o.ReasoningEffort != "" {
		config["model_reasoning_effort"] = o.ReasoningEffort
	}
	if len(config) > 0 {
		params["config"] = config
	}
	var reply struct {
		Thread   Task    `json:"thread"`
		Model    string  `json:"model"`
		Provider string  `json:"modelProvider"`
		Effort   *string `json:"reasoningEffort"`
	}
	err = s.call(ctx, "thread/start", params, &reply, true)
	if err != nil {
		var rejected *RPCError
		if owned && environment == nil && errors.As(err, &rejected) {
			// Remove only our own clean, unattached worktree after a definite
			// rejection. On uncertainty preserve it for inspection.
			if _, cleanup := s.git(ctx, o.CWD, "worktree", "remove", "--", workspace); cleanup == nil {
				_ = s.remove(ctx, filepath.Dir(workspace), false)
				r.Worktree = ""
			}
		}
		return err
	}
	if reply.Thread.ID == "" {
		s.uncertain = true
		return fmt.Errorf("create response is missing its task ID; inspect before retrying")
	}
	r.Task, r.Outcome, r.Created = &reply.Thread, "created", true
	r.Task.ModelProvider = reply.Provider
	if o.ModelProvider != "" && reply.Provider != o.ModelProvider {
		return fmt.Errorf("new task did not retain requested provider; no message sent")
	}
	if o.Model != "" && reply.Model != o.Model {
		return fmt.Errorf("new task did not retain requested model; no message sent")
	}
	if reply.Model != "" {
		r.Task.Model = reply.Model
	}
	if r.Task.ReasoningEffort == nil {
		r.Task.ReasoningEffort = reply.Effort
	}
	if o.ReasoningEffort != "" && (r.Task.ReasoningEffort == nil || *r.Task.ReasoningEffort != o.ReasoningEffort) {
		return fmt.Errorf("new task did not retain requested reasoning effort; no message sent")
	}
	if o.Projectless && r.Task.ProjectID != "" {
		return fmt.Errorf("new task unexpectedly has a project assignment")
	}
	if project != "" && r.Task.ProjectID != project {
		return fmt.Errorf("new task did not retain its project assignment")
	}
	if o.Title != "" {
		if err := s.name(ctx, r.Task.ID, o.Title); err != nil {
			return err
		}
		r.Task.Name = o.Title
	}
	if o.Mode != "" {
		if err := s.mode(ctx, *r.Task, o.Mode, r); err != nil {
			return err
		}
		r.Outcome = "created"
	}
	if o.Message != "" || len(o.Images) > 0 {
		if err := s.message(ctx, o, r); err != nil {
			return err
		}
		if o.WaitHistory {
			return s.waitHistory(ctx, r)
		}
	}
	return nil
}
