# Version 1.0.0 cutover

The CLI connects directly to an existing stock Codex app server: locally by Unix
socket, remotely through SSH and stock `codex app-server proxy`. The proxy forwards
raw WebSocket traffic. Version 1.0.0 removes the former panel backend and defines
the first stable command and JSON contract.

## Migration

1. Build and validate the client, then update the invoking computer's binary and
   optional skill only when deployment is separately authorized.
2. Preserve existing target host/account/SSH aliases. A target may optionally set
   an absolute `socket`; otherwise stock proxy uses Codex's default socket.
3. Verify stock `codex` is on the remote account's PATH and its server is already
   running. The CLI does not start or configure servers or authentication.
4. Remote codex-tasks binaries are unused. They are not uninstalled automatically.
   There is no private-protocol compatibility mode or fallback.
5. Discard old find cursors. Search now requires the server and covers only its
   exposed task list/read interface. Direct SQLite/WAL reads are removed.

Task IDs, history, projects, worktrees and retained attachments need no migration.
Existing model caches, setup logs, and activity logs are left untouched but become
inert; version 1.0.0 neither reads nor deletes them. Every new task now requires an
explicit absolute `--cwd`, including `--projectless` tasks.

Recovery means restoring the previous client binary if necessary; it does not
rewrite Codex task state or replay an uncertain operation. Existing remote helper
binaries may be retained until deployment verification completes. This repository
change itself does not deploy, uninstall or restart anything.

## Validation

Use the existing managed Codex 0.154.0 binary with isolated homes, fake credentials
and a local mock model. The stock lifecycle test exercises task operations and
remote creation through the actual stock proxy with no remote codex-tasks binary.
Run `go test ./...`, `go vet ./...`, the lifecycle test with `CODEX_TASKS_TEST_CODEX`,
and validate the packaged tree. Identity mismatches, unavailable servers, rejected
methods/uploads and uncertain submissions must stop without automatic replay.

Generate schemas from the exact candidate stock executable when updating support:

```sh
codex app-server generate-json-schema --experimental --out /temporary/schema
```

The CLI was originally extracted from codex-ops; the standalone invocation remains
`codex-tasks OPERATION`. No Ops service, fixed fleet inventory, authentication
setup or app-server lifecycle management is introduced by this cutover.
