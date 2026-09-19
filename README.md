# codex-tasks

A standalone shell utility for Codex task management. Use it when native task
helpers are unavailable, broken, or do not expose enough history or control.
Native helpers remain the first choice when they can do the job.

Find and read tasks, create projects and worktrees, fork, send messages, follow
progress, switch modes, and archive or restore tasks. The optional agent skill
uses the same executable. Neither a skill nor a repository checkout is needed
to use an installed binary.

## Requirements and scope

- Linux, with access to an existing stock Codex app server's Unix socket.
- Tested against Codex **0.154.0**. Experimental protocol methods are used;
  compatibility with other versions is not guaranteed.
- Git and standard Linux utilities for workspace creation; OpenSSH on the client
  and stock `codex` on the remote account's PATH. Configure existing SSH aliases.
- Existing Codex authentication belongs to the server. No new API key is needed.

This utility never starts or manages an app server, changes login or SSH
configuration, or writes the task index/history directly. Desktop-only task
access and cross-host handoff still need native helpers. All task access, including
`find`, uses the running stock app server; there are no direct database reads.

The connection is **modal → local codex-tasks → stock Codex app server**. Remote
targets use `ssh <alias> codex app-server proxy` to carry the stock WebSocket
connection. No remote codex-tasks installation or private protocol is required.

## Build and install manually

Using your existing Go 1.23+ toolchain:

```sh
git clone https://github.com/adenta/codex-tasks.git
cd codex-tasks
go build -trimpath -o build/codex-tasks ./cmd/codex-tasks
./build/codex-tasks --help
```

When ready to install, copy `build/codex-tasks` into an existing user bin directory
on PATH on the computer invoking commands (the desktop for the modal). Remote
servers need stock Codex, not another copy of codex-tasks. Nothing installs
automatically; no service or fleet tooling is included.
`make dist` builds a local Linux archive and checksum without publishing it.

Optionally copy `skills/codex-tasks` into your Codex skills directory. The skill
prefers native helpers and falls back to this utility for missing capabilities.
Updating or removing the skill has no effect on the executable.

## Configure existing endpoints

Local commands use the current OS hostname/account and `$CODEX_HOME`, defaulting
to `~/.codex`. Use `--target local` to explicitly select this account, including
to restrict `find` to local tasks. For remote access create
`~/.config/codex-tasks/config.json` (or use `$CODEX_TASKS_CONFIG`):

```json
{
  "targets": [
    {"host": "server", "account": "agent", "ssh_alias": "my-server"}
  ]
}
```

`server` is the actual remote hostname, `agent` its OS account, and `my-server`
an existing SSH alias. Configure
one destination account per host. Server host/account identity is checked through
stock `command/exec` before destination mutations.
No named hosts are built in.

The server reports its Codex home during initialization. Optional local
`codex_home` and `socket` fields accept absolute paths. `$CODEX_HOME` overrides
`codex_home`; the default socket is
`CODEX_HOME/app-server-control/app-server-control.sock`. This is an attachment
point for the existing server, not a socket owned by this utility. A target can
optionally set an absolute `"socket"` path, passed to stock proxy's `--sock`;
otherwise the proxy uses Codex's default socket on that account.

```sh
codex-tasks help config
codex-tasks find --query 'review permissions'
codex-tasks projects --target server/agent
codex-tasks read TASK_UUID --host server --limit 20
codex-tasks create --host server --cwd /absolute/repo --mode plan --message-file brief.txt
codex-tasks environments --host server --cwd /absolute/repo --json
codex-tasks create --host server --cwd /absolute/repo --environment environment.toml --message-file brief.txt
codex-tasks message TASK_UUID --host server --message-file follow-up.txt
codex-tasks progress TASK_UUID --host server --wait 30s
codex-tasks fork TASK_UUID --host server --title 'Follow-up review'
codex-tasks mode TASK_UUID --host server --mode plan
codex-tasks archive TASK_UUID --host server
codex-tasks unarchive TASK_UUID --host server
```

Every command supports `--help` and `-h`; `help COMMAND` works too. Help and version
work without configuration, Codex, SSH, or network access. Use `--json` for scripts.

### Image attachments

`create` and `message` accept repeatable `--image FILE` arguments, with or without
text. Image paths are on the machine running the CLI, even for remote tasks:

```sh
codex-tasks create --target local --projectless --image screenshot.png
codex-tasks create --host server --cwd /absolute/repo --message-file brief.txt --image screenshot.png
```

PNG and JPEG are supported: at most eight images, 10 MiB and 40 megapixels per
image, and 40 MiB combined. The CLI snapshots caller files privately, verifies the
server identity, and uploads through stock `fs/writeFile` (base64 file content).
All uploads finish before a task is created or messaged. No SFTP or remote helper
is needed. No task submission is automatically retried.

Copies live under `CODEX_HOME/codex-tasks/attachments` on the receiving account.
Accepted and uncertain submissions retain their files; entries older than seven
days are removed on subsequent attachment staging or clipboard capture, not by a
background service. Known pre-submission failures clean up their copies when the
destination is reachable. Caller-owned source files are never removed. Inspection
of an uncertain task must precede any retry. CLI history text omits image content.

## Results and recovery

- Follow discovery and history cursors. An incomplete search is not proof of absence.
  Search covers only tasks exposed by stock `thread/list` and exact `thread/read`.
  Old database-search cursors must be discarded; restart without `--cursor`.
- Activity and setup logs stay on the invoking computer. `setup_log_path` is local;
  `worktree`, `workspace`, and `attachment_directory` identify destination paths.
- Git task creation defaults to an isolated detached worktree from local
  `origin/HEAD`; use `--ref` for a selected ref or `--checkout` to use the checkout.
  Before starting the task, creation copies an ignored root `AGENTS.override.md`
  from the source checkout into the new worktree. Missing files and source
  symlinks are skipped; existing destination files are never overwritten. Copy
  failures stop creation and report the retained worktree for inspection. Other
  ignored files and local environment setup scripts are not processed.
  Forks inherit the source checkout; they do not copy uncommitted files.
- `accepted` is input acceptance; `completed` is turn completion. Neither proves
  that the user's overall task is finished. Approvals and input requests stay in
  the task's Codex UI.
- On `unknown` or `partial`, preserve returned task/project/worktree IDs and
  inspect history before retrying. Mutations are never automatically replayed.
- Activity under `CODEX_HOME/codex-tasks` contains IDs/outcomes, not message bodies,
  and is capped at two 5 MiB files. Logging warnings are not a reason to retry.

Exit codes: **0** result returned (including incomplete discovery), **1** failed
or partial operation, **2** invalid arguments/configuration, **3** uncertain
mutation. See [the full result contract](docs/contract.md).

## Development

```sh
go test ./...
go vet ./...
CODEX_TASKS_TEST_CODEX=/absolute/path/to/codex go test ./... -count=1
```

The last command is required before release. It runs a disposable server with an
isolated home, fake credentials and a local mock model. No live task state or real
inference is used. Without the environment variable, the stock-runtime test skips.
CI supplies Codex 0.154.0 explicitly and checks its version.

See [migration and validation notes](docs/migration.md). Extracted from
[adenta/codex-ops](https://github.com/adenta/codex-ops), snapshot
`ef47c97e74086501019f76671e140c9a5abaf3fa`.

## Modal inference

New tasks selected from the launcher use the configured `modal` provider and
Modal's endpoint hostname as the model ID. No routing preset is appended.
Subscription continues to use the destination's current Codex defaults.
The model and provider remain explicit when forking a task.

Configure Modal on each execution host with Responses over HTTPS:

```toml
[model_providers.modal]
name = "Modal"
base_url = "https://inference.us-west.modal.direct/v1"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
env_key = "MODAL_PROXY_TOKEN"
```

Set `MODAL_PROXY_TOKEN` to the combined proxy token `wk-<id>.ws-<secret>`
in the app-server environment, or use Codex's provider auth command backed by
an existing credential store. API tokens (ak-/as-) are not inference credentials.
Create Shared Endpoints separately in Modal; the CLI does not provision models,
manage credentials, or configure billing. Existing OpenRouter task history is
not migrated or silently rerouted.

## Optional Omarchy popup

The authenticated Modal workspace catalog is built into `codex-tasks models --json`.
Use `--refresh` to update it explicitly; otherwise a valid local cache is reused.
Catalog refresh reads MODAL_PROXY_TOKEN, or uses the desktop keyring via
`secret-tool lookup application codex-tasks provider modal`. Cached reads and
help work offline. Only endpoints available to that credential are listed.
See `codex-tasks help models` for cache location and failure behavior.

See [ui/omarchy](ui/omarchy/README.md) for a themed remote task composer and an
Alt+Space binding. The CLI remains independent of Quickshell and Omarchy.
`targets` prints the local account and configured remote destinations as JSON
without network access. The local entry includes `local: true` with its actual
hostname/account, and appears as `local` in the popup's target picker.
`create --projectless` can omit `--cwd` to allocate a unique workspace under the
executing account's Documents/Codex, with work/outputs and developer instructions.
`create --wait-history --message-file -` waits up to ten seconds for readable
accepted input before returning `history_ready: true`; this does not wait for
inference completion.

Modal models with advertised reasoning levels expose an Effort selector.
Default preserves the server's configured behavior. `create --reasoning-effort`
sets and verifies the selected value through the stock app-server interface.
