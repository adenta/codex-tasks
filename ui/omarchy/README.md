# Omarchy task popup

Optional Quickshell plugin for the codex-tasks CLI. Uses stock Omarchy controls
and theme tokens. No extra daemon, Codex modifications, or local worker.

Install the current CLI on this desktop. Remote accounts need only stock Codex.
The modal calls the local CLI, which connects directly to the stock app server
through SSH and `codex app-server proxy`.
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

Attachments are uploaded through stock Codex file operations before submitting
once. No SFTP or remote codex-tasks installation is required. No software is
installed automatically. Missing clipboard tools or unsupported images produce
an error.

Draft image files are deleted on removal, dismissal, or confirmed successful
delivery. Receiving copies are retained for accepted/uncertain delivery. Managed
files older than seven days expire on the next image capture/staging operation.
The private cache is `CODEX_HOME/codex-tasks/attachments`; it contains image bytes,
unlike launcher.ini. Reloading clears the in-memory draft; its abandoned files
are subject to that same expiry. No background cleanup service is added.
The server, Git execution choice, and cached project lists
are stored in `~/.config/codex-tasks/launcher.ini`. Prompt text is never stored.
Each fresh composer starts with no project, like the Server default inference choice;
changing servers also clears the project selection. Grace is the first-run server.
A missing server requires explicit
selection; destinations are never silently substituted.

For a new Git worktree, the Environment picker reads existing
`.codex/environments/*.toml` files on the selected host/project. A sole environment
is selected automatically. Multiple environments require a choice; **None** skips
setup. Explicit choices, including None, are remembered per host/project path.
Missing or invalid saved selections block sending until you choose again. Discovery
errors have a retry button. Existing checkouts, non-Git folders, and projectless
tasks do not run environment setup.

The CLI reads the selected configuration from the source checkout, then runs its
setup script through Codex's `command/exec` with Bash in the new worktree before
starting the task. Linux setup overrides are respected. Setup uses the server's
computed environment, without interactive input, and has a ten-minute limit.
It requests `dangerFullAccess` to preserve its existing account-level setup access;
server requirements may reject it. The later task's permissions are unaffected.
There is no fallback to the SSH subprocess runner. The modal shows preparation status;
background sends still close immediately and notify after delivery. A failed setup
retains the worktree and displays its path, readable diagnostic output, and a setup
log path on this desktop. Nonempty setup runs stream to private mode-0600
files under `CODEX_HOME/codex-tasks/setup-logs`. Capture is limited to 1 MiB per stream
and 2 MiB per log plus metadata. Logs older than seven days expire on the next
nonempty setup; script output is not written to the activity log. Inspect failures
before creating again; the retained submission cannot be resent from the modal.
No environment editing or action buttons are provided. Destination operations
use stock file and command methods; unsupported methods fail explicitly.

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
with Server default (no model/provider override), reflecting the selected server's
TOML configuration. Subscription is an explicit OpenAI provider choice. The dropdown
then shows favorites and remaining models alphabetically. Search matches name or
ID; stars save local favorites. Missing favorites remain visible but disabled.
Space toggles a favorite while navigating results with arrow keys.

The installed CLI supplies defaults and models through
`models --host HOST --json`. Catalog requests pass through the selected server's
loopback proxy; the desktop needs no Modal credentials. The catalog cache is scoped
by host/account. Opening reuses it; refresh explicitly updates it. Defaults are
read afresh. Changing servers clears the selection and ignores stale responses.
Failures retain cached models with a warning; catalog reads never run inference.
Modal favorites are stored separately from previous OpenRouter favorites.

The destination account must already have a configured `modal` Responses provider.
Selecting a model passes its ID, provider, and advertised context size through
`codex-tasks create` and the stock app-server API. No automatic provider fallback or
resubmission occurs. Ordinary subscription tasks and Credits are unchanged.

For Modal models, the Effort selector offers Default and advertised reasoning
levels. Default preserves destination configuration. Fresh drafts and model changes
reset it; failed submissions retain it. It uses the stock app-server interface.
