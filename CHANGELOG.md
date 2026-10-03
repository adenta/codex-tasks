# Changelog

This project follows Semantic Versioning. Incompatible changes to the documented
CLI or JSON result contract require a major release; backward-compatible additions
require a minor release; compatible fixes require a patch release.

## 1.1.0

- Added `find --archived=true|false` and `--updated-before` with RFC3339 timestamps,
  including searches without a text query. Task results expose creation/update times.
- Added batch archive and restore with multiple IDs or explicit-target JSON file/stdin
  input, connection reuse, per-task results, and no automatic mutation retries.
- Search continuation cursors from earlier versions must be restarted.

## 1.0.0

The first stable contract narrows `codex-tasks` to headless task operations.

Breaking changes from the unversioned development builds:

- Removed the `models`, `environments`, and `activity` commands.
- Removed `--refresh`, `--environment`, `--wait-history`, `--source-task`, and
  the activity-only filter flags.
- Removed catalog, environment-setup, generated-workspace, history-readiness,
  and activity-status fields from JSON results.
- Removed `desktop_projects` from `targets` output.
- Required an explicit absolute `--cwd` for every `create`, including
  `--projectless`.

Previously written model caches, setup logs, and activity logs are inert and are
not deleted automatically.
