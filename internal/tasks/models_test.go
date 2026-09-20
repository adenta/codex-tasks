package tasks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestModelsUseSelectedServerAndAccountCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"endpoint","name":"Model","context_length":1000,"input_modalities":["text"],"reasoning_options":[{"type":"effort","values":["low","high"]}]}]}`)
	}))
	defer upstream.Close()
	provider, model := "openai", "subscription-model"
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		switch m {
		case "config/read":
			return map[string]any{"config": map[string]any{"model": model, "model_provider": provider, "model_providers": map[string]any{"modal": map[string]any{"base_url": upstream.URL + "/v1"}}}}, nil
		case "model/list":
			return map[string]any{"data": []any{map[string]any{"model": "subscription-default", "isDefault": true}}}, nil
		default:
			return nil, fmt.Errorf("unexpected RPC %s", m)
		}
	}}
	s := service{rpc: f}
	o := opts("models", "")
	read := func(account string) Result {
		t.Helper()
		r := Result{Host: "grace", Account: account}
		if err := s.models(context.Background(), o, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := read("agent")
	if r.DefaultModel != model || r.DefaultProvider != provider || r.SubscriptionModel != model || len(r.Models) != 1 || !reflect.DeepEqual(r.Models[0].InputModalities, []string{"text"}) {
		t.Fatalf("bad catalog/defaults: %+v", r)
	}
	provider, model = "modal", "endpoint"
	r = read("agent")
	if hits != 1 || r.DefaultModel != "endpoint" || r.DefaultProvider != "modal" || r.SubscriptionModel != "subscription-default" {
		t.Fatalf("defaults must stay fresh independently of cached catalog: %+v; hits=%d", r, hits)
	}
	read("other")
	if hits != 2 {
		t.Fatalf("catalog crossed account boundary: %d", hits)
	}
	upstream.Close()
	o.Refresh = true
	r = read("agent")
	if len(r.Models) != 1 || r.Warning == "" || r.DefaultModel != "endpoint" {
		t.Fatalf("failed refresh lost defaults/cache: %+v", r)
	}
}

func TestModelsWithoutProxyStillReportDefault(t *testing.T) {
	f := &fakeRPC{handle: func(m string, p map[string]any) (any, error) {
		if m == "config/read" {
			return map[string]any{"config": map[string]any{"model": "configured"}}, nil
		}
		if m == "model/list" {
			return nil, fmt.Errorf("unavailable")
		}
		t.Fatalf("unexpected RPC %s", m)
		return nil, nil
	}}
	r := Result{}
	if err := (&service{rpc: f}).models(context.Background(), opts("models", ""), &r); err != nil {
		t.Fatal(err)
	}
	if r.DefaultModel != "configured" || r.SubscriptionModel != "configured" || r.Warning == "" {
		t.Fatalf("bad defaults %+v", r)
	}
}
