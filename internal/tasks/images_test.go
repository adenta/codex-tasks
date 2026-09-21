package tasks

import (
	"bytes"
	"context"
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
		{"create", "--cwd", filepath.Dir(path), "--projectless", "--image", path},
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
	old, err := newImageDir(p, "send-")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-imageRetention - time.Hour)
	_ = os.Chtimes(old, past, past)
	_, err = newImageDir(p, "send-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired attachment survived")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("recent submission directory removed")
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

func TestImageStagingIdentityAndBounds(t *testing.T) {
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
