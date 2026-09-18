// Package endpoints describes account-owned connections, never server lifecycle.
package endpoints

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

var identity = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func ValidIdentity(s string) bool { return identity.MatchString(s) }
func ParseHost(s string) (string, error) {
	if !ValidIdentity(s) {
		return "", fmt.Errorf("invalid host identity")
	}
	return s, nil
}

type Target struct {
	Host    string `json:"host"`
	Account string `json:"account"`
	Alias   string `json:"ssh_alias,omitempty"`
	Socket  string `json:"socket,omitempty"`
}
type Config struct {
	Host      string   `json:"-"`
	Account   string   `json:"-"`
	CodexHome string   `json:"codex_home,omitempty"`
	Socket    string   `json:"socket,omitempty"`
	Targets   []Target `json:"targets,omitempty"`
}

func (c Config) SocketPath() string {
	if c.Socket != "" {
		return c.Socket
	}
	return filepath.Join(c.CodexHome, "app-server-control/app-server-control.sock")
}
func (c Config) Sources() []Target {
	out := []Target{{Host: c.Host, Account: c.Account}}
	return append(out, c.Targets...)
}

// Load derives identity from the executing OS account, never from remote input.
func Load() (Config, error) {
	var c Config
	home, err := os.UserHomeDir()
	if err != nil {
		return c, err
	}
	configFile := os.Getenv("CODEX_TASKS_CONFIG")
	explicit := configFile != ""
	if !explicit {
		base, err := os.UserConfigDir()
		if err != nil {
			return c, err
		}
		configFile = filepath.Join(base, "codex-tasks/config.json")
	}
	if !filepath.IsAbs(configFile) {
		return c, fmt.Errorf("configuration path must be absolute")
	}
	f, err := os.Open(configFile)
	if err == nil {
		defer f.Close()
		d := json.NewDecoder(io.LimitReader(f, 1<<20))
		d.DisallowUnknownFields()
		if err = d.Decode(&c); err != nil {
			return c, fmt.Errorf("invalid endpoint configuration: %w", err)
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return c, fmt.Errorf("configuration must contain one JSON object")
		}
	} else if explicit || !os.IsNotExist(err) {
		return c, err
	}
	host, err := os.Hostname()
	if err != nil {
		return c, err
	}
	c.Host = strings.ToLower(host)
	u, err := user.Current()
	if err != nil {
		return c, err
	}
	c.Account = u.Username
	if c.CodexHome == "" {
		c.CodexHome = filepath.Join(home, ".codex")
	}
	if v := os.Getenv("CODEX_HOME"); v != "" {
		c.CodexHome = v
	}
	if !filepath.IsAbs(c.CodexHome) || (c.Socket != "" && !filepath.IsAbs(c.Socket)) {
		return c, fmt.Errorf("Codex home and socket paths must be absolute")
	}
	if !ValidIdentity(c.Host) || !ValidIdentity(c.Account) {
		return c, fmt.Errorf("unsupported local host/account identity")
	}
	seen := map[string]bool{c.Host: true}
	for i := range c.Targets {
		t := &c.Targets[i]
		t.Host = strings.ToLower(t.Host)
		if !ValidIdentity(t.Host) || !ValidIdentity(t.Account) || !ValidIdentity(t.Alias) {
			return c, fmt.Errorf("target requires valid host, account and existing SSH alias")
		}
		if t.Socket != "" && (!filepath.IsAbs(t.Socket) || strings.ContainsAny(t.Socket, "\x00\r\n")) {
			return c, fmt.Errorf("target socket must be an absolute path")
		}
		if seen[t.Host] {
			return c, fmt.Errorf("duplicate host %q; configure one account per host", t.Host)
		}
		seen[t.Host] = true
	}
	return c, nil
}
