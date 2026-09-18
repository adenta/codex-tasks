package tasks

import (
	"fmt"
	"github.com/adenta/codex-tasks/internal/buildinfo"
	"io"
	"strings"
)

var commandHelp = map[string]string{
	"environments": `environments --cwd DIRECTORY
List .codex/environments/*.toml in the selected destination checkout. CWD must be
absolute. Does not run scripts or require an app-server connection. JSON includes
environment_git and environments (id filename, name, optional error). Non-Git
directories return no environments; malformed files are listed as unavailable.
Version 1 TOML is supported, including multiline setup scripts and OS overrides.
Example: codex-tasks environments --host server --cwd /path/to/repo --json`,
	"models": `models [--json] [--refresh]
List the public OpenRouter model catalog without credentials or endpoint configuration.
Uses $XDG_CACHE_HOME/codex-tasks/openrouter-models.json (default
~/.cache/codex-tasks/openrouter-models.json). A missing or invalid cache triggers
a fetch; --refresh explicitly fetches again. Requests time out after 15 seconds.
Failed refreshes return usable cached models with a warning; without a usable
cache they fail. Successful fetches replace the cache atomically.
JSON contains models (id, name, context_length), refreshed_at, and optional warning.
Without --json, prints ID, name and context tokens as tab-separated columns.
This command is local; task routing flags are not supported. It does not configure
providers or route inference. Example: codex-tasks models --json --refresh`,
	"targets": `targets
Print local and configured remote targets as JSON without a network request.
The local entry has local: true and the actual OS hostname/account.`,
	"find": `find --query TEXT [--archive all|active|archived] [--limit N] [--cursor CURSOR]
Search IDs, codex://threads/UUID links, titles and previews on configured sources.
Includes archives by default; limit 20 per source (1–100), scan bound 1000 entries.
Follow every next_cursor with unchanged query, filters and selectors. Coverage
may be incomplete even when a match is found. Reads the canonical SQLite index
with committed WAL visible; does not require a running server.
Example: codex-tasks find --query 'review permissions' --target server/agent`,
	"projects": `projects [--limit N] [--cursor CURSOR]
List saved projects and all roots on the target account. Unavailable roots are
marked explicitly. Limit defaults to 20 (1–100); follow next_cursor.
Example: codex-tasks projects --host server`,
	"list": `list [--project ID] [--archived] [--limit N] [--cursor CURSOR]
List active tasks, or archived tasks with --archived. Limit 20 (1–100).
Filter by the target's project ID. Repeat selectors/filters with next_cursor.
Example: codex-tasks list --archived --target server/agent`,
	"read": `read TASK [--turn ID] [--limit N] [--cursor CURSOR]
     [--item ID --offset N] [--max-chars N] [--include-outputs]
Read user/assistant messages and plans, preserving formatting. Reasoning omitted.
Limit 20 (1–100); max-chars 4000 (1–32000) per item. Diagnostic outputs are opt-in
and share those bounds. Offset is a Unicode character offset, default 0.
Follow next_cursor even on empty pages. For truncated items use that item's
continuation_cursor, --item and next_offset; retain task/turn/output filters.
Example: codex-tasks read TASK_UUID --limit 20 --host server`,
	"create": `create [--cwd DIRECTORY] [--project ID | --projectless]
       [--checkout | --ref REF] [--title TITLE] [--model MODEL] [--model-provider PROVIDER]
       [--model-context-window TOKENS] [--reasoning-effort VALUE] [--environment FILE.toml]
       [--mode plan|default] [--message-file FILE|-] [--image FILE ...] [--wait DURATION]
Create a task only within the user's requested scope. CWD must be absolute on
the target. Match or create project assignment; ambiguous roots require --project.
Git defaults to a detached worktree from local origin/HEAD; no guessed ref or
fetch. --ref selects a requested ref; --checkout uses the existing checkout.
Confirmed non-Git directories are used directly. --projectless omits assignment.
--environment selects an existing filename inside the checkout's .codex/environments
directory. The selected configuration is read before creation, then its setup
script runs through Codex command/exec with Bash in the new worktree before any task starts.
The destination OS setup override replaces the default script when present.
An empty script succeeds. Setup uses the Codex server-computed environment, has
no interactive stdin, and times out after 10 minutes. It requests dangerFullAccess
to preserve account-level setup access; server requirements may reject it. This
does not alter the later task's permissions. No subprocess fallback is attempted.
Omitting --environment skips setup; --checkout and non-Git directories cannot use it.
Failure retains the worktree and returns setup_status, setup_exit_code when known,
and up to 8 KiB of readable setup_output. A lost reply is unknown and never replayed.
Inspect before retrying; scripts may have side effects. setup_log_path names a
private destination file under CODEX_HOME/codex-tasks/setup-logs. Streamed output
is captured up to 1 MiB per stream and 2 MiB per log plus metadata; line breaks
are preserved. Logs older than seven days expire on the next nonempty setup.
Output is not stored in the activity log. Scripts should avoid printing secrets.
Omitted model/provider/mode preserve server defaults. Provider selection requires
--model and an already-configured provider on the execution host. Optional
--reasoning-effort selects an advertised effort for the new task. Omit it to keep
the configured default. The returned setting is verified before sending input.
--model-context-window supplies the custom model context limit. No credentials
are handled and no provider is configured by this CLI. Optional first message starts work.
Explicit OpenRouter models use MODEL@preset/codex-tasks, applied on the execution
host. An existing matching suffix is accepted; other presets are rejected.
Manage routing in that OpenRouter preset; edits affect subsequent requests from
existing preset tasks. Existing plain-model tasks and other providers are unchanged.
Preset errors never trigger a retry without the preset.
Title max 512 bytes. On partial/unknown preserve project/worktree/task IDs and
inspect before another create. Never blindly replay after connection loss.
With --projectless, omitting --cwd allocates Documents/Codex/date/task-* with work/outputs and developer instructions.
--wait-history requires text or images and waits up to 10s for readable accepted input.
Example: codex-tasks create --cwd /path/to/repo --mode plan --message-file brief.txt`,
	"fork": `fork TASK [--title TITLE] [--mode plan|default]
Fork persisted history before any unfinished running turn. Inherits checkout;
does not copy uncommitted files or create a worktree. Title max 512 bytes.
Omitted settings inherit the source. Preserve the created ID after partial setup.
Example: codex-tasks fork TASK_UUID --title 'Follow-up review'`,
	"message": `message TASK [--message-file FILE|-] [--image FILE ...] [--wait DURATION]
Send authorized input; resume unloaded tasks or steer an active turn using its
expected turn ID. Omitted model/reasoning/permissions/mode remain unchanged.
Accepted is not completed. Approval/input requests require the task's Codex UI.
After unknown delivery, inspect recent history before deciding whether to retry.
Example: printf '%s\n' 'Review the change' | codex-tasks message TASK_UUID --message-file -`,
	"progress": `progress TASK [--turn ID] [--wait DURATION]
Observe latest or specified turn. Wait defaults to 0s; at most 60s.
Use the turn ID returned by message. Completed means that turn ended, not that
the user's overall objective is complete. Surface needs_attention in Codex UI.
Example: codex-tasks progress TASK_UUID --turn TURN_UUID --wait 30s`,
	"mode": `mode TASK --mode plan|default
Change subsequent-turn mode while preserving model/reasoning/permissions.
Does not interrupt current work. May resume an unloaded task.
Example: codex-tasks mode TASK_UUID --mode plan`,
	"archive": `archive TASK
Archive through the app server. Refuses currently active tasks. The server may
also archive spawned descendants; inspect the task family before acting.
Example: codex-tasks archive TASK_UUID`,
	"unarchive": `unarchive TASK
Restore an archived task through the app server.
Example: codex-tasks unarchive TASK_UUID`,
	"activity": `activity [--since DURATION] [--task ID] [--action NAME] [--outcome VALUE]
         [--limit N] [--follow]
Inspect activity on the invoking account; remote selectors are not supported.
Defaults: since 24h, limit 20 (1–100). --follow streams new events until Ctrl-C;
with --json it emits JSON Lines. Two 5 MiB files bound retention, not time.
Stored under CODEX_HOME/codex-tasks. Contains identifiers/outcomes, not messages.
Covers this CLI only; logging failure does not imply a failed task operation.
Example: codex-tasks activity --outcome unknown --since 24h`,
	"config": `Configuration (no configuration changes are made by this help command)
Read $CODEX_TASKS_CONFIG, otherwise $XDG_CONFIG_HOME/codex-tasks/config.json
(default ~/.config/codex-tasks/config.json). Missing default config uses local
account only. Identity comes from the OS hostname and username.
Example configuration on your desktop:
{
  "targets": [{"host":"server","account":"agent","ssh_alias":"my-server"}]
}
Use the real remote hostname/account and an existing SSH alias. One account per
host. Install the same codex-tasks binary on PATH on selected remote accounts.
Optional local "codex_home" and "socket" are absolute paths. CODEX_HOME overrides
codex_home; otherwise ~/.codex. Default socket is
CODEX_HOME/app-server-control/app-server-control.sock. Remote paths are resolved
by the remote helper's own configuration, never copied from the caller.
No SSH configuration, login, service or app-server lifecycle changes are made.
For connection failures verify the alias/account and running server with its
owner. The CLI never starts a replacement server. Incompatible helpers must be
updated manually. No task recreation or automatic mutation retry is attempted.`,
}

const commonHelp = `
Common options: --target local|HOST/ACCOUNT or --host HOST (mutually exclusive),
--json (one result object), --source-task UUID (default CODEX_THREAD_ID).
TASK accepts a UUID or codex://threads/UUID. Put flags after TASK, or TASK after
all flags. With no selector commands use the local account; find searches all
configured sources. --target local restricts find to the current account.
Remote selectors do not apply to activity or models.
Messages: --message-file FILE or - for stdin, maximum 1 MiB. Shell-quote text;
prefer a file/stdin for multiline messages. --wait defaults to 0s, maximum 60s.
Images: create/message accept repeatable --image FILE, always a caller-local path,
including for remote tasks. Text is optional with images. PNG/JPEG only, at most
8 images, 10 MiB and 40 megapixels each, 40 MiB combined. Remote images use the
existing SSH alias and installed sftp client; all uploads finish before submission.
Remote helpers must advertise image support; no automatic update or retry occurs.
Private copies under CODEX_HOME/codex-tasks/attachments are retained on acceptance
or uncertain delivery; files older than 7 days are removed on the next staging or
clipboard capture. Caller-owned source files are never deleted. Image operations
allow up to 2 extra minutes for staging/transfer. History text omits image content.
Runtime actions attach to an existing account-owned Unix app-server socket.
Find, environments, activity, targets and models can operate without a running server. Native tools remain
necessary for desktop-only tasks and handoff; the optional skill prefers native
helpers and uses this CLI when those helpers are unavailable or insufficient.
JSON metadata and outcomes: see docs/contract.md. Unknown/partial results retain
known side effects. Operation IDs are correlation IDs, not retry keys.
Exit: 0 result returned (may include incomplete discovery), 1 failed/partial,
2 invalid input/config, 3 uncertain mutation. Never replay a mutation blindly.
Use 'codex-tasks help config' for endpoint setup and troubleshooting.
`

// Help works before reading configuration or connecting to a runtime.
func Help(args []string, w io.Writer) (int, bool) {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(w, buildinfo.BuildID)
		return 0, true
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || (args[0] == "help" && len(args) == 1) {
		fmt.Fprint(w, help, commonHelp)
		return 0, true
	}
	cmd := args[0]
	requested := false
	if cmd == "help" {
		if len(args) != 2 {
			fmt.Fprintln(w, "Usage: codex-tasks help COMMAND")
			return 2, true
		}
		cmd = args[1]
		requested = true
	} else {
		for i := 1; i < len(args); i++ {
			a := args[i]
			if a == "--help" || a == "-h" {
				requested = true
				break
			}
			if strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
				switch a {
				case "--refresh", "--wait-history", "--json", "--checkout", "--projectless", "--archived", "--include-outputs", "--follow":
				default:
					i++
				}
			}
		}
	}
	if !requested {
		return 0, false
	}
	text, ok := commandHelp[cmd]
	if !ok {
		fmt.Fprintf(w, "Unknown command %q\n", clean(cmd, 80))
		return 2, true
	}
	fmt.Fprintln(w, "Usage: codex-tasks "+text)
	if cmd != "config" && cmd != "models" {
		fmt.Fprint(w, commonHelp)
	}
	return 0, true
}
