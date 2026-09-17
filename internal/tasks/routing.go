package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

func splitTarget(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("target must be HOST/ACCOUNT")
	}
	host := strings.ToLower(parts[0])
	if !endpoints.ValidIdentity(host) || !endpoints.ValidIdentity(parts[1]) {
		return "", "", fmt.Errorf("invalid host/account")
	}
	return host, parts[1], nil
}

func resolveRoute(p endpoints.Config, o Options) (host, account, alias string, err error) {
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
		if t.NativeOnly {
			err = fmt.Errorf("%s/%s is a desktop account; use native desktop tools", host, account)
			return
		}
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
		if strings.Contains(err.Error(), "desktop account") {
			r.ErrorCategory = "unsupported_operation"
		}
		return err
	}
	o.Target, o.Host = "", host
	if alias == "" {
		return executeLocal(ctx, p, o, r)
	}
	return dispatch(ctx, alias, o, r)
}

// Read-only handshake precedes dispatch; the executing helper checks the same
// identity and protocol again before touching any task.
func checkRemoteTasks(ctx context.Context, alias, host, account string, r *Result, launcher ...bool) error {
	cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes", "--", alias, endpoints.Command, "_capabilities")
	var output limitedBuffer
	var diagnostic diagnosticBuffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	err := cmd.Run()
	var cap struct {
		Protocol int    `json:"tasks_protocol"`
		Launcher bool   `json:"remote_launcher"`
		Host     string `json:"host"`
		Account  string `json:"account"`
	}
	if err != nil {
		r.ErrorCategory = "transport_unavailable"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() != 255 {
			r.ErrorCategory = "remote_incompatible"
		}
		return fmt.Errorf("cannot check %s/%s: %s. No task action was sent", host, account, diagnostic.message(err))
	}
	if json.Unmarshal(output.data, &cap) != nil || cap.Protocol != 2 {
		r.ErrorCategory = "remote_incompatible"
		return fmt.Errorf("update codex-tasks on %s before using task tools v2. No task action was sent", host)
	}
	if len(launcher) > 0 && launcher[0] && !cap.Launcher {
		r.ErrorCategory = "remote_incompatible"
		return fmt.Errorf("update codex-tasks on %s for remote launcher support; no task was created", host)
	}
	if cap.Host != host || cap.Account != account {
		r.ErrorCategory = "destination_mismatch"
		return fmt.Errorf("connection identifies %s/%s; expected %s/%s. No task action was sent", clean(cap.Host, 80), clean(cap.Account, 80), host, account)
	}
	return nil
}

type diagnosticBuffer struct{ data []byte }

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 2048 - len(b.data); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *diagnosticBuffer) message(err error) string {
	// Recognized transport errors only: remote stderr can contain arbitrary
	// shell output, credentials, or echoed input and must not be reproduced.
	lower := strings.ToLower(string(b.data))
	for _, reason := range []string{"permission denied", "host key verification failed", "connection refused", "connection timed out", "could not resolve hostname", "no route to host", "command not found", "not found", "connection closed"} {
		if strings.Contains(lower, reason) {
			return reason
		}
	}
	if err != nil {
		return clean(err.Error(), 200)
	}
	return "invalid remote response"
}
