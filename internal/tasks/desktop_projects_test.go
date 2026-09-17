package tasks

import (
	"github.com/adenta/codex-tasks/internal/endpoints"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopProjectsUsePathsAndExactHost(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".codex-global-state.json"), []byte(`{"remote-projects":[{"id":"desktop-only-id","hostId":"remote-ssh-discovered:grace","remotePath":"/work/repo","label":"Repo"},{"hostId":"remote-ssh-discovered:other","remotePath":"/other"}]}`), 0600)
	p := desktopProjects(endpoints.Config{CodexHome: home, Targets: []endpoints.Target{{Host: "grace", Alias: "grace"}}})
	if len(p) != 1 || p[0]["path"] != "/work/repo" || p[0]["id"] != "" {
		t.Fatalf("bad inventory: %+v", p)
	}
}
