# Changes from upstream

Let's Build Clauder forks [codex-micro-for-claude-code](https://github.com/dustinwdev/codex-micro-for-claude-code) 0.7.0 by dustinwdev. What it adds:

- **Claude desktop app sessions.** The Code tab's sessions attach too; agent keys bring the app forward and open the chat.
- **Mic key tap and hold.** `{ hold = "...", tap = "..." }`: hold mirrors a chord for push-to-talk, a quick tap presses another, for Wispr Flow's two shortcuts.
- **Double-tap send.** `send = { double = "enter" }` presses Enter on a double tap; one tap does nothing.
- **Pins and manual keys.** Hold the logo key and press an agent key to pin the chat you last typed in. Pins persist in `pins.json`. `[agents] manual = true` leaves keys to pinned chats only.
- **The dial follows the sidebar.** `chat-next` and `chat-prev` read the app's own chat files and step the sidebar in its order, opening chats that aren't running (`internal/daemon/sidebar.go`). `[agents] hide` skips groups.
- **Model and effort on the pad.** `model-next`, `model-prev` step Opus, Fable, Sonnet; effort steps are available on any control.
- **LaunchAgent daemon.** The daemon runs from a fixed, signed binary under a LaunchAgent so macOS permission grants survive rebuilds. Sessions ask launchd to start it and never spawn their own copy. `scripts/install-macos.sh` sets it up.
- **Launcher.** `bin/lets-build-clauder` runs `build/` or the installed daemon binary; it no longer downloads releases.
- **Renamed** throughout: plugin, command, skill, binary, config and state paths, LaunchAgent label, module path.
