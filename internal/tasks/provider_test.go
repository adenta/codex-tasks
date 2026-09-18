package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingProviderDoesNotRetryOrSendMessage(t *testing.T) {
	o := opts("create", "")
	o.CWD = t.TempDir()
	o.Projectless = true
	o.Model = "deepseek/deepseek-v4.1-flash"
	o.ModelProvider = "openrouter_together"
	o.Message = "must not be sent"
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m != "thread/start" || p["modelProvider"] != o.ModelProvider {
			t.Fatalf("unexpected fallback or message: %s %#v", m, p)
		}
		return nil, fmt.Errorf("provider not configured")
	}}
	s := service{rpc: f, home: t.TempDir()}
	r := Result{}
	if err := s.create(context.Background(), o, &r); err == nil || len(f.methods) != 1 || r.InputAccepted {
		t.Fatalf("unsafe failure handling: %v %+v", err, r)
	}
}

func TestRemoteTransportsMappedProvider(t *testing.T) {
	dir := t.TempDir()
	captured := filepath.Join(dir, "request.json")
	script := "#!/bin/sh\nfor arg do last=$arg; done\nif [ \"$last\" = _capabilities ]; then printf '%s\\n' '{\"tasks_protocol\":2,\"model_provider\":true,\"host\":\"grace\",\"account\":\"agent\"}'; exit 0; fi\n/bin/cat > '" + captured + "'\nexit 255\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	o := opts("create", "")
	o.Host = "grace"
	o.Model = "deepseek/deepseek-v4.1-flash"
	o.ModelProvider = "openrouter_together"
	o.ContextWindow = 32000
	r := Result{Account: "agent"}
	_ = dispatch(context.Background(), "grace", o, &r)
	b, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	var request remoteRequest
	if err := json.Unmarshal(b, &request); err != nil {
		t.Fatal(err)
	}
	if request.Options.ModelProvider != o.ModelProvider || request.Options.Model != o.Model || request.Options.ContextWindow != o.ContextWindow {
		t.Fatalf("route changed in transport: %+v", request.Options)
	}
}

func TestProviderMismatchNeverStartsPaidTurn(t *testing.T) {
	o := opts("create", "")
	o.CWD = t.TempDir()
	o.Projectless = true
	o.Model = "vendor/model"
	o.ModelProvider = "custom"
	o.ContextWindow = 32000
	o.Message = "must not be sent"
	task := storedTask()
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m != "thread/start" {
			return nil, fmt.Errorf("unexpected RPC: %s", m)
		}
		if p["modelProvider"] != "custom" || p["model"] != "vendor/model" || p["serviceTier"] != "default" {
			t.Fatalf("wrong request: %#v", p)
		}
		if p["config"].(map[string]any)["model_context_window"] != 32000 {
			t.Fatal("context not supplied")
		}
		return map[string]any{"thread": task, "model": "vendor/model", "modelProvider": "wrong"}, nil
	}}
	s := service{rpc: f, home: t.TempDir()}
	r := Result{}
	err := s.create(context.Background(), o, &r)
	if err == nil || !strings.Contains(err.Error(), "provider") || r.Task == nil || len(f.methods) != 1 {
		t.Fatalf("unsafe mismatch handling: %v %+v", err, r)
	}
}
func TestProviderFlagValidation(t *testing.T) {
	for _, args := range [][]string{{"create", "--projectless", "--model-provider", "custom"}, {"create", "--projectless", "--model-context-window", "32000"}, {"list", "--model-provider", "custom"}} {
		if _, err := parse(args, strings.NewReader("")); err == nil {
			t.Fatalf("accepted invalid flags: %v", args)
		}
	}
	o, err := parse([]string{"create", "--projectless", "--model-provider", "custom", "--model", "vendor/model", "--model-context-window", "32000"}, strings.NewReader(""))
	if err != nil || o.ModelProvider != "custom" || o.ContextWindow != 32000 {
		t.Fatalf("%+v %v", o, err)
	}
}
