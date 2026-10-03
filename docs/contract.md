# Task tools v2 contract

English is the default presentation. `--json` returns one client-side result object.
Both local and remote targets use the stock app-server API. Task and
turn status are observations, separate from the command's `outcome`.

| Field | Meaning |
| --- | --- |
| `action`, `host`, `account` | Requested command and executing endpoint (invoker for aggregate find) |
| `operation_id` | Client-generated idempotency/correlation ID; not a receipt or retry key |
| `outcome` | `ok`, `found`, `ambiguous`, `not_found`, `incomplete`, `created`, `partial`, `accepted`, `started`, `completed`, `needs_attention`, `settings_updated`, `archived`, `unarchived`, `interrupted`, `failed`, or `unknown` |
| `task`, `tasks` | Original `id`, `name`, `cwd`, `projectId`, `status`; find adds owning `host`, `account`, and `archived` |
| `coverage` | Find sources: `target`, `status`, optional explanatory `detail` |
| `search_complete` | All selected source coverage completed; unavailable sources count as incomplete |
| `next_cursor` | More scanning remains. Repeat the same command, selectors, and filters with this opaque cursor |
| `items` | History items: `id`, `turn_id`, `type`, `text`; truncation adds `truncated`, `next_offset`, `continuation_cursor`; `omitted_parts` counts non-text message parts |
| `omitted_items` | Scanned non-message items omitted from this page |
| `item_found` | Present for targeted reads; false means not found in this scan, not necessarily absent from remaining pages |
| `projects` | `id`, `name`, all `roots`; `unavailable` and `reason` when none is accessible on the executing account |
| `created`, `input_accepted` | Known side effects, retained if a subsequent operation or observation fails |
| `turn_id`, `turn_status`, `attention` | Turn observation and UI action needed |
| `error_category`, `error` | Specific category and bounded explanation |
| `worktree`, `project_id` | Known creation artifacts, including partial failures |

Optional empty fields may be omitted. Task metadata uses upstream camelCase;
codex-tasks result metadata uses snake_case. Clients should tolerate additional fields.
Remote connections run stock `codex app-server proxy` over the configured SSH
alias. The proxy carries raw WebSocket bytes. Only stock app-server JSON-RPC
requests cross the connection; no remote codex-tasks helper or private protocol
exists. An optional target `socket` selects proxy's `--sock`. The server reports
its Codex home during initialization. Stock `command/exec` verifies server
host/account before mutation; mismatches stop the operation.

Attachments use stock `fs/writeFile` with base64 file bytes after local validation.
`attachment_directory` reports retained destination copies. Worktree paths are
destination paths.

Find coverage statuses are `complete`, `more`, and `unavailable`. Coverage describes
only what the running server exposes. `found` does not imply complete coverage;
`not_found` means the selected server search completed, not that no hidden task
exists. Exact IDs use `thread/read`; archive membership is derived from its
server-reported rollout path when present, otherwise explicit archive filters
are resolved through `thread/list`. General search scans active and archived
`thread/list` pages with client-side title and initial-preview matching. It does
not search later history. A source scans at
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
`operation_failed`, `invalid_image`, and `image_upload_failed`. Local permission
errors distinguish sandbox or OS access denial from a missing socket. Remote stderr
is discarded; errors describe the connection or stock RPC rejection. Exit 0 means the command returned successfully
(including incomplete discovery); 1 means failed/partial operation; 2 means
invalid arguments; 3 means an uncertain mutation. Inspect side-effect flags and
known IDs before retrying. No mutation is replayed automatically.

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
Native desktop tools are outside the fake-SSH test coverage.

## Timestamp discovery and batch actions (1.1)

Task `createdAt` and `updatedAt` are optional upstream Unix timestamps in seconds.
English output renders UTC RFC3339 timestamps. `find --updated-before TIMESTAMP`
accepts an RFC3339 timestamp with timezone (including fractional seconds), compares
strictly against server `updatedAt`, and excludes missing timestamps. It permits
omitting `--query`; text and time filters combine when both are present.
`--archived=false` selects unarchived tasks and `--archived=true` archived tasks;
omission searches both. The existing `--archive` option remains supported but
cannot be combined with `--archived`. Search cursors bind the normalized cutoff,
text, archive filter, and source scope. Restart older-version cursors.

`archive` and `unarchive` accept multiple positional IDs or task links on a single
selected target. `--tasks-file FILE|-` instead reads a JSON task array or one find
result's `tasks` array, with `id`, `host`, and `account` required per entry. Extra
metadata is ignored. File input cannot be combined with IDs or target selectors.
Limits are 1 MiB and 1000 distinct host/account/ID identities; duplicates collapse.
An empty array succeeds without connections. Input and configured routes are
validated before any mutation. The file represents an explicit selection, not an
instruction to fetch additional pages or reapply search filters.

Batch JSON retains the top-level result envelope and adds `results` (per-task
results with original ID, host, account, outcome/error) and `summary` counts:
`succeeded`, `failed`, `unknown`, `unattempted`. Top-level host/account identifies
the invoker; per-task host/account identifies the mutation destination. Results are
grouped by target in first-appearance order, preserving order within each target.
Single positional-task commands keep their existing output contract.

The client reuses a verified connection per target, performs actions sequentially,
and gives each task 30 seconds. Archive refuses currently running tasks. Definitive
rejections do not stop subsequent tasks. Uncertain delivery or a broken connection
stops that target; remaining entries are `unattempted`, and other targets continue.
No mutation is replayed. A failure before connection establishment leaves every
entry for that target unattempted. Cancellation leaves remaining tasks unattempted.
The server may also archive spawned descendants, as with single-task archive.

Batch exit codes: 0 all succeeded (including empty input); 1 definitive failures or
unattempted entries; 2 invalid input/configured routes before mutation; 3 any uncertain
mutation. Aggregate outcome is `ok`, `partial` (successes plus failures/unattempted),
`failed` (no successes), or `unknown` (any uncertain mutation). Inspect per-task
results before retrying; an operation ID is not a retry key.
