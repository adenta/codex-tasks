package tasks

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
)

// A stock JSON-RPC peer behind fake SSH. This subprocess can access only paths
// assigned by the fixture. Real Codex proxy compatibility is tested separately.
func TestStockStreamPeer(t *testing.T) {
	if os.Getenv("TASKS_STOCK_PEER") != "1" {
		return
	}
	mode := os.Getenv("TASKS_PEER_MODE")
	root := os.Getenv("TASKS_PEER_ROOT")
	record := func(m string) {
		f, _ := os.OpenFile(filepath.Join(root, "requests"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		defer f.Close()
		f.WriteString(m + "\n")
	}
	listener := &stdioListener{conn: &proxyConn{reader: os.Stdin, writer: os.Stdout, closeFn: func() {}}, done: make(chan struct{})}
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			os.Exit(2)
		}
		defer c.CloseNow()
		for {
			_, message, err := c.Read(context.Background())
			if err != nil {
				os.Exit(0)
			}
			var req struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(message, &req) != nil {
				os.Exit(2)
			}
			record(req.Method)
			if req.Method == "initialized" {
				continue
			}
			if mode == "pre-disconnect" {
				os.Exit(0)
			}
			result := any(map[string]any{})
			var failure any
			switch req.Method {
			case "initialize":
				result = map[string]any{"codexHome": filepath.Join(root, "server"), "platformOs": "linux"}
			case "command/exec":
				args := []string{}
				for _, v := range req.Params["command"].([]any) {
					args = append(args, v.(string))
				}
				req.Params["command"] = args
				if len(args) == 3 && args[2] == "hostname && id -un" {
					identity := "server\nagent\n"
					if mode == "wrong-host" {
						identity = "other\nagent\n"
					}
					if mode == "wrong-account" {
						identity = "server\nother\n"
					}
					result = map[string]any{"exitCode": 0, "stdout": identity}
				} else {
					var err error
					result, err, _ = fixtureDestination(context.Background(), req.Method, req.Params)
					if err != nil {
						failure = map[string]any{"code": -32000, "message": err.Error()}
					}
				}
			case "fs/writeFile":
				if mode == "upload-failed" {
					failure = map[string]any{"code": -32000, "message": "fixture upload rejected"}
					break
				}
				fallthrough
			case "fs/readDirectory", "fs/readFile", "fs/remove":
				var err error
				result, err, _ = fixtureDestination(context.Background(), req.Method, req.Params)
				if err != nil {
					failure = map[string]any{"code": -32000, "message": err.Error()}
				}
			case "thread/read":
				result = map[string]any{"thread": map[string]any{"id": req.Params["threadId"], "status": map[string]any{"type": "idle"}, "canAcceptDirectInput": true}}
			case "thread/start":
				result = map[string]any{"thread": map[string]any{"id": "00000000-0000-4000-8000-000000000001", "cwd": req.Params["cwd"]}}
			case "turn/start":
				if mode == "disconnect" {
					os.Exit(0)
				}
				if mode == "rejected" {
					failure = map[string]any{"code": -32000, "message": "fixture rejected"}
					break
				}
				if mode == "unsupported" {
					failure = map[string]any{"code": -32601, "message": "method unsupported"}
					break
				}
				record("input=" + string(message))
				result = map[string]any{"turn": map[string]any{"id": "turn", "status": "inProgress"}}
			default:
				failure = map[string]any{"code": -32601, "message": "unexpected method"}
			}
			reply := map[string]any{"id": req.ID, "result": result}
			if failure != nil {
				delete(reply, "result")
				reply["error"] = failure
			}
			b, _ := json.Marshal(reply)
			if c.Write(context.Background(), websocket.MessageText, b) != nil {
				os.Exit(0)
			}
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}

type stdioListener struct {
	conn     net.Conn
	mu       sync.Mutex
	accepted bool
	done     chan struct{}
	once     sync.Once
}

func (l *stdioListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.accepted {
		l.accepted = true
		l.mu.Unlock()
		return l.conn, nil
	}
	l.mu.Unlock()
	<-l.done
	return nil, io.EOF
}
func (l *stdioListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *stdioListener) Addr() net.Addr { return l.conn.LocalAddr() }

func TestRemoteStockTransport(t *testing.T) {
	for _, mode := range []string{"success", "pre-disconnect", "wrong-host", "wrong-account", "disconnect", "rejected", "unsupported", "upload-failed"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			os.Mkdir(bin, 0700)
			os.Mkdir(filepath.Join(root, "server"), 0700)
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(filepath.Join(root, "args")) + "\nexec " + shellQuote(os.Args[0]) + " -test.run=^TestStockStreamPeer$\n"
			if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			t.Setenv("TASKS_STOCK_PEER", "1")
			t.Setenv("TASKS_PEER_ROOT", root)
			t.Setenv("TASKS_PEER_MODE", mode)
			p := endpoints.Config{Host: "client", Account: "local", CodexHome: filepath.Join(root, "client"), Targets: []endpoints.Target{{Host: "server", Account: "agent", Alias: "configured-alias", Socket: "/socket/has ' quotes $(literal)"}}}
			os.Mkdir(p.CodexHome, 0700)
			o := opts("message", "00000000-0000-4000-8000-000000000001")
			o.Target = "server/agent"
			o.Message = "SECRET_PROMPT\n`literal` $(literal)"
			o.JSON = true
			source := testPNG(t, root)
			o.Images = []string{source}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var output, stderr strings.Builder
			code := run(ctx, p, o, &output, &stderr)
			var r Result
			if err := json.Unmarshal([]byte(output.String()), &r); err != nil {
				t.Fatal(err, output.String())
			}
			requests, _ := os.ReadFile(filepath.Join(root, "requests"))
			calls := string(requests)
			args, _ := os.ReadFile(filepath.Join(root, "args"))
			if !strings.Contains(string(args), "codex app-server proxy --sock "+shellQuote(p.Targets[0].Socket)) {
				t.Fatal("not stock proxy or incorrect shell quoting", string(args))
			}
			if strings.Contains(string(args), "SECRET") {
				t.Fatal("prompt sent through shell")
			}
			dirs, _ := os.ReadDir(filepath.Join(root, "server", "codex-tasks", "attachments"))
			switch mode {
			case "success":
				if code != 0 || !r.InputAccepted || len(dirs) != 1 || !strings.Contains(calls, "SECRET_PROMPT\\n`literal` $(literal)") {
					t.Fatal(r, calls, code)
				}
			case "disconnect":
				if code != 3 || r.Outcome != "unknown" || len(dirs) != 1 || strings.Count(calls, "turn/start\n") != 1 || strings.Contains(calls, "fs/remove") {
					t.Fatal(r, calls, code)
				}
			case "wrong-host", "wrong-account":
				if code != 1 || r.ErrorCategory != "destination_mismatch" || strings.Contains(calls, "fs/") || strings.Contains(calls, "thread/") {
					t.Fatal(r, calls)
				}
			case "pre-disconnect":
				if code != 1 || r.ErrorCategory != "transport_unavailable" || strings.Contains(calls, "turn/") {
					t.Fatal(r, calls)
				}
			case "upload-failed":
				if code != 1 || len(dirs) != 0 || strings.Contains(calls, "thread/") || !strings.Contains(calls, "fs/remove") {
					t.Fatal(r, calls)
				}
			default:
				if code != 1 || r.ErrorCategory != "server_rejected" || len(dirs) != 0 || strings.Count(calls, "turn/start\n") != 1 {
					t.Fatal(r, calls)
				}
			}
			localDirs, _ := os.ReadDir(imageRoot(p))
			if len(localDirs) != 0 {
				t.Fatal("snapshot leaked")
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatal("source deleted")
			}
			activity, _ := os.ReadFile(filepath.Join(p.CodexHome, "codex-tasks", "tasks-activity.jsonl"))
			if strings.Contains(string(activity), "SECRET") {
				t.Fatal("activity leaked prompt")
			}
		})
	}
}

func TestShellQuotePreservesSocketPath(t *testing.T) {
	value := "/tmp/a 'quoted' $(printf BAD) `printf BAD` \\ file"
	output, err := exec.Command("sh", "-c", "printf '%s' "+shellQuote(value)).Output()
	if err != nil || string(output) != value {
		t.Fatalf("unsafe quote: %q %v", output, err)
	}
}

func TestMissingStockProxy(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "attempts")
	script := "#!/bin/sh\nprintf x >> " + shellQuote(marker) + "\nexit 127\n"
	if err := os.WriteFile(filepath.Join(root, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := DialRemote(ctx, "configured-alias", ""); err == nil {
		t.Fatal("missing proxy succeeded")
	}
	calls, _ := os.ReadFile(marker)
	if string(calls) != "x" {
		t.Fatal("retried missing proxy", string(calls))
	}
}
