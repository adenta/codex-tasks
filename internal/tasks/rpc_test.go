package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func TestRPCCommandExecStreamsOutputAndExit(t *testing.T) {
	socket := testSocket(t, func(c *websocket.Conn, ctx context.Context) {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var req struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(b, &req); err != nil {
				t.Error(err)
				return
			}
			if req.Method == "initialized" {
				continue
			}
			if req.Method == "initialize" {
				writeRPC(t, c, ctx, map[string]any{"id": req.ID, "result": map[string]any{}})
				continue
			}
			if req.Method != "command/exec" {
				t.Error("unexpected method", req.Method)
				return
			}
			for _, chunk := range []struct{ id, stream, text string }{{"other", "stdout", "ignore"}, {"setup", "stdout", "one\ntwo\n"}, {"setup", "stderr", "error\n"}} {
				writeRPC(t, c, ctx, map[string]any{"method": "command/exec/outputDelta", "params": map[string]any{"processId": chunk.id, "stream": chunk.stream, "deltaBase64": base64.StdEncoding.EncodeToString([]byte(chunk.text))}})
			}
			writeRPC(t, c, ctx, map[string]any{"id": req.ID, "result": map[string]any{"exitCode": 17, "stdout": "", "stderr": ""}})
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var output strings.Builder
	var reply commandResult
	err = c.CommandExec(ctx, map[string]any{"processId": "setup", "streamStdoutStderr": true, "command": []string{"fixture"}}, &reply, func(b []byte) { output.Write(b) })
	if err != nil || reply.ExitCode == nil || *reply.ExitCode != 17 || output.String() != "one\ntwo\nerror\n" {
		t.Fatalf("%+v %q %v", reply, output.String(), err)
	}
}

func testSocket(t *testing.T, handler func(*websocket.Conn, context.Context)) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "task-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		handler(c, r.Context())
	})}
	go server.Serve(listener)
	t.Cleanup(func() { _ = server.Close() })
	return socket
}
func writeRPC(t *testing.T, c *websocket.Conn, ctx context.Context, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Error(err)
	}
}

func TestRPCInterleavedNotificationAndApprovalDoNotBecomeReplies(t *testing.T) {
	id := uuid.NewString()
	turn := uuid.NewString()
	socket := testSocket(t, func(c *websocket.Conn, ctx context.Context) {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var request struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			_ = json.Unmarshal(b, &request)
			if request.Method == "initialized" {
				continue
			}
			if request.Method == "thread/read" {
				writeRPC(t, c, ctx, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": id, "turn": Turn{ID: turn, Status: "inProgress"}}})
				writeRPC(t, c, ctx, map[string]any{"id": "approval-1", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": id}})
			}
			if request.Method == "" {
				t.Error("client answered a user approval")
				return
			}
			writeRPC(t, c, ctx, map[string]any{"id": request.ID, "result": map[string]any{"ok": true}})
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var result map[string]any
	if err := c.Call(ctx, "thread/read", map[string]any{"threadId": id}, &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatal(result)
	}
	o := c.observe(id)
	if o.TurnID != turn || o.Attention != "item/commandExecution/requestApproval" {
		t.Fatal(o)
	}
}

func TestRPCServerRejectionSurvivesSuccessfulTransport(t *testing.T) {
	socket := testSocket(t, func(c *websocket.Conn, ctx context.Context) {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var r struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			_ = json.Unmarshal(b, &r)
			if r.Method == "initialized" {
				continue
			}
			if r.Method == "initialize" {
				writeRPC(t, c, ctx, map[string]any{"id": r.ID, "result": map[string]any{}})
			} else {
				writeRPC(t, c, ctx, map[string]any{"id": r.ID, "error": map[string]any{"code": -32601, "message": "unsupported operation"}})
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var result any
	if _, ok := c.Call(ctx, "thread/fork", map[string]any{}, &result).(*RPCError); !ok {
		t.Fatal("server failure was lost")
	}
}
