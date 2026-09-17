package tasks

import (
	"encoding/json"
	"github.com/adenta/codex-tasks/internal/endpoints"
	"os"
	"path/filepath"
)

// Read-only presentation inventory. Desktop project IDs never leave this client.
func desktopProjects(c endpoints.Config) []map[string]string {
	var state struct {
		Projects []struct {
			HostID string `json:"hostId"`
			Path   string `json:"remotePath"`
			Label  string `json:"label"`
		} `json:"remote-projects"`
	}
	b, err := os.ReadFile(filepath.Join(c.CodexHome, ".codex-global-state.json"))
	if err != nil || json.Unmarshal(b, &state) != nil {
		return nil
	}
	out := []map[string]string{}
	for _, p := range state.Projects {
		for _, t := range c.Targets {
			if !t.NativeOnly && t.Alias != "" && p.HostID == "remote-ssh-discovered:"+t.Alias && filepath.IsAbs(p.Path) {
				out = append(out, map[string]string{"host": t.Host, "path": p.Path, "name": p.Label})
				break
			}
		}
	}
	return out
}
