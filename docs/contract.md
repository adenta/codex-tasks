# Task tools v2 contract

English is the default presentation. `--json` returns one client-side result object.
Both local and remote targets use the stock app-server API. Task and
turn status are observations, separate from the command's `outcome`.

| Field | Meaning |
| --- | --- |
| `action`, `host`, `account` | Requested command and executing endpoint (invoker for aggregate find) |
| `operation_id` | Existing activity correlation ID; not a receipt or retry key |
| `outcome` | `ok`, `found`, `ambiguous`, `not_found`, `incomplete`, `created`, `partial`, `accepted`, `started`, `completed`, `needs_attention`, `settings_updated`, `archived`, `unarchived`, `interrupted`, `failed`, or `unknown` |
| `task`, `tasks` | Original `id`, `name`, `cwd`, `projectId`, `status`; find adds owning `host`, `account`, and `archived` |
| `coverage` | Find sources: `target`, `status`, optional explanatory `detail` |
| `search_complete` | All selected source coverage completed; unavailable sources count as incomplete |
| `next_cursor` | More scanning remains. Repeat the same command, selectors, and filters with this opaque cursor |
| `items` | History items: `id`, `turn_id`, `type`, `text`; truncation adds `truncated`, `next_offset`, `continuation_cursor`; `omitted_parts` counts non-text message parts |
| `omitted_items` | Scanned non-message items omitted from this page |
| `item_found` | Present for targeted reads; false means not found in this scan, not necessarily absent from remaining pages |
| `projects` | `id`, `name`, all `roots`; `unavailable` and `reason` when none is accessible on the executing account |
| `environments`, `environment_git` | Environment discovery: filename `id`, display `name`, optional validation `error`; whether the project is Git |
| `setup_status`, `setup_exit_code`, `setup_output` | Selected environment setup: `completed`, `failed`, `timed_out`, or `unknown`; exit code when known and readable captured diagnostic tail up to 8 KiB |
| `setup_log_path` | Private invoking-computer setup log under `CODEX_HOME/codex-tasks/setup-logs`; 1 MiB capture per stream, 2 MiB file plus metadata, seven-day retention cleaned at next setup |
| `created`, `input_accepted` | Known side effects, retained if subsequent setup/observation fails |
| `turn_id`, `turn_status`, `attention` | Turn observation and UI action needed |
| `error_category`, `error` | Specific category and bounded explanation |
| `worktree`, `project_id` | Known creation artifacts, including partial failures |
| `activity_status` | Existing invoking-account log availability, independent of task outcome |

Optional empty fields may be omitted. Task metadata uses upstream camelCase;
codex-tasks result metadata uses snake_case. Clients should tolerate additional fields.
Remote connections run stock `codex app-server proxy` over the configured SSH
alias. The proxy carries raw WebSocket bytes. Only stock app-server JSON-RPC
requests cross the connection; no remote codex-tasks helper or private protocol
exists. An optional target `socket` selects proxy's `--sock`. The server reports
its Codex home during initialization. Stock `command/exec` verifies server
host/account before mutation; mismatches stop the operation.

Attachments use stock `fs/writeFile` with base64 file bytes after local validation.
`attachment_directory` reports retained destination copies. Worktree/workspace
paths are destination paths; setup logs and activity belong to the invoking
computer. `_clipboard-image`, `_import-image`, and `_discard-images` remain local
launcher helpers and do not require a running server.

Find coverage statuses are `complete`, `more`, and `unavailable`. Coverage describes
only what the running server exposes. `found` does not imply complete coverage;
`not_found` means the selected server search completed, not that no hidden task
exists. Exact IDs use `thread/read`; archive membership is derived from its
server-reported rollout path when present, otherwise explicit archive filters
are resolved through `thread/list`. General search scans active and archived
`thread/list` pages with client-side title/preview matching. A source scans at
most 1000 entries per call. Limits remain per source (default 20, maximum 100).
Pagination uses upstream opaque cursors and is a bounded observation, not a
snapshot. Old database-search cursors are rejected: restart without `--cursor`.
No task database or rollout is read directly, even for local targets.

Read scans at most 200 upstream items per call, one item per upstream page, so
filtered messages and plans count accurately against the result limit. History
continuations are bound to task ID, turn filter, and diagnostic-output filter.
Offsets count Unicode characters in sanitized text. Use the item's continuation
cursor for long-item reads, not the page's next cursor. Diagnostic items count
against the same limit and character bound; reasoning stays omitted.

Failure categories include `route_unavailable`, `unsupported_operation`,
`transport_unavailable`, `destination_mismatch`,
`server_rejected`, `transport_uncertain`, `observation_unavailable`, and
`operation_failed`, `invalid_image`, `image_upload_failed`, and `environment_setup_failed`. Remote stderr is discarded; errors describe the connection or stock RPC rejection. Exit 0 means the command returned successfully
(including incomplete discovery); 1 means failed/partial operation; 2 means
invalid arguments; 3 means an uncertain mutation. Inspect side-effect flags and
known IDs before retrying. No mutation is replayed automatically.

## Public model catalog

`models --json [--refresh]` is a local catalog command and does not use the task
result envelope or remote dispatch. Successful results contain `models` (each
with `id`, `name`, and positive `context_length`), an RFC 3339 `refreshed_at`, and
an optional `warning`. Models are deduplicated and sorted by ID; missing names
use the ID. Entries without an ID or positive context length are skipped.

The catalog uses the public [OpenRouter models API](https://openrouter.ai/docs/api/api-reference/models/get-models).
Requests have a 15-second timeout and responses/cache reads are limited to 16 MiB.
A valid cache is reused until an explicit refresh. Missing or invalid caches
trigger fetching. Refresh failure returns valid cached data with a warning and
exit 0; failure without usable data returns `{"error":"..."}` and exit 1.
A cache write failure returns fetched data with a warning. Invalid arguments
produce stderr diagnostics and exit 2. No task or inference request is made.

## Examples

```sh
codex-tasks find --query 'Review permissions'
codex-tasks find --query 'codex://threads/UUID' --target love/agent --json
codex-tasks read UUID --target love/agent --limit 20
codex-tasks read UUID --target love/agent --item ITEM --offset 4000 --cursor CURSOR
codex-tasks read UUID --target love/agent --include-outputs --max-chars 8000
```

Illustrative English discovery result:

```text
Found 1 matching task(s) on this page.

Review permissions
Task ID: …
Target: love/agent
Project ID: …
Workspace: /home/agent/workspaces/example

love/agent: search complete.
xps/andre: could not search.
Connection refused.

Search coverage is incomplete. Missing results do not establish that a task does not exist.
```

## Validation boundary

Deterministic workflow/regression tests use fake RPC, fake SSH, temporary state,
and a disposable stock Codex runtime with a mock model. They cover duplicate
locations, archives, pagination and query binding, incomplete coverage, default
message/plan history, sparse scans, long-item continuation, bounded diagnostics,
route/account rejection before mutation, partial creation, uncertain delivery,
active-turn races, approvals, and unavailable project roots.

The packaged skill is reviewed against those scenarios and its command examples
are parsed during tests. These checks do not establish model compliance with
instructions; model-backed skill evaluations require separate authorization.
Native desktop tools are outside the fake-SSH and codex-tasks activity coverage.
