package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/adenta/codex-tasks/internal/buildinfo"
	"github.com/adenta/codex-tasks/internal/endpoints"
	"github.com/google/uuid"
)

const maxMessage = 1 << 20
const remoteLimit = 8 << 20 // accounts for JSON escaping of a 1 MiB prompt
const help = `Usage: codex-tasks OPERATION [TASK] [OPTIONS]
  models [--json] [--refresh]
  environments --cwd DIRECTORY
  find --query TEXT [--archive all|active|archived] [--limit N] [--cursor CURSOR]
  projects | list [--project ID] [--archived] [--limit N] [--cursor CURSOR]
  read TASK [--turn ID] [--limit N] [--cursor CURSOR]
       [--item ID --offset N] [--max-chars N] [--include-outputs]
  create --cwd DIRECTORY [--project ID | --projectless] [--checkout | --ref REF]
         [--environment FILE.toml] [--title TITLE] [--model MODEL] [--model-provider PROVIDER] [--mode plan|default] [--message-file FILE|-] [--image FILE ...]
  fork TASK [--title TITLE] [--mode plan|default]
  message TASK [--message-file FILE|-] [--image FILE ...] [--wait DURATION]
  progress TASK [--turn ID] [--wait DURATION]
  mode TASK --mode plan|default
  archive TASK | unarchive TASK
  activity [--since DURATION] [--task ID] [--action NAME] [--outcome VALUE]
           [--limit N] [--follow]
Common: --target local|HOST/ACCOUNT or --host HOST; --json for scripts.
Operation attribution: --source-task ID
Defaults: English output; find searches inventory, other commands use the local account.
20 results per source/page, no wait, 24h activity; --wait is at most 60s.
Native desktop task tools remain necessary for desktop-only targets and handoff.
`

type Options struct {
	Environment     string        `json:"environment,omitempty"`
	WaitHistory     bool          `json:"wait_history,omitempty"`
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
	SourceTaskID    string        `json:"source_task_id,omitempty"`
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
	Since           time.Duration `json:"-"`
	ActivityTask    string        `json:"-"`
	ActivityAction  string        `json:"-"`
	ActivityOutcome string        `json:"-"`
	Follow          bool          `json:"-"`
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
	fs.StringVar(&o.SourceTaskID, "source-task", os.Getenv("CODEX_THREAD_ID"), "source task UUID")
	fs.StringVar(&o.CWD, "cwd", "", "workspace directory")
	fs.StringVar(&o.Environment, "environment", "", "environment filename for new worktree setup")
	fs.StringVar(&o.Project, "project", "", "project ID")
	fs.StringVar(&o.Title, "title", "", "task title")
	fs.StringVar(&o.Model, "model", "", "model override for new task")
	fs.StringVar(&o.ModelProvider, "model-provider", "", "configured provider for new task")
	fs.IntVar(&o.ContextWindow, "model-context-window", 0, "context tokens for explicit custom model")
	fs.StringVar(&o.Mode, "mode", "", "plan or default")
	fs.StringVar(&o.Ref, "ref", "", "Git starting ref")
	fs.BoolVar(&o.Checkout, "checkout", false, "use existing checkout")
	fs.BoolVar(&o.WaitHistory, "wait-history", false, "wait up to 10s for accepted input in history")
	fs.BoolVar(&o.Projectless, "projectless", false, "omit project assignment")
	fs.BoolVar(&o.Archived, "archived", false, "list archived tasks")
	fs.IntVar(&o.Limit, "limit", 20, "page size")
	fs.StringVar(&o.Cursor, "cursor", "", "page cursor")
	fs.StringVar(&o.TurnID, "turn", "", "turn UUID")
	fs.DurationVar(&o.Wait, "wait", 0, "bounded progress wait")
	fs.DurationVar(&o.Since, "since", 24*time.Hour, "activity window")
	fs.StringVar(&o.ActivityTask, "task", "", "activity task filter")
	fs.StringVar(&o.ActivityAction, "action", "", "activity operation filter")
	fs.StringVar(&o.ActivityOutcome, "outcome", "", "activity outcome filter")
	fs.BoolVar(&o.Follow, "follow", false, "follow activity")
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
		"environments": "cwd",
		"find":         "query archive limit cursor", "projects": "limit cursor", "list": "project archived limit cursor", "read": "turn limit cursor item offset max-chars include-outputs",
		"create": "cwd project projectless checkout ref environment title model model-provider model-context-window mode message-file image wait wait-history", "fork": "title mode",
		"message": "message-file image wait", "progress": "turn wait", "mode": "mode", "archive": "", "unarchive": "",
		"activity": "since task action outcome limit follow",
	}
	names, ok := allowed[o.Action]
	if !ok {
		return o, fmt.Errorf("unknown task operation %q", clean(o.Action, 100))
	}
	var bad string
	fs.Visit(func(f *flag.Flag) {
		if !strings.Contains(" host target json source-task "+names+" ", " "+f.Name+" ") {
			bad = f.Name
		}
	})
	if bad != "" {
		return o, fmt.Errorf("--%s is not valid for %s", bad, o.Action)
	}
	if o.Action == "activity" && (o.Host != "" || o.Target != "") {
		return o, fmt.Errorf("activity is stored on its originating host; run this command there through SSH")
	}
	if o.SourceTaskID != "" {
		id, err := taskID(o.SourceTaskID)
		if err != nil {
			return o, fmt.Errorf("invalid source task ID")
		}
		o.SourceTaskID = id
	}
	if o.ActivityTask != "" {
		id, err := taskID(o.ActivityTask)
		if err != nil {
			return o, err
		}
		o.ActivityTask = id
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
	if o.Environment != "" && (o.Action != "create" || o.Checkout || !filepath.IsAbs(o.CWD) || !validEnvironmentID(o.Environment)) {
		return fmt.Errorf("--environment requires create with an absolute --cwd, a .toml filename, and a new Git worktree")
	}
	if o.Action == "environments" && !filepath.IsAbs(o.CWD) {
		return fmt.Errorf("environments requires --cwd with an absolute directory")
	}
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
	if o.SourceTaskID != "" {
		if _, err := taskID(o.SourceTaskID); err != nil {
			return err
		}
	}
	if o.TurnID != "" {
		if _, err := uuid.Parse(o.TurnID); err != nil {
			return fmt.Errorf("invalid turn UUID")
		}
	}
	switch o.Action {
	case "find", "projects", "list", "create", "activity", "environments":
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
	if o.WaitHistory && (o.Action != "create" || (strings.TrimSpace(o.Message) == "" && len(o.Images) == 0)) {
		return fmt.Errorf("--wait-history requires create with text or images")
	}
	if o.Action == "create" && !(o.Projectless && o.CWD == "") && !filepath.IsAbs(o.CWD) {
		return fmt.Errorf("create requires --cwd with an absolute directory")
	}
	if o.Action == "message" && strings.TrimSpace(o.Message) == "" && len(o.Images) == 0 {
		return fmt.Errorf("message requires nonempty --message-file FILE or - or --image FILE")
	}
	if o.Action == "mode" && o.Mode == "" {
		return fmt.Errorf("mode requires --mode plan|default")
	}
	if o.Action == "activity" && o.Since <= 0 {
		return fmt.Errorf("since must be a positive duration")
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
	if args[0] == "_images" && len(args) == 1 {
		return runImages(paths, stdin, stdout)
	}
	if args[0] == "_clipboard-image" && len(args) == 1 {
		ctx, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		if err := clipboardImage(ctx, paths, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if args[0] == "_import-image" && len(args) == 2 {
		if err := importImage(paths, args[1], stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if args[0] == "_discard-images" {
		if err := discardDrafts(paths, args[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
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
		_ = json.NewEncoder(stdout).Encode(map[string]any{"targets": targets, "desktop_projects": desktopProjects(paths)})
		return 0
	}
	if args[0] == "_capabilities" {
		if len(args) != 1 {
			return 2
		}
		_ = json.NewEncoder(stdout).Encode(map[string]any{"tasks_protocol": tasksProtocol, "build_id": buildinfo.BuildID, "host": string(paths.Host), "account": paths.Account})
		return 0
	}
	if args[0] == "_remote" {
		return runRemote(ctx, paths, args[1:], stdin, stdout)
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
	if o.Action == "activity" {
		return showActivity(ctx, paths, o, stdout, stderr)
	}
	return run(ctx, paths, o, stdout, stderr)
}

func run(ctx context.Context, paths endpoints.Config, o Options, stdout, stderr io.Writer) int {
	o = localTarget(paths, o)
	start := time.Now()
	target := string(paths.Host)
	if o.Host != "" {
		target = strings.ToLower(o.Host)
	}
	if o.Target != "" {
		target, _, _ = splitTarget(o.Target)
	}
	r := Result{OperationID: o.OperationID, Host: target, Account: paths.Account, Action: o.Action, Outcome: "ok", ActivityStatus: "ok"}
	log := activityLog{home: paths.CodexHome}
	e := Activity{Event: "task_operation_started", Timestamp: float64(start.UnixNano()) / 1e9, OperationID: o.OperationID, BuildID: buildinfo.BuildID, Action: o.Action, Host: string(paths.Host), Account: paths.Account, TargetHost: target, SourceTaskID: o.SourceTaskID, TaskID: o.TaskID, Outcome: "started"}
	if e.SourceTaskID == "" {
		e.SourceTaskID = "unknown"
	}
	// The event's started means command invocation, not target turn execution.
	e.Outcome = "invoked"
	logErr := log.append(e)
	opCtx, cancel := context.WithTimeout(ctx, operationTimeout(o))
	defer cancel()
	var actionErr error
	if o.Action == "find" {
		actionErr = discover(opCtx, paths, o, &r, executeAt)
	} else {
		actionErr = executeAt(opCtx, paths, o, &r)
	}
	setError(&r, actionErr)
	e.Event, e.Timestamp, e.Outcome = "task_operation_outcome", float64(time.Now().UnixNano())/1e9, r.Outcome
	e.TargetHost = r.Host
	e.TargetAccount, e.ErrorCategory, e.DurationMS = r.Account, r.ErrorCategory, time.Since(start).Milliseconds()
	e.InputAccepted = r.InputAccepted
	if r.Task != nil {
		e.TaskID = r.Task.ID
	}
	e.TurnID = r.TurnID
	if err := log.append(e); err != nil {
		logErr = err
	}
	if logErr != nil {
		r.ActivityStatus = "unavailable"
		fmt.Fprintln(stderr, "Warning: task activity could not be fully recorded.")
	} else {
		r.ActivityStatus = "ok"
	}
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

func executeLocal(ctx context.Context, p endpoints.Config, o Options, r *Result) error {
	r.Host, r.Account = string(p.Host), p.Account
	if o.Action == "environments" {
		return listEnvironments(ctx, o.CWD, r)
	}
	if o.Action == "find" {
		return (&service{home: p.CodexHome}).find(ctx, o, r)
	}
	c, err := Dial(ctx, p.SocketPath())
	if err != nil {
		r.ErrorCategory = "transport_unavailable"
		return err
	}
	defer c.Close()
	s := service{rpc: c, client: c, home: p.CodexHome}
	err = s.execute(ctx, o, r)
	if s.uncertain {
		r.Outcome, r.ErrorCategory = "unknown", "transport_uncertain"
	} else if err != nil && r.InputAccepted {
		r.Outcome, r.ErrorCategory = "unknown", "observation_unavailable"
	}
	return err
}

func connection(p endpoints.Config, target string) (string, string, error) {
	for _, c := range p.Targets {
		if strings.EqualFold(c.Host, target) && c.Alias != "" {
			return c.Alias, c.Account, nil
		}
	}
	return "", "", fmt.Errorf("no configured connection to %s", clean(target, 80))
}

type remoteRequest struct {
	Version int     `json:"version"`
	Account string  `json:"account"`
	Options Options `json:"options"`
}

func dispatch(ctx context.Context, alias string, o Options, r *Result) error {
	if r.Account == "" {
		return fmt.Errorf("expected destination account is required")
	}
	if err := checkRemoteTasks(ctx, alias, strings.ToLower(o.Host), r.Account, r); err != nil {
		r.Outcome = "failed"
		return err
	}
	if len(o.Images) > 0 {
		req := imageRequest{Host: strings.ToLower(o.Host), Account: r.Account, Action: "prepare"}
		reply, err := remoteImages(ctx, alias, req)
		if err != nil {
			r.Outcome = "failed"
			return err
		}
		if !filepath.IsAbs(reply.Directory) || !strings.HasPrefix(filepath.Base(reply.Directory), "send-") {
			r.Outcome = "failed"
			return fmt.Errorf("invalid remote attachment directory; no task was submitted")
		}
		// Never delete files after an uncertain submission. Cleanup of known
		// failures gets its own bounded context, even if the upload timed out.
		defer func() {
			if r.Outcome != "unknown" && !r.InputAccepted {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				req.Action, req.Directory = "remove", reply.Directory
				_, _ = remoteImages(cleanup, alias, req)
			}
		}()
		o.Images, err = uploadImages(ctx, alias, reply.Directory, o.Images)
		if err != nil {
			r.Outcome = "failed"
			r.ErrorCategory = "image_upload_failed"
			return err
		}
	}
	b, _ := json.Marshal(remoteRequest{Version: tasksProtocol, Account: r.Account, Options: o})
	cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes", "--", alias, endpoints.Command, "_remote")
	cmd.Stdin = strings.NewReader(string(b))
	// Bound transport output, including noise from remote shell startup files.
	var output limitedBuffer
	cmd.Stdout = &output
	var diagnostic diagnosticBuffer
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	var reply Result
	if json.Unmarshal(output.data, &reply) == nil && reply.OperationID == o.OperationID && reply.Action == o.Action && reply.Host == strings.ToLower(o.Host) && reply.Account == r.Account && reply.Outcome != "" {
		*r = reply
		return nil
	}
	r.Outcome, r.ErrorCategory = "unknown", "transport_uncertain"
	if o.Action == "find" || o.Action == "list" || o.Action == "read" || o.Action == "projects" || o.Action == "progress" || o.Action == "environments" {
		r.Outcome, r.ErrorCategory = "failed", "transport_unavailable"
		return fmt.Errorf("remote read failed: %s", diagnostic.message(err))
	}
	if err == nil {
		return fmt.Errorf("remote response was invalid; inspect the target before retrying")
	}
	return fmt.Errorf("remote dispatch outcome is unknown: %s; inspect the target before retrying", diagnostic.message(err))
}

type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > remoteLimit {
		return 0, fmt.Errorf("remote output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func runRemote(ctx context.Context, p endpoints.Config, args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) != 0 {
		return 2
	}
	var request remoteRequest
	decoder := json.NewDecoder(io.LimitReader(stdin, remoteLimit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.Version != tasksProtocol {
		return 2
	}
	if err := validate(request.Options); err != nil {
		return 2
	}
	o := request.Options
	if request.Account != p.Account || o.Target != "" || o.Action == "activity" || (o.Host != "" && !strings.EqualFold(o.Host, string(p.Host))) {
		return 2
	}
	if o.TaskID != "" {
		o.TaskID, _ = taskID(o.TaskID)
	}
	r := Result{OperationID: o.OperationID, Host: string(p.Host), Account: p.Account, Action: o.Action, Outcome: "ok"}
	if err := validateImageFiles(o.Images); err != nil {
		setError(&r, err)
		_ = json.NewEncoder(stdout).Encode(r)
		return 0
	}
	opCtx, cancel := context.WithTimeout(ctx, operationTimeout(o))
	defer cancel()
	setError(&r, executeLocal(opCtx, p, o, &r))
	_ = json.NewEncoder(stdout).Encode(r)
	return 0
}

func showActivity(ctx context.Context, p endpoints.Config, o Options, stdout, stderr io.Writer) int {
	log := activityLog{home: p.CodexHome}
	since := time.Now().Add(-o.Since)
	seen := map[string]bool{}
	first := true
	lastStatus := ""
	for {
		limit := o.Limit
		if o.Follow {
			limit = 0
		}
		s, err := log.read(ActivityFilter{Since: since, Task: o.ActivityTask, Action: o.ActivityAction, Outcome: o.ActivityOutcome, Limit: limit})
		if !o.Follow {
			if o.JSON {
				_ = json.NewEncoder(stdout).Encode(s)
			} else {
				fmt.Fprintf(stdout, "Task activity: %s · %d operations · %s\n", s.Status, s.Operations, s.Retention)
				for _, e := range s.Events {
					printActivity(stdout, e)
				}
			}
		} else {
			retained := map[string]bool{}
			for i, e := range s.Events {
				key := fmt.Sprintf("%s/%s/%f", e.OperationID, e.Event, e.Timestamp)
				retained[key] = true
				if seen[key] || (first && i < len(s.Events)-o.Limit) {
					continue
				}
				if o.JSON {
					_ = json.NewEncoder(stdout).Encode(e)
				} else {
					printActivity(stdout, e)
				}
			}
			seen = retained // memory is bounded by the retained log files
			first = false
			if s.Status != "ok" && s.Status != lastStatus {
				fmt.Fprintln(stderr, "Activity history is", s.Status)
			}
			lastStatus = s.Status
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !o.Follow {
			if s.Status != "ok" {
				return 1
			}
			return 0
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(time.Second):
		}
	}
}
func printActivity(w io.Writer, e Activity) {
	fmt.Fprintf(w, "%s  %s  %s  task=%s  operation=%s\n", time.UnixMilli(int64(e.Timestamp*1000)).Local().Format(time.RFC3339), e.Action, e.Outcome, e.TaskID, e.OperationID)
}
