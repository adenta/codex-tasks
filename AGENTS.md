# Working on codex-tasks

This is the standalone task CLI and optional skill. Preserve direct shell usage
and complete offline help. Native helpers are preferred by the skill; this CLI
fills gaps when they are unavailable, broken, or insufficient.

Do not add server lifecycle management, fleet management,
authentication setup or provider/billing infrastructure. Keep remote SSH aliases
explicit, verify host/account identity before mutation, and never replay an
uncertain mutation automatically. Task state/history is read-only outside the
existing app-server RPC interface.

The repository-owned Ansible playbook may install the versioned executable and
matching skill to the explicit personal caller inventory. Keep that deployment
narrow: it must not configure SSH, authentication, app-server lifecycle, endpoint
configuration, or task state, and it must verify target identity before mutation.

Run `go test ./...`, `go vet ./...`, and the stock lifecycle test using
`CODEX_TASKS_TEST_CODEX` before release. Use an isolated Codex home, fake credentials
and a mock model; no live inference or task mutation in tests. Use existing managed
toolchains. Implementation/publishing does not authorize deployment.
