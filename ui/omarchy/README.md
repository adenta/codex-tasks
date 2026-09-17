# Omarchy remote task popup

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
`~/.local/bin/codex-tasks` and remote hosts in its normal configuration.

Enter sends; Shift+Enter adds a newline; Escape closes and discards the prompt.
The server, project per server, Git execution choice, and cached project lists
are stored in `~/.config/codex-tasks/launcher.ini`. Prompt text is never stored.
Grace/no project is the first-run selection. A missing server requires explicit
selection; destinations are never silently substituted.

Opening uses cached project choices with a background refresh. A host change
loads that host's cache and refreshes it. No typing-triggered requests or polling.
Creation sends once and waits up to ten seconds for readable user-message history
before requesting that Codex open the returned task UUID. URL dispatch does not
prove that the desktop loaded it. An uncertain result must be inspected before
another send; Open task never resends the prompt.

Remove the binding and plugin entry, then the plugin directory, to uninstall the
popup. The CLI and skill remain independent. Remove launcher.ini to reset choices.
