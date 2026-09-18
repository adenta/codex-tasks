package tasks

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

func (s *service) newProjectlessWorkspace(ctx context.Context) (string, error) {
	home, err := s.command(ctx, "", false, "sh", "-c", `printf '%s' "$HOME"`)
	if err != nil || !filepath.IsAbs(home) {
		return "", fmt.Errorf("cannot resolve destination home")
	}
	root := filepath.Join(home, "Documents", "Codex", time.Now().Format("2006-01-02"))
	if err = s.mkdir(ctx, root, true); err != nil {
		return "", err
	}
	dir, err := s.command(ctx, "", true, "mktemp", "-d", filepath.Join(root, "task-XXXXXXXX"))
	if err != nil {
		return "", err
	}
	for _, name := range []string{"work", "outputs"} {
		if err = s.mkdir(ctx, filepath.Join(dir, name), false); err != nil {
			if !s.uncertain {
				_ = s.remove(ctx, dir, true)
			}
			return "", err
		}
	}
	return dir, nil
}
func projectlessInstructions(dir string) string {
	return fmt.Sprintf("### Projectless Chat\nThis task starts in a generated directory under the user's Documents/Codex folder. The generated directory name is only a filesystem identifier; do not infer language, locale, or preferences from it.\nPrefer answering inline unless local files make the result more useful.\nUse %s for intermediate files, scratch analysis, scripts, drafts, and temporary assets.\nUse %s for user-facing deliverables. Link only files in that outputs directory when referring to saved deliverables.\nDo not write directly in the home directory unless explicitly asked.", filepath.Join(dir, "work"), filepath.Join(dir, "outputs"))
}
func (s *service) waitHistory(ctx context.Context, r *Result) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		page := Result{}
		if err := s.items(ctx, Options{TaskID: r.Task.ID, TurnID: r.TurnID, Limit: 100, MaxChars: 4000}, &page); err == nil {
			for _, item := range page.Items {
				if item.Type == "userMessage" && item.TurnID == r.TurnID {
					if _, err = s.thread(ctx, r.Task.ID); err == nil {
						r.HistoryReady = true
						return nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("message accepted but history is not yet readable; open the existing task without resending")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
