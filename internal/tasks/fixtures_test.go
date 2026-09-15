package tasks

import (
	"github.com/adenta/codex-tasks/internal/endpoints"
	"path/filepath"
)

// Historical inventory is test data only; production uses explicit configuration.
func fixtureAccount(host, account, home string) (endpoints.Config, error) {
	c := endpoints.Config{Host: host, Account: account, CodexHome: filepath.Join(home, ".codex")}
	if host == "xps" {
		c.NativeOnly = true
		c.Targets = []endpoints.Target{{Host: "grace", Account: "agent", Alias: "grace"}, {Host: "love", Account: "agent", Alias: "love"}}
	} else {
		c.Targets = []endpoints.Target{{Host: "xps", Account: "andre", NativeOnly: true}, {Host: "love", Account: "agent"}}
	}
	return c, nil
}
func fixtureRole(host string) (endpoints.Config, error) {
	return fixtureAccount(host, "andre", "/nonexistent")
}
