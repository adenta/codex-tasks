package tasks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
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

// The subprocess substitutes only SSH/SFTP; it never contacts a host or model.
func TestImageTransportHelper(t *testing.T) {
	kind := os.Getenv("TASKS_IMAGE_HELPER")
	if kind == "" {
		return
	}
	home := os.Getenv("TASKS_IMAGE_REMOTE_HOME")
	mode := os.Getenv("TASKS_IMAGE_MODE")
	marker := os.Getenv("TASKS_IMAGE_MARKER")
	record := func(s string) {
		f, _ := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if f != nil {
			fmt.Fprintln(f, s)
			_ = f.Close()
		}
	}
	last := os.Args[len(os.Args)-1]
	if kind == "sftp" {
		record("upload")
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			// Test-generated paths have no spaces/escapes; real OpenSSH quoting has
			// a separate standalone SFTP-server test below.
			fields := strings.Fields(line)
			if len(fields) != 3 {
				os.Exit(2)
			}
			a, b := strings.Trim(fields[1], "\""), strings.Trim(fields[2], "\"")
			if fields[0] == "put" {
				data, err := os.ReadFile(a)
				if err != nil {
					os.Exit(2)
				}
				if err := os.WriteFile(b, data, 0600); err != nil {
					os.Exit(2)
				}
				if mode == "upload-failed" {
					os.Exit(1)
				}
			} else if fields[0] == "rename" {
				if os.Rename(a, b) != nil {
					os.Exit(2)
				}
			} else {
				os.Exit(2)
			}
		}
		os.Exit(0)
	}
	p := endpoints.Config{Host: "love", Account: "agent", CodexHome: home}
	switch last {
	case "_capabilities":
		host := "love"
		if mode == "wrong-host" {
			host = "other"
		}
		protocol := tasksProtocol
		if mode == "old" {
			protocol--
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"tasks_protocol": protocol, "host": host, "account": "agent"})
	case "_images":
		b, _ := io.ReadAll(os.Stdin)
		var req imageRequest
		_ = json.Unmarshal(b, &req)
		record(req.Action)
		os.Exit(runImages(p, bytes.NewReader(b), os.Stdout))
	case "_remote":
		record("submit")
		var req remoteRequest
		if json.NewDecoder(os.Stdin).Decode(&req) != nil || len(req.Options.Images) != 1 || validateImageFiles(req.Options.Images) != nil {
			os.Exit(2)
		}
		if !strings.HasPrefix(req.Options.Images[0], imageRoot(p)+"/") {
			os.Exit(2)
		}
		if mode == "unknown" {
			os.Exit(255)
		}
		r := Result{OperationID: req.Options.OperationID, Action: req.Options.Action, Host: "love", Account: "agent", Outcome: "started", InputAccepted: true}
		if mode == "rejected" {
			r.Outcome = "failed"
			r.InputAccepted = false
			r.Error = "rejected"
		}
		_ = json.NewEncoder(os.Stdout).Encode(r)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func TestRemoteImageDelivery(t *testing.T) {
	for _, mode := range []string{"success", "upload-failed", "unknown", "rejected", "old", "wrong-host"} {
		t.Run(mode, func(t *testing.T) {
			bin := t.TempDir()
			local := t.TempDir()
			remote := t.TempDir()
			marker := filepath.Join(t.TempDir(), "calls")
			for _, kind := range []string{"ssh", "sftp"} {
				script := "#!/bin/sh\nTASKS_IMAGE_HELPER=" + kind + " exec '" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run=^TestImageTransportHelper$ -- \"$@\"\n"
				if err := os.WriteFile(filepath.Join(bin, kind), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			t.Setenv("TASKS_IMAGE_MODE", mode)
			t.Setenv("TASKS_IMAGE_REMOTE_HOME", remote)
			t.Setenv("TASKS_IMAGE_MARKER", marker)
			p := endpoints.Config{Host: "xps", Account: "andre", CodexHome: local, Targets: []endpoints.Target{{Host: "love", Account: "agent", Alias: "love"}}}
			source := testPNG(t, t.TempDir())
			o := opts("create", "")
			o.Host = "love"
			o.Projectless = true
			o.Images = []string{source}
			r := Result{OperationID: o.OperationID, Action: o.Action, Outcome: "ok"}
			err := executeAt(context.Background(), p, o, &r)
			calls, _ := os.ReadFile(marker)
			entries, _ := os.ReadDir(imageRoot(endpoints.Config{CodexHome: remote}))
			switch mode {
			case "success":
				if err != nil || !r.InputAccepted || len(entries) != 1 || string(calls) != "prepare\nupload\nsubmit\n" {
					t.Fatal(r, err, string(calls), len(entries))
				}
			case "unknown":
				if err == nil || r.Outcome != "unknown" || len(entries) != 1 || string(calls) != "prepare\nupload\nsubmit\n" {
					t.Fatal(r, err, string(calls), len(entries))
				}
			case "upload-failed":
				if err == nil || r.Outcome != "failed" || len(entries) != 0 || string(calls) != "prepare\nupload\nremove\n" {
					t.Fatal(r, err, string(calls), len(entries))
				}
			case "rejected":
				if len(entries) != 0 || string(calls) != "prepare\nupload\nsubmit\nremove\n" {
					t.Fatal(r, err, string(calls), len(entries))
				}
			default:
				if err == nil || len(entries) != 0 || len(calls) != 0 {
					t.Fatal(r, err, string(calls), len(entries))
				}
			}
			localEntries, _ := os.ReadDir(imageRoot(p))
			if len(localEntries) != 0 {
				t.Fatal("local transport snapshots leaked")
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatal("source deleted")
			}
		})
	}
}

func TestSFTPBatchWithRealLocalServer(t *testing.T) {
	server := ""
	for _, path := range []string{"/usr/lib/ssh/sftp-server", "/usr/lib/openssh/sftp-server"} {
		if _, err := os.Stat(path); err == nil {
			server = path
			break
		}
	}
	sftp, err := exec.LookPath("sftp")
	if server == "" || err != nil {
		t.Skip("existing OpenSSH SFTP tools unavailable")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "sftp.log")
	script := "#!/bin/sh\nexec '" + sftp + "' -q -D '" + server + "' -b - 2>'" + log + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "sftp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	sourceDir := filepath.Join(t.TempDir(), "spaces [brackets] * ? \"quote\" \\slash")
	dest := filepath.Join(t.TempDir(), "remote [directory] space")
	_ = os.Mkdir(sourceDir, 0700)
	_ = os.Mkdir(dest, 0700)
	source := testPNG(t, sourceDir)
	remote, err := uploadImages(context.Background(), "unused", dest, []string{source})
	if err != nil {
		b, _ := os.ReadFile(log)
		t.Fatalf("%v: %s", err, b)
	}
	a, _ := os.ReadFile(source)
	b, err := os.ReadFile(remote[0])
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("transfer corrupted", err)
	}
	if _, err := sftpPath("/bad\npath"); err == nil {
		t.Fatal("accepted batch injection")
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
	p := endpoints.Config{Host: "love", Account: "agent", CodexHome: t.TempDir()}
	for _, req := range []imageRequest{
		{Host: "wrong", Account: "agent", Action: "prepare"},
		{Host: "love", Account: "wrong", Action: "prepare"},
		{Host: "love", Account: "agent", Action: "remove", Directory: p.CodexHome},
	} {
		b, _ := json.Marshal(req)
		var out bytes.Buffer
		if code := runImages(p, bytes.NewReader(b), &out); code != 2 {
			t.Fatal("unsafe staging accepted", req)
		}
	}
	if _, err := os.Stat(imageRoot(p)); !os.IsNotExist(err) {
		t.Fatal("identity mismatch mutated files")
	}
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
