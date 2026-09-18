package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

func TestLocalTargetRoutingAndDiscovery(t *testing.T) {
	p := endpoints.Config{Host: "workstation", Account: "agent", Targets: []endpoints.Target{{Host: "server", Account: "remote", Alias: "server-alias"}}}
	o, err := parse([]string{"find", "--query", "example", "--target", "local"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	host, account, alias, err := resolveRoute(p, o)
	if err != nil || host != p.Host || account != p.Account || alias != "" {
		t.Fatalf("route: %s/%s %q %v", host, account, alias, err)
	}
	calls := 0
	r := Result{}
	err = discover(context.Background(), p, o, &r, func(_ context.Context, _ endpoints.Config, query Options, result *Result) error {
		calls++
		if query.Target != "workstation/agent" {
			t.Errorf("searched unexpected target: %s", query.Target)
		}
		result.Tasks = []Task{{ID: "match"}}
		return nil
	})
	if err != nil || calls != 1 || len(r.Coverage) != 1 || r.Coverage[0].Target != "workstation/agent" || r.Tasks[0].Host != p.Host {
		t.Fatalf("discovery: %+v %v", r, err)
	}
	if _, err := parse([]string{"list", "--target", "local", "--host", "server"}, strings.NewReader("")); err == nil {
		t.Fatal("accepted conflicting selectors")
	}
}

func TestTargetsIncludesLocalIdentity(t *testing.T) {
	p := endpoints.Config{Host: "workstation", Account: "agent", CodexHome: t.TempDir(), Targets: []endpoints.Target{{Host: "server", Account: "remote", Alias: "server-alias"}}}
	var out, stderr strings.Builder
	if code := Run(p, []string{"targets"}, strings.NewReader(""), &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var result struct {
		Targets []struct {
			Host    string
			Account string
			Local   bool
		}
	}
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Targets) != 2 || !result.Targets[0].Local || result.Targets[0].Host != p.Host || result.Targets[0].Account != p.Account || result.Targets[1].Local {
		t.Fatalf("unexpected targets: %s", out.String())
	}
}

func TestLocalTargetReportsConnectionFailure(t *testing.T) {
	p := endpoints.Config{Host: "desktop", Account: "andre", CodexHome: t.TempDir()}
	r := Result{}
	err := executeAt(context.Background(), p, Options{Action: "list", Target: "local"}, &r)
	if err == nil || r.ErrorCategory != "transport_unavailable" || r.Host != p.Host || r.Account != p.Account {
		t.Fatalf("expected actual socket failure: %+v %v", r, err)
	}
}
