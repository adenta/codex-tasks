// Package tasks controls tasks on an existing Codex app server. It never starts
// a competing daemon or writes Codex's task index or conversation history.
package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/adenta/codex-tasks/internal/buildinfo"
	"github.com/coder/websocket"
)

type rpcReply struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// RPCError distinguishes an explicit server rejection from an uncertain write.
// The server's arbitrary error text is never put in the activity log.
type RPCError struct {
	Method  string
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s rejected (%d): %s", e.Method, e.Code, clean(e.Message, 1000))
}

type observation struct {
	TurnID     string
	TurnStatus string
	Attention  string
	Terminal   bool
}

type Client struct {
	conn          *websocket.Conn
	transport     *http.Transport
	mu            sync.Mutex
	next          int
	pending       map[int]chan rpcReply
	observed      map[string]observation
	commandOutput map[string]func([]byte)
	changed       chan struct{}
	done          chan struct{}
}

func Dial(ctx context.Context, socket string) (*Client, error) {
	info, err := os.Lstat(socket)
	if err != nil {
		return nil, fmt.Errorf("app-server control socket unavailable: %w", err)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSocket == 0 || !ok || int(owner.Uid) != os.Getuid() {
		return nil, fmt.Errorf("app-server socket must belong to the executing account and must not be a symlink")
	}
	t := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	conn, response, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: t}, CompressionMode: websocket.CompressionDisabled,
	})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.CloseIdleConnections()
		return nil, fmt.Errorf("app-server control socket unavailable: %w", err)
	}
	c := &Client{conn: conn, transport: t, pending: map[int]chan rpcReply{}, observed: map[string]observation{}, commandOutput: map[string]func([]byte){}, changed: make(chan struct{}, 1), done: make(chan struct{})}
	conn.SetReadLimit(16 << 20)
	go c.read()
	var result json.RawMessage
	if err := c.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "codex-tasks-tasks", "version": buildinfo.BuildID},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &result); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.write(ctx, map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Close() { _ = c.conn.CloseNow(); c.transport.CloseIdleConnections(); <-c.done }

type commandResult struct {
	ExitCode *int   `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func (c *Client) CommandExec(ctx context.Context, params map[string]any, result *commandResult, output func([]byte)) error {
	id := params["processId"].(string)
	c.mu.Lock()
	c.commandOutput[id] = output
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.commandOutput, id); c.mu.Unlock() }()
	err := c.Call(ctx, "command/exec", params, result)
	if ctx.Err() != nil {
		// Stop only this connection's command; never reissue setup after a lost reply.
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var ignored json.RawMessage
		_ = c.Call(stopCtx, "command/exec/terminate", map[string]any{"processId": id}, &ignored)
	}
	return err
}
func (c *Client) write(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, b)
}
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	c.next++
	id := c.next
	reply := make(chan rpcReply, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.write(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case r := <-reply:
		if r.Error != nil {
			return &RPCError{method, r.Error.Code, r.Error.Message}
		}
		if len(r.Result) == 0 {
			return fmt.Errorf("empty app-server response")
		}
		return json.Unmarshal(r.Result, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		// A final response can be followed immediately by a clean disconnect.
		select {
		case r := <-reply:
			if r.Error != nil {
				return &RPCError{method, r.Error.Code, r.Error.Message}
			}
			return json.Unmarshal(r.Result, result)
		default:
			return fmt.Errorf("app-server disconnected before replying")
		}
	}
}

func (c *Client) read() {
	defer close(c.done)
	for {
		_, b, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		var r rpcReply
		if json.Unmarshal(b, &r) != nil {
			return
		}
		if r.Method == "" {
			var id int
			if json.Unmarshal(r.ID, &id) != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- r:
				default:
				}
			}
			continue
		}
		if r.Method == "command/exec/outputDelta" {
			var p struct {
				ProcessID  string `json:"processId"`
				Delta      string `json:"deltaBase64"`
				CapReached bool   `json:"capReached"`
			}
			if json.Unmarshal(r.Params, &p) != nil {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(p.Delta)
			if err != nil {
				continue
			}
			c.mu.Lock()
			if output := c.commandOutput[p.ProcessID]; output != nil {
				output(data)
				if p.CapReached {
					output([]byte("\n[Codex output limit reached]\n"))
				}
			}
			c.mu.Unlock()
			continue
		}
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
			Status struct {
				ActiveFlags []string `json:"activeFlags"`
			} `json:"status"`
		}
		if json.Unmarshal(r.Params, &p) != nil || p.ThreadID == "" {
			continue
		}
		c.mu.Lock()
		o := c.observed[p.ThreadID]
		switch r.Method {
		case "turn/started", "turn/completed":
			o.TurnID, o.TurnStatus = p.Turn.ID, p.Turn.Status
			o.Terminal = r.Method == "turn/completed"
		case "thread/status/changed":
			o.Attention = ""
			if len(p.Status.ActiveFlags) > 0 {
				o.Attention = p.Status.ActiveFlags[0]
			}
		}
		// Approval, input, and dynamic tool requests belong to the user/hosting
		// client. Never invent replies or execute tools on this client's behalf.
		if len(r.ID) > 0 && string(r.ID) != "null" {
			o.Attention = r.Method
		}
		c.observed[p.ThreadID] = o
		c.mu.Unlock()
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}
}
func (c *Client) observe(id string) observation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observed[id]
}
