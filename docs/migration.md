# Migration from Codex Ops

The standalone invocation is `codex-tasks OPERATION`, replacing
`codex-ops tasks OPERATION`. The task behavior and JSON result contract are
preserved. There is no compatibility wrapper or Ops runtime dependency.

## What changed

- The fixed host/account inventory became explicit account-owned JSON configuration.
- Remote dispatch calls the same standalone binary over an existing SSH alias.
  The helper verifies actual OS host/account identity and protocol before actions.
- Root and per-command help work independently of configuration and runtime.
- Activity and newly created worktrees use the `codex-tasks` namespace.
- The optional skill describes standalone installation and native-helper fallback.
- The optional Omarchy launcher and public model catalog are included here.
  The launcher uses `codex-tasks models`; no Ops catalog helper is required.
  Service management and provider-audit code remain outside this repository.

## Cutover, when deployment is authorized

1. Build and validate the standalone executable. Copy it manually to the selected
   invoking and remote accounts' existing PATH directories; configure their routes.
2. Verify read access to the existing server on each target. Keep authentication,
   runtime installation and lifecycle under their current owner.
3. Switch command callers and the optional skill together. Account for any shared
   launcher consumers before removing the old Ops task entry/skill packaging.
4. Leave task IDs, Codex state, rollouts, projects and old worktree paths intact.
   They do not need migration. New worktrees use a different prefix only.
5. Old activity remains in `CODEX_HOME/codex-ops`. The standalone CLI starts its
   own activity history; it does not silently merge or read the old logs. If that
   history is needed, plan a one-time transfer while neither CLI writes it.

Recovery replaces only the standalone executable/configuration/skill with a
known-good standalone release. It does not restore an obsolete Ops service or
rewrite Codex state. Preserve only scoped recovery files needed by an unfinished
cutover and remove them after verification.

## Validation

The extraction was validated with stock Codex 0.154.0 using an existing managed
executable copied into local scratch space. Its experimental schema includes
project creation, settings updates, fork cutoffs and steering preconditions used
by this client. The disposable lifecycle test covers project assignment, creation,
Plan mode, history across server restart, unloaded-task resume, fork, archive and
unarchive, sender disconnect and progress from another client. Additional tests
cover Git worktree isolation, partial creation, uncertain dispatch, identity
mismatches, pagination, bounded output, canonical state containment and WAL reads.

Before extraction, read-only checks against current Grace and Love app servers
confirmed stock 0.154.0, ChatGPT login, existing Unix sockets/proxy processes and
working project/task/paginated-history calls. No deployment or live mutation smoke
test is implied by these checks. XPS desktop-owned shell access is not added.

For protocol updates, generate the schema from the exact candidate executable:

```sh
codex app-server generate-json-schema --experimental --out /temporary/schema
```

Compare required fields and run the isolated tests before changing the supported
version. See [official app-server documentation](https://learn.chatgpt.com/docs/app-server).

## Local targets and endpoint availability

`--target local` selects the executing OS account, including for local-only
searches. `targets` now includes that account with `local: true`; the popup labels
it `local` and remembers the selection using the actual hostname.

The `native_only` setting has been removed. Before using the updated CLI, delete
that field from the top level and any target entries in your configuration.
Every remote target requires an explicit SSH alias. Remove inventory-only entries
that have no usable SSH route, or configure an existing alias for them.
Unknown configuration fields are rejected rather than silently ignored.
Endpoint availability is determined by actual index access and server connections.
