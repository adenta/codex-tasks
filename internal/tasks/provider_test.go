package tasks

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

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
