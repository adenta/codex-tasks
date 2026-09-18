# Omarchy task popup

Optional Quickshell plugin for the codex-tasks CLI. Uses stock Omarchy controls
and theme tokens. No extra daemon, Codex modifications, or local worker.

Install the current CLI on this desktop and its configured remote accounts.
Copy this directory (excluding tests) into
`~/.config/omarchy/plugins/adenta.codex-tasks/`, then add
`{"id":"adenta.codex-tasks"}` to the existing `plugins` array in
`~/.config/omarchy/shell.json`. Preserve other settings.

Add a customizable binding to `~/.config/hypr/bindings.lua`:

```lua
o.bind("ALT + SPACE", "New remote Codex task", "omarchy-shell shell summon adenta.codex-tasks")
```

Check existing bindings before replacing one. Reload Hyprland and check errors.
The plugin can also be opened with that shell command. Uses the CLI installed at
`~/.local/bin/codex-tasks`, the local account, and remote hosts in its normal
configuration. The target picker labels the current machine `local`; it uses
that account's existing app server. Connection failures are reported when an
operation is attempted.

Enter sends and opens Codex; Alt+Enter sends in the background and closes the
modal immediately without opening Codex. Both actions have separate buttons.
Shift+Enter adds a newline; Escape closes and discards an idle prompt.
Ctrl+V pastes text or PNG/JPEG images using the existing `wl-paste` executable.
It also attaches a single pasted absolute image path, quoted path, `~/` path, or
local `file://` URL. Use **Attach image…** to browse for a PNG or JPEG on this
computer. Selected files are copied into the draft cache; originals are preserved.
Image thumbnails have removal buttons; image-only tasks are supported. Up to eight
images are allowed (10 MiB and 40 megapixels each, 40 MiB combined). Attachments stay
with the draft when changing targets, reopening during delivery, or reviewing a
failed/uncertain result. Sending is blocked while clipboard capture is pending.

Local tasks receive private file copies. Remote tasks upload via the installed
OpenSSH `sftp` client and selected SSH alias before submitting once. Both CLI
copies must support images; older remote helpers are rejected before upload or
task creation. No software is installed automatically. Missing clipboard/SFTP
tools or unsupported image formats produce an error.

Draft image files are deleted on removal, dismissal, or confirmed successful
delivery. Receiving copies are retained for accepted/uncertain delivery. Managed
files older than seven days expire on the next image capture/staging operation.
The private cache is `CODEX_HOME/codex-tasks/attachments`; it contains image bytes,
unlike launcher.ini. Reloading clears the in-memory draft; its abandoned files
are subject to that same expiry. No background cleanup service is added.
The server, Git execution choice, and cached project lists
are stored in `~/.config/codex-tasks/launcher.ini`. Prompt text is never stored.
Each fresh composer starts with no project, like the Subscription model default;
changing servers also clears the project selection. Grace is the first-run server.
A missing server requires explicit
selection; destinations are never silently substituted.

Opening uses cached project choices with a background refresh. A host change
loads that host's cache and refreshes it. No typing-triggered requests or polling.
Send & open sends once and waits up to ten seconds for readable user-message history
before requesting that Codex open the returned task UUID. URL dispatch does not
prove that the desktop loaded it. An uncertain result must be inspected before
another send; Open task never resends the prompt.

Background sending skips the history wait and notifies when input is accepted,
with an optional Open task action. This confirms delivery, not task completion.
Failed or uncertain delivery retains the prompt and attachment references in memory until you reopen the
launcher and explicitly close it or successfully retry a confirmed failure.
Uncertain submissions cannot be resent. Reopening during delivery shows the
existing submission; only one submission can be pending at a time. Shell restart
or plugin reload clears in-memory state.

Remove the binding and plugin entry, then the plugin directory, to uninstall the
popup. The CLI and skill remain independent. Remove launcher.ini to reset choices.

The picker also reads the desktop's saved remote folder inventory through
`targets`, matching only configured SSH aliases. Desktop-only folders are sent
as paths, never desktop project IDs. The server resolves or creates its own
project assignment. Git mode choices on an unclassified folder apply if it is
Git; non-Git folders run directly. Desktop state is never modified.

## Inference

The Inference selector shares the Server/Project row. Each fresh composer starts
with Subscription (no model/provider override). The dropdown pins Subscription,
then favorites, then remaining models alphabetically. Search matches name or ID;
star buttons save local favorites independently of the model catalog. Missing
favorites stay visible but disabled. Space toggles a favorite when navigating
results with the arrow keys.

The installed `codex-tasks` binary supplies the catalog through `models --json`;
no separate helper is required. It reads the public OpenRouter catalog and keeps
an atomic cache at `$XDG_CACHE_HOME/codex-tasks/openrouter-models.json` (default
`~/.cache/codex-tasks/openrouter-models.json`). Opening uses that cache, fetching
when it is missing or invalid; the refresh button explicitly updates it.
Failure preserves cached models and subscription access. Catalog requests time
out after 15 seconds and do not handle credentials or route inference.

The destination account must already have a configured `openrouter` provider.
Selecting a model passes its ID, provider, and advertised context size through
`codex-tasks create`. Both local and remote CLI must support provider selection;
older helpers are rejected before creation. No automatic provider fallback or
resubmission occurs. Ordinary subscription tasks and Credits are unchanged.
