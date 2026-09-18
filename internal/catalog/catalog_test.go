package catalog

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalogCacheLifecycle(t *testing.T) {
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected credentials")
		}
		if fail {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write([]byte(`{"data":[{"id":" z ","name":" Zulu ","context_length":32000},{"id":"a","context_length":8000},{"id":"z","context_length":1},{"id":"invalid","context_length":0},{"id":"","context_length":5}]}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "cache", "models.json")
	get := func(refresh bool) Result {
		t.Helper()
		result, err := load(context.Background(), server.Client(), server.URL, path, refresh)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := get(false)
	if len(result.Models) != 2 || result.Models[0].Name != "a" || result.Models[1].Name != "Zulu" || result.Models[1].ContextLength != 32000 || result.RefreshedAt.IsZero() {
		t.Fatalf("bad normalization: %+v", result)
	}
	get(false)
	if calls != 1 {
		t.Fatalf("cached read made network request: %d", calls)
	}
	get(true)
	if calls != 2 {
		t.Fatal("refresh did not fetch")
	}
	before, _ := os.ReadFile(path)
	fail = true
	result = get(true)
	after, _ := os.ReadFile(path)
	if result.Warning == "" || !bytes.Equal(before, after) {
		t.Fatal("failed refresh lost cache or warning")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary cache files leaked")
	}
}

func TestInvalidResponses(t *testing.T) {
	for _, body := range []string{`broken`, `{}`, `{"data":[]}`, `{"data":[{"id":"bad","context_length":-1}]}`, `{"data":[]} {}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "models.json")
			if _, err := load(context.Background(), server.Client(), server.URL, path, false); err == nil {
				t.Fatal("accepted invalid catalog")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("cached invalid response")
			}
		})
	}
	if err := decode(strings.NewReader(strings.Repeat(" ", maxBytes+1)), &Result{}); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := load(ctx, server.Client(), server.URL, filepath.Join(t.TempDir(), "models.json"), false); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestInvalidCacheAndSaveFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"a","context_length":10}]}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "models.json")
	os.WriteFile(path, []byte(`{"models":[{"id":"a","context_length":10}]}`), 0600)
	result, err := load(context.Background(), server.Client(), server.URL, path, false)
	if err != nil || result.RefreshedAt.IsZero() {
		t.Fatalf("invalid cache was not replaced: %v", err)
	}
	result, err = load(context.Background(), server.Client(), server.URL, filepath.Join(path, "child"), false)
	if err != nil || len(result.Models) != 1 || result.Warning == "" {
		t.Fatalf("save failure discarded fetched models: %+v %v", result, err)
	}
}

func TestRunCachedAndInvalidArguments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	path := filepath.Join(dir, "codex-tasks", "openrouter-models.json")
	if err := writeCache(path, Result{Models: []Model{{ID: "a", Name: "Alpha", ContextLength: 10}}, RefreshedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"--json"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"context_length":10`) {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	for _, args := range [][]string{{"--host", "remote"}, {"unexpected"}} {
		if code := Run(args, &out, &stderr); code != 2 {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}
