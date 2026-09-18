package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

func splitTarget(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("target must be local or HOST/ACCOUNT")
	}
	host := strings.ToLower(parts[0])
	if !endpoints.ValidIdentity(host) || !endpoints.ValidIdentity(parts[1]) {
		return "", "", fmt.Errorf("invalid host/account")
	}
	return host, parts[1], nil
}

// Expand the shorthand before routing, discovery, or activity attribution.
func localTarget(p endpoints.Config, o Options) Options {
	if o.Target == "local" {
		o.Target = p.Host + "/" + p.Account
	}
	return o
}

func resolveRoute(p endpoints.Config, o Options) (host, account, alias string, err error) {
	o = localTarget(p, o)
	host, account = p.Host, p.Account
	if o.Host != "" {
		host = strings.ToLower(o.Host)
	}
	if o.Target != "" {
		host, account, err = splitTarget(o.Target)
		if err != nil {
			return
		}
	}
	for _, t := range p.Sources() {
		if t.Host != host {
			continue
		}
		if o.Target != "" && t.Account != account {
			err = fmt.Errorf("configured connection reaches %s/%s, not %s/%s", host, t.Account, host, account)
			return
		}
		account = t.Account
		if host != p.Host {
			if t.Alias == "" {
				err = fmt.Errorf("no configured SSH route to %s/%s", host, account)
				return
			}
			alias = t.Alias
		}
		return
	}
	err = fmt.Errorf("no configured task route to %s/%s", host, account)
	return
}

func executeAt(ctx context.Context, p endpoints.Config, o Options, r *Result) error {
	host, account, alias, err := resolveRoute(p, o)
	r.Host, r.Account = host, account
	if err != nil {
		r.ErrorCategory = "route_unavailable"
		return err
	}
	o.Target, o.Host = "", host
	var c *Client
	if alias == "" {
		c, err = Dial(ctx, p.SocketPath())
	} else {
		socket := ""
		for _, t := range p.Targets {
			if t.Host == host && t.Account == account {
				socket = t.Socket
			}
		}
		c, err = DialRemote(ctx, alias, socket)
	}
	if err != nil {
		r.ErrorCategory = "transport_unavailable"
		return err
	}
	defer c.Close()
	if c.CodexHome == "" || c.PlatformOS != "linux" {
		return fmt.Errorf("stock server must report its Codex home and Linux platform")
	}
	s := service{rpc: c, client: c, home: c.CodexHome, localHome: p.CodexHome}
	if alias != "" {
		out, e := s.command(ctx, "", false, "sh", "-c", "hostname && id -un")
		if e != nil {
			return fmt.Errorf("cannot verify server identity: %w", e)
		}
		fields := strings.Fields(out)
		if len(fields) != 2 || strings.ToLower(fields[0]) != host || fields[1] != account {
			r.ErrorCategory = "destination_mismatch"
			return fmt.Errorf("server identity does not match %s/%s; no task action was sent", host, account)
		}
	}
	err = s.executeWithImages(ctx, p, o, r)
	if s.uncertain {
		r.Outcome, r.ErrorCategory = "unknown", "transport_uncertain"
	} else if err != nil && r.InputAccepted {
		r.Outcome, r.ErrorCategory = "unknown", "observation_unavailable"
	}
	return err
}
