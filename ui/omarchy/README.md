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
The server, project per server, Git execution choice, and cached project lists
are stored in `~/.config/codex-tasks/launcher.ini`. Prompt text is never stored.
Grace/no project is the first-run selection. A missing server requires explicit
selection; destinations are never silently substituted.

Opening uses cached project choices with a background refresh. A host change
loads that host's cache and refreshes it. No typing-triggered requests or polling.
Send & open sends once and waits up to ten seconds for readable user-message history
before requesting that Codex open the returned task UUID. URL dispatch does not
prove that the desktop loaded it. An uncertain result must be inspected before
another send; Open task never resends the prompt.

Background sending skips the history wait and notifies when input is accepted,
with an optional Open task action. This confirms delivery, not task completion.
Failed or uncertain delivery retains the prompt in memory until you reopen the
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
