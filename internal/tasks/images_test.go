package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

func testPNG(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "screenshot.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImageArgumentsAndValidation(t *testing.T) {
	path := testPNG(t, t.TempDir())
	for _, args := range [][]string{
		{"create", "--projectless", "--image", path, "--wait-history"},
		{"message", uuidForTest(), "--image", path},
	} {
		o, err := parse(args, strings.NewReader(""))
		if err != nil || !reflect.DeepEqual(o.Images, []string{path}) {
			t.Fatalf("%+v %v", o, err)
		}
	}
	if _, err := parse([]string{"read", uuidForTest(), "--image", path}, nil); err == nil {
		t.Fatal("read accepted an image")
	}
	p := endpoints.Config{CodexHome: t.TempDir()}
	dir, files, err := stageImages(p, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	after, _ := os.ReadFile(files[0])
	if !bytes.Equal(before, after) || dir == filepath.Dir(path) {
		t.Fatal("snapshot changed bytes or reused source")
	}
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("snapshot not private")
	}
	bad := filepath.Join(t.TempDir(), "bad.png")
	_ = os.WriteFile(bad, []byte("not a PNG"), 0600)
	if err := validateImageFiles([]string{bad}); err == nil {
		t.Fatal("accepted invalid image")
	}
	if err := validateImageFiles(make([]string, 9)); err == nil {
		t.Fatal("accepted too many images")
	}
	if err := discardDrafts(p, []string{path, files[0]}); err == nil {
		t.Fatal("allowed deletion outside drafts")
	}
	old, err := newImageDir(p, "draft-")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-imageRetention - time.Hour)
	_ = os.Chtimes(old, past, past)
	_, err = newImageDir(p, "draft-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired draft survived")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("recent submission removed")
	}
}

func TestImageInputStartAndSteer(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			task := storedTask()
			task.Status.Type = "idle"
			if active {
				task.Status.Type = "active"
			}
			turn := Turn{ID: uuidForTest(), Status: "inProgress"}
			f := &fakeRPC{handle: func(method string, p map[string]any) (any, error) {
				switch method {
				case "thread/turns/list":
					return map[string]any{"data": []Turn{turn}}, nil
				case "turn/start", "turn/steer":
					want := []any{map[string]any{"type": "localImage", "path": "/private/image.png"}}
					if !reflect.DeepEqual(p["input"], want) {
						t.Fatalf("wrong image input: %#v", p)
					}
					if method == "turn/steer" {
						return map[string]any{"turnId": turn.ID}, nil
					}
					return map[string]any{"turn": turn}, nil
				}
				return nil, fmt.Errorf("unexpected %s", method)
			}}
			o := opts("message", task.ID)
			o.Images = []string{"/private/image.png"}
			r := Result{Task: &task}
			if err := (&service{rpc: f}).message(context.Background(), o, &r); err != nil || !r.InputAccepted {
				t.Fatal(r, err)
			}
		})
	}
}

func TestImageOnlyHistoryReady(t *testing.T) {
	task := storedTask()
	turn := uuidForTest()
	f := &fakeRPC{handle: func(method string, p map[string]any) (any, error) {
		if method == "thread/read" {
			return map[string]any{"thread": task}, nil
		}
		if method == "thread/items/list" {
			return map[string]any{"data": []any{map[string]any{"turnId": turn, "item": map[string]any{"type": "userMessage", "id": "image-only", "content": []any{map[string]any{"type": "localImage", "path": "/image.png"}}}}}}, nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	}}
	r := Result{Task: &task, TurnID: turn}
	if err := (&service{rpc: f}).waitHistory(context.Background(), &r); err != nil || !r.HistoryReady {
		t.Fatal(r, err)
	}
}

func TestClipboardImageAndDraftCleanup(t *testing.T) {
	bin := t.TempDir()
	source := testPNG(t, t.TempDir())
	script := "#!/bin/sh\nif [ \"$1\" = --list-types ]; then printf '%s\\n' \"$TASKS_CLIPBOARD_TYPES\"; else /bin/cat \"$TASKS_CLIPBOARD_IMAGE\"; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "wl-paste"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("TASKS_CLIPBOARD_IMAGE", source)
	p := endpoints.Config{CodexHome: t.TempDir()}
	for _, mime := range []string{"text/plain", "image/png", "image/gif"} {
		t.Setenv("TASKS_CLIPBOARD_TYPES", mime)
		var out bytes.Buffer
		err := clipboardImage(context.Background(), p, &out)
		if mime == "image/gif" {
			if err == nil {
				t.Fatal("silently ignored unsupported image")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Image bool
			Path  string
			URL   string
		}
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal(out.String())
		}
		if mime == "text/plain" {
			if result.Image {
				t.Fatal("text became image")
			}
			continue
		}
		if !result.Image || result.Path == "" || !strings.HasPrefix(result.URL, "file:///") {
			t.Fatal(result)
		}
		a, _ := os.ReadFile(source)
		b, _ := os.ReadFile(result.Path)
		if !bytes.Equal(a, b) {
			t.Fatal("clipboard bytes changed")
		}
		if err := discardDrafts(p, []string{result.Path}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
			t.Fatal("draft not removed")
		}
	}
}

func TestImageStagingIdentityAndBounds(t *testing.T) {
	var out bytes.Buffer
	w := imageWriter{w: &out, n: maxImageBytes - 1}
	if _, err := w.Write([]byte("xx")); err == nil || out.Len() != 0 {
		t.Fatal("clipboard size bound failed")
	}
	large := testPNG(t, t.TempDir())
	if err := os.Truncate(large, maxImageBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := validateImageFiles([]string{large}); err == nil {
		t.Fatal("oversized image accepted")
	}
	if err := os.Truncate(large, maxImageBytes); err != nil {
		t.Fatal(err)
	}
	if err := validateImageFiles([]string{large, large, large, large, large}); err == nil {
		t.Fatal("aggregate image bound failed")
	}
}

func TestImportAndPasteImagePaths(t *testing.T) {
	dir := t.TempDir()
	source := testPNG(t, dir)
	spaced := filepath.Join(dir, "image with spaces.png")
	data, _ := os.ReadFile(source)
	if err := os.WriteFile(spaced, data, 0600); err != nil {
		t.Fatal(err)
	}
	p := endpoints.Config{CodexHome: t.TempDir()}
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --list-types ]; then printf 'text/plain\\n'; else printf '%s' \"$TASKS_CLIPBOARD_TEXT\"; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "wl-paste"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, value := range []string{source, "\"" + spaced + "\"", "file://" + strings.ReplaceAll(spaced, " ", "%20")} {
		t.Setenv("TASKS_CLIPBOARD_TEXT", value)
		var out bytes.Buffer
		if err := clipboardImage(context.Background(), p, &out); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Image bool
			Path  string
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Image || result.Path == source || result.Path == spaced {
			t.Fatalf("not a private attachment: %+v", result)
		}
		got, err := os.ReadFile(result.Path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("import changed image")
		}
		if err := discardDrafts(p, []string{result.Path}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(spaced); err != nil {
			t.Fatal("source removed", err)
		}
	}
	for _, value := range []string{"file://otherhost/tmp/a.png", "relative.png", "/tmp/a.png\n/tmp/b.png"} {
		if localImagePath(value) != "" {
			t.Fatalf("accepted %q", value)
		}
	}
	invalid := filepath.Join(dir, "invalid.png")
	if err := os.WriteFile(invalid, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run(p, []string{"_import-image", invalid}, strings.NewReader(""), &out, &diagnostic); code != 1 {
		t.Fatal("invalid image accepted")
	}
	out.Reset()
	if code := Run(p, []string{"_import-image", spaced}, strings.NewReader(""), &out, &diagnostic); code != 0 {
		t.Fatal(diagnostic.String())
	}
}
