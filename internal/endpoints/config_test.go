package endpoints

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPathsAndIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("CODEX_TASKS_CONFIG", "")
	t.Setenv("CODEX_HOME", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.CodexHome != filepath.Join(root, ".codex") || c.Account == "" || c.Host == "" || len(c.Targets) != 0 {
		t.Fatal(c)
	}
	file := filepath.Join(root, "config.json")
	t.Setenv("CODEX_TASKS_CONFIG", file)
	if _, err := Load(); err == nil {
		t.Fatal("explicit missing config ignored")
	}
	if err := os.WriteFile(file, []byte(`{"codex_home":"/configured","socket":"/socket","targets":[{"host":"fixture-server","account":"agent","ssh_alias":"existing-alias"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", "/environment")
	c, err = Load()
	if err != nil || c.CodexHome != "/environment" || c.SocketPath() != "/socket" || c.Targets[0].Alias != "existing-alias" {
		t.Fatal(c, err)
	}
}
func TestRejectInvalidConfig(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CODEX_TASKS_CONFIG", f)
	t.Setenv("CODEX_HOME", "")
	for _, body := range []string{`{"host":"spoofed"}`, `{"account":"root"}`, `{"socket":"relative"}`, `{"codex_home":"relative"}`, `{"targets":[{"host":"server","account":"a","ssh_alias":"-oProxyCommand=bad"}]}`, `{"targets":[{"host":"server","account":"a","ssh_alias":"server"},{"host":"server","account":"b","ssh_alias":"other"}]}`, `{} {}`, `{"targets":[{"host":"server","account":"a"}]}`} {
		if err := os.WriteFile(f, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	t.Setenv("CODEX_TASKS_CONFIG", "relative")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatal(err)
	}
}
