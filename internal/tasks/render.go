package tasks

import (
	"encoding/json"
	"fmt"
	"io"
)

func render(w io.Writer, r Result, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(w).Encode(r)
		return
	}
	location := clean(r.Host, 80) + "/" + clean(r.Account, 80)
	if r.Action == "find" {
		if len(r.Tasks) == 0 {
			fmt.Fprintln(w, "No matching tasks were returned on this page.")
		} else {
			fmt.Fprintf(w, "Found %d matching task(s) on this page.\n", len(r.Tasks))
		}
		if r.Outcome == "ambiguous" {
			fmt.Fprintln(w, "Multiple tasks match this search. Select a task ID and its target before acting.")
		}
	} else {
		status := map[string]string{"ok": "Request completed", "created": "Task created", "partial": "Task created; setup is incomplete", "started": "The turn is running", "accepted": "Input accepted; consumption has not been confirmed", "completed": "The turn completed", "settings_updated": "Settings updated", "archived": "Task archived", "unarchived": "Task restored", "needs_attention": "The task needs your attention", "unknown": "The action's outcome is unknown; inspect the task before retrying", "failed": "The request failed", "interrupted": "The turn was interrupted"}[r.Outcome]
		if status == "" {
			status = "Task status: " + clean(r.Outcome, 80)
		}
		fmt.Fprintf(w, "%s on %s.\n", status, location)
	}
	printTask := func(t Task) {
		target := location
		if t.Host != "" {
			target = clean(t.Host, 80) + "/" + clean(t.Account, 80)
		}
		name := clean(t.Name, 512)
		if name == "" {
			name = "Untitled task"
		}
		fmt.Fprintf(w, "\n%s\nTask ID: %s\nTarget: %s\n", name, clean(t.ID, 100), target)
		if t.Status.Type != "" {
			fmt.Fprintf(w, "Status: %s\n", clean(t.Status.Type, 80))
		}
		if t.Archived {
			fmt.Fprintln(w, "Archived: yes")
		}
		if t.ProjectID != "" {
			fmt.Fprintf(w, "Project ID: %s\n", clean(t.ProjectID, 100))
		}
		if t.CWD != "" {
			fmt.Fprintf(w, "Workspace: %s\n", clean(t.CWD, 1000))
		}
	}
	if r.Task != nil {
		printTask(*r.Task)
	}
	for _, t := range r.Tasks {
		printTask(t)
	}
	for _, c := range r.Coverage {
		status := map[string]string{"complete": "search complete", "more": "more tasks remain to search", "unavailable": "could not search", "native_tools_required": "desktop search requires native tools"}[c.Status]
		fmt.Fprintf(w, "\n%s: %s.\n", clean(c.Target, 160), status)
		if c.Detail != "" {
			fmt.Fprintln(w, clean(c.Detail, 1200))
		}
	}
	if r.SearchComplete != nil && !*r.SearchComplete {
		fmt.Fprintln(w, "\nSearch coverage is incomplete. Missing results do not establish that a task does not exist.")
	}
	for _, p := range r.Projects {
		fmt.Fprintf(w, "\n%s\nProject ID: %s\n", clean(p.Name, 512), clean(p.ID, 100))
		if p.Unavailable {
			fmt.Fprintf(w, "Unavailable: %s\n", clean(p.Reason, 500))
		}
		for _, root := range p.Roots {
			fmt.Fprintf(w, "Root: %s\n", clean(root.Path, 1000))
		}
	}
	for _, i := range r.Items {
		label := map[string]string{"userMessage": "User", "agentMessage": "Assistant", "plan": "Plan"}[i.Type]
		if label == "" {
			label = "Diagnostic: " + clean(i.Type, 80)
		}
		fmt.Fprintf(w, "\n%s (item %s; turn %s):\n%s\n", label, clean(i.ID, 512), clean(i.TurnID, 100), readable(i.Text))
		if i.OmittedParts > 0 {
			fmt.Fprintf(w, "[Omitted %d non-text content part(s).]\n", i.OmittedParts)
		}
		if i.Truncated {
			fmt.Fprintf(w, "[Truncated. Repeat read with the same target/turn/output filter, --item %s --offset %d --cursor %s]\n", clean(i.ID, 512), i.NextOffset, i.ContinuationCursor)
		}
	}
	if r.OmittedItems > 0 {
		fmt.Fprintf(w, "\nOmitted %d non-message item(s). --include-outputs includes supported diagnostics; reasoning is omitted.\n", r.OmittedItems)
	}
	if r.ItemFound != nil && !*r.ItemFound {
		fmt.Fprintln(w, "Requested item was not found in the scanned history; follow the cursor if one is returned.")
	}
	if r.TurnID != "" {
		fmt.Fprintf(w, "Turn ID: %s (%s)\n", clean(r.TurnID, 100), clean(r.TurnStatus, 80))
	}
	if r.Mode != "" {
		fmt.Fprintf(w, "Mode for subsequent turns: %s\n", clean(r.Mode, 80))
	}
	if r.NextCursor != "" {
		fmt.Fprintf(w, "\nMore results remain. Repeat the same query and target with --cursor %s\n", r.NextCursor)
	}
	if r.Worktree != "" {
		fmt.Fprintf(w, "Worktree: %s\n", clean(r.Worktree, 1000))
	}
	if r.ProjectID != "" {
		fmt.Fprintf(w, "Project ID: %s\n", clean(r.ProjectID, 100))
	}
	if r.Attention != "" {
		fmt.Fprintf(w, "Open the task in Codex to handle: %s\n", clean(r.Attention, 200))
	}
	if r.Error != "" {
		if r.InputAccepted {
			fmt.Fprintln(w, "Input was accepted; inspect progress before sending it again.")
		}
		if r.Created {
			fmt.Fprintln(w, "Task created; setup is incomplete.")
			fmt.Fprintln(w, "Use the existing task ID above. Do not create a replacement merely because setup failed.")
		}
		fmt.Fprintln(w, clean(r.Error, 1200))
	}
}
