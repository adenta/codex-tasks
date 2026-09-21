package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/adenta/codex-tasks/internal/endpoints"
	"github.com/google/uuid"
)

const maxMessage = 1 << 20
const help = `Usage: codex-tasks OPERATION [TASK] [OPTIONS]
  find --query TEXT [--archive all|active|archived] [--limit N] [--cursor CURSOR]
  projects | list [--project ID] [--archived] [--limit N] [--cursor CURSOR]
  read TASK [--turn ID] [--limit N] [--cursor CURSOR]
       [--item ID --offset N] [--max-chars N] [--include-outputs]
  create --cwd DIRECTORY [--project ID | --projectless] [--checkout | --ref REF]
         [--title TITLE] [--model MODEL] [--model-provider PROVIDER]
         [--model-context-window TOKENS] [--reasoning-effort VALUE]
         [--mode plan|default] [--message-file FILE|-] [--image FILE ...]
  fork TASK [--title TITLE] [--mode plan|default]
  message TASK [--message-file FILE|-] [--image FILE ...] [--wait DURATION]
  progress TASK [--turn ID] [--wait DURATION]
  mode TASK --mode plan|default
  archive TASK | unarchive TASK
Common: --target local|HOST/ACCOUNT or --host HOST; --json for scripts.
Defaults: English output; find searches inventory, other commands use the local account.
20 results per source/page, no wait; --wait is at most 60s.
Native desktop task tools remain necessary for desktop-only targets and handoff.
`

type Options struct {
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	Target          string        `json:"target,omitempty"`
	Query           string        `json:"query,omitempty"`
	Archive         string        `json:"archive,omitempty"`
	ItemID          string        `json:"item_id,omitempty"`
	Offset          int           `json:"offset,omitempty"`
	MaxChars        int           `json:"max_chars,omitempty"`
	IncludeOutputs  bool          `json:"include_outputs,omitempty"`
	Action          string        `json:"action"`
	TaskID          string        `json:"task_id,omitempty"`
	OperationID     string        `json:"operation_id"`
	Host            string        `json:"host,omitempty"`
	Message         string        `json:"message,omitempty"`
	Images          []string      `json:"images,omitempty"`
	CWD             string        `json:"cwd,omitempty"`
	Project         string        `json:"project,omitempty"`
	Title           string        `json:"title,omitempty"`
	ModelProvider   string        `json:"model_provider,omitempty"`
	ContextWindow   int           `json:"context_window,omitempty"`
	Model           string        `json:"model,omitempty"`
	Mode            string        `json:"mode,omitempty"`
	Ref             string        `json:"ref,omitempty"`
	Checkout        bool          `json:"checkout,omitempty"`
	Projectless     bool          `json:"projectless,omitempty"`
	Archived        bool          `json:"archived,omitempty"`
	Limit           int           `json:"limit"`
	Cursor          string        `json:"cursor,omitempty"`
	TurnID          string        `json:"turn_id,omitempty"`
	Wait            time.Duration `json:"wait,omitempty"`
	JSON            bool          `json:"-"`
}

func taskID(value string) (string, error) {
	if strings.HasPrefix(value, "codex://threads/") {
		value = strings.TrimPrefix(value, "codex://threads/")
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("expected a task UUID or codex://threads/UUID")
	}
	return id.String(), nil
}

func parse(args []string, stdin io.Reader) (Options, error) {
	o := Options{Action: args[0], OperationID: uuid.NewString()}
	fs := flag.NewFlagSet("tasks "+o.Action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Host, "host", "", "configured host")
	fs.StringVar(&o.Target, "target", "", "local or host/account")
	fs.StringVar(&o.Query, "query", "", "task link, ID, or title/preview words")
	fs.StringVar(&o.Archive, "archive", "", "all, active, or archived")
	fs.StringVar(&o.ItemID, "item", "", "read one history item")
	fs.IntVar(&o.Offset, "offset", 0, "character offset within --item")
	fs.IntVar(&o.MaxChars, "max-chars", 4000, "characters per history item")
	fs.BoolVar(&o.IncludeOutputs, "include-outputs", false, "include diagnostic tool items")
	fs.BoolVar(&o.JSON, "json", false, "JSON output")
	fs.StringVar(&o.CWD, "cwd", "", "workspace directory")
	fs.StringVar(&o.Project, "project", "", "project ID")
	fs.StringVar(&o.Title, "title", "", "task title")
	fs.StringVar(&o.Model, "model", "", "model override for new task")
	fs.StringVar(&o.ReasoningEffort, "reasoning-effort", "", "reasoning effort override for new task; omit for configured default")
	fs.StringVar(&o.ModelProvider, "model-provider", "", "configured provider for new task")
	fs.IntVar(&o.ContextWindow, "model-context-window", 0, "context tokens for explicit custom model")
	fs.StringVar(&o.Mode, "mode", "", "plan or default")
	fs.StringVar(&o.Ref, "ref", "", "Git starting ref")
	fs.BoolVar(&o.Checkout, "checkout", false, "use existing checkout")
	fs.BoolVar(&o.Projectless, "projectless", false, "omit project assignment")
	fs.BoolVar(&o.Archived, "archived", false, "list archived tasks")
	fs.IntVar(&o.Limit, "limit", 20, "page size")
	fs.StringVar(&o.Cursor, "cursor", "", "page cursor")
	fs.StringVar(&o.TurnID, "turn", "", "turn UUID")
	fs.DurationVar(&o.Wait, "wait", 0, "bounded progress wait")
	var messageFile string
	fs.StringVar(&messageFile, "message-file", "", "file or - for stdin")
	fs.Func("image", "local PNG/JPEG file (repeatable)", func(value string) error {
		path, err := filepath.Abs(value)
		if err == nil {
			o.Images = append(o.Images, path)
		}
		return err
	})
	tail := args[1:]
	// Accept the documented OPERATION TASK --flags form as well as flags first.
	if len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		o.TaskID = tail[0]
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		if o.TaskID != "" || fs.NArg() != 1 {
			return o, fmt.Errorf("expected one task ID")
		}
		o.TaskID = fs.Arg(0)
	}
	allowed := map[string]string{
		"find": "query archive limit cursor", "projects": "limit cursor", "list": "project archived limit cursor", "read": "turn limit cursor item offset max-chars include-outputs",
		"create": "cwd project projectless checkout ref title model model-provider model-context-window reasoning-effort mode message-file image wait", "fork": "title mode",
		"message": "message-file image wait", "progress": "turn wait", "mode": "mode", "archive": "", "unarchive": "",
	}
	names, ok := allowed[o.Action]
	if !ok {
		return o, fmt.Errorf("unknown task operation %q", clean(o.Action, 100))
	}
	var bad string
	fs.Visit(func(f *flag.Flag) {
		if !strings.Contains(" host target json "+names+" ", " "+f.Name+" ") {
			bad = f.Name
		}
	})
	if bad != "" {
		return o, fmt.Errorf("--%s is not valid for %s", bad, o.Action)
	}
	if messageFile != "" {
		var reader io.Reader = stdin
		if messageFile != "-" {
			f, err := os.Open(messageFile)
			if err != nil {
				return o, err
			}
			defer f.Close()
			reader = f
		}
		b, err := io.ReadAll(io.LimitReader(reader, maxMessage+1))
		if err != nil {
			return o, err
		}
		if len(b) > maxMessage {
			return o, fmt.Errorf("message exceeds 1 MiB")
		}
		o.Message = string(b)
	}
	return o, validate(o)
}

func validate(o Options) error {
	if len(o.Images) > maxImages || (len(o.Images) > 0 && o.Action != "create" && o.Action != "message") {
		return fmt.Errorf("--image is supported by create/message only, up to 8 images")
	}
	for _, path := range o.Images {
		if !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, 0) {
			return fmt.Errorf("invalid image path")
		}
	}
	if o.Target != "" {
		if o.Host != "" {
			return fmt.Errorf("choose --target or --host, not both")
		}
		if o.Target != "local" {
			if _, _, err := splitTarget(o.Target); err != nil {
				return err
			}
		}
	}
	if o.Action == "find" && (strings.TrimSpace(o.Query) == "" || len(o.Query) > 1000) {
		return fmt.Errorf("find requires --query with 1–1000 bytes")
	}
	if o.Archive != "" && o.Archive != "all" && o.Archive != "active" && o.Archive != "archived" {
		return fmt.Errorf("archive must be all, active, or archived")
	}
	if o.Offset < 0 || o.MaxChars < 0 || o.MaxChars > 32000 || len(o.ItemID) > 512 {
		return fmt.Errorf("invalid history bounds; max-chars must be 1–32000")
	}
	if o.Offset != 0 && o.ItemID == "" {
		return fmt.Errorf("--offset requires --item")
	}
	if o.Host != "" {
		if _, err := endpoints.ParseHost(strings.ToLower(o.Host)); err != nil {
			return err
		}
	}
	if o.Limit < 1 || o.Limit > 100 {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	if o.Wait < 0 || o.Wait > 60*time.Second {
		return fmt.Errorf("wait must be between 0s and 60s")
	}
	if o.ModelProvider != "" && (o.Action != "create" || o.Model == "" || len(o.ModelProvider) > 128 || strings.ContainsAny(o.ModelProvider, " \t\n\r")) {
		return fmt.Errorf("--model-provider requires create with --model and a provider ID")
	}
	if o.ReasoningEffort != "" && (o.Action != "create" || len(o.ReasoningEffort) > 64 || strings.ContainsAny(o.ReasoningEffort, " \t\n\r") || o.ReasoningEffort == "default") {
		return fmt.Errorf("--reasoning-effort requires create and an advertised effort value; omit for default")
	}
	if o.ContextWindow < 0 || (o.ContextWindow != 0 && (o.ModelProvider == "" || o.ContextWindow < 1024)) {
		return fmt.Errorf("--model-context-window requires an explicit provider and at least 1024 tokens")
	}
	if o.Mode != "" && o.Mode != "plan" && o.Mode != "default" {
		return fmt.Errorf("mode must be plan or default")
	}
	if len(o.Message) > maxMessage || len(o.Title) > 512 || len(o.Cursor) > 8192 {
		return fmt.Errorf("argument exceeds its size limit")
	}
	if _, err := uuid.Parse(o.OperationID); err != nil {
		return fmt.Errorf("invalid operation ID")
	}
	if o.Projectless && o.Project != "" {
		return fmt.Errorf("choose --project or --projectless")
	}
	if o.Checkout && o.Ref != "" {
		return fmt.Errorf("choose --checkout or --ref")
	}
	if o.TurnID != "" {
		if _, err := uuid.Parse(o.TurnID); err != nil {
			return fmt.Errorf("invalid turn UUID")
		}
	}
	switch o.Action {
	case "find", "projects", "list", "create":
		if o.TaskID != "" {
			return fmt.Errorf("%s does not accept a positional task ID", o.Action)
		}
	case "read", "fork", "message", "progress", "mode", "archive", "unarchive":
		if _, err := taskID(o.TaskID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported task operation")
	}
	if o.Action == "create" && !filepath.IsAbs(o.CWD) {
		return fmt.Errorf("create requires --cwd with an absolute directory")
	}
	if o.Action == "message" && strings.TrimSpace(o.Message) == "" && len(o.Images) == 0 {
		return fmt.Errorf("message requires nonempty --message-file FILE or - or --image FILE")
	}
	if o.Action == "mode" && o.Mode == "" {
		return fmt.Errorf("mode requires --mode plan|default")
	}
	return nil
}

// Run uses only the current account's state and its configured connections.
func Run(paths endpoints.Config, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if code, handled := Help(args, stdout); handled {
		return code
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if args[0] == "targets" {
		if len(args) != 1 {
			return 2
		}
		type listedTarget struct {
			endpoints.Target
			Local bool `json:"local,omitempty"`
		}
		targets := []listedTarget{}
		for i, t := range paths.Sources() {
			targets = append(targets, listedTarget{Target: t, Local: i == 0})
		}
		_ = json.NewEncoder(stdout).Encode(map[string]any{"targets": targets})
		return 0
	}
	o, err := parse(args, stdin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if o.TaskID != "" {
		o.TaskID, _ = taskID(o.TaskID)
	}
	if home := os.Getenv("CODEX_HOME"); home != "" {
		if !filepath.IsAbs(home) {
			fmt.Fprintln(stderr, "CODEX_HOME must be absolute")
			return 2
		}
		paths.CodexHome = home
	}
	return run(ctx, paths, o, stdout, stderr)
}

func run(ctx context.Context, paths endpoints.Config, o Options, stdout, stderr io.Writer) int {
	o = localTarget(paths, o)
	target := string(paths.Host)
	if o.Host != "" {
		target = strings.ToLower(o.Host)
	}
	if o.Target != "" {
		target, _, _ = splitTarget(o.Target)
	}
	r := Result{OperationID: o.OperationID, Host: target, Account: paths.Account, Action: o.Action, Outcome: "ok"}
	opCtx, cancel := context.WithTimeout(ctx, operationTimeout(o))
	defer cancel()
	var actionErr error
	if o.Action == "find" {
		actionErr = discover(opCtx, paths, o, &r, executeAt)
	} else {
		actionErr = executeAt(opCtx, paths, o, &r)
	}
	setError(&r, actionErr)
	render(stdout, r, o.JSON)
	if r.Outcome == "unknown" {
		return 3
	}
	if r.Error != "" || r.Outcome == "failed" {
		return 1
	}
	return 0
}

func setError(r *Result, err error) {
	if err == nil {
		return
	}
	r.Error = clean(err.Error(), 1200)
	if r.Outcome != "unknown" {
		r.Outcome = "failed"
		if r.Created {
			r.Outcome = "partial"
		}
	}
	if r.ErrorCategory == "" {
		r.ErrorCategory = "operation_failed"
		var rejected *RPCError
		if errors.As(err, &rejected) {
			r.ErrorCategory = "server_rejected"
		}
	}
}
