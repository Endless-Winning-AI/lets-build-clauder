# Let's Build Clauder

**Let's Build Clauder** makes the Codex Micro macropad (and the Work Louder Creator Micro 2, which runs the same firmware) work with the **Claude desktop app** and **Claude Code**. The name is a play on the pad's maker, Work Louder: Claude + Louder.

Each chat gets one of the pad's six agent keys. The key's color shows what the chat is doing, and pressing it brings that chat forward. The mic key drives a dictation app such as Wispr Flow, the dial walks the app's sidebar, the joystick changes model and effort, and every control can be remapped.

Let's Build Clauder is a fork of [codex-micro-for-claude-code](https://github.com/dustinwdev/codex-micro-for-claude-code) by dustinwdev, changed to work in the desktop app, not only in terminals. It is an independent project, not affiliated with OpenAI, Work Louder or Anthropic.

## Requirements

- Claude Code **2.1.303** or newer (`claude --version`), or the Claude desktop app's Code tab.
- macOS (Linux works for terminal sessions; the desktop-app features are macOS only).
- A Codex Micro or Creator Micro 2 over Bluetooth or USB-C. Quit the ChatGPT app if it's running: only one app can use the pad at a time.
- Go (`brew install go`) to build.

Terminals the plugin can drive (switch windows, answer prompts): Ghostty, iTerm2, Terminal (switching only), any terminal under Hyprland on Linux, and sessions inside tmux, WezTerm or kitty with remote control on.

## Setup (macOS)

1. **Get the code and build the daemon.**

   ```sh
   git clone https://github.com/Endless-Winning-AI/lets-build-clauder.git
   cd lets-build-clauder
   scripts/install-macos.sh
   ```

   The script builds `lets-build-clauder`, signs it, copies it to `~/Library/Application Support/lets-build-clauder/lets-build-clauder`, writes the LaunchAgent `~/Library/LaunchAgents/ai.endlesswinning.lets-build-clauder.plist` and loads it. The daemon then starts at login and keeps running.

   Signing matters: macOS ties its permission grants to the signed program, so a stable signing identity lets you rebuild without re-granting. The script uses the first "Apple Development" identity in your keychain; pass `--identity "Apple Development: you@example.com (TEAMID)"` to pick one. With no identity it signs ad-hoc and you must re-grant after each rebuild.

2. **Allow the daemon.** Under System Settings > Privacy & Security, add the installed binary (press `+`, then Cmd-Shift-G and paste the path) to both:

   - **Input Monitoring**: to read the pad.
   - **Accessibility**: to hold and press keys for dictation and double-tap Enter.

   Then restart it: `launchctl kickstart -k gui/$(id -u)/ai.endlesswinning.lets-build-clauder`.

   If the pad is also used by the ChatGPT app, switch **ChatGPT** off under Input Monitoring.

3. **Install the plugin** from the checkout:

   ```sh
   claude plugin marketplace add /path/to/lets-build-clauder
   claude plugin install lets-build-clauder@lets-build-clauder
   ```

4. Start a new session and run `/lets-build-clauder`. It checks everything and prints the fix for anything missing. The daemon's own report is `"$HOME/Library/Application Support/lets-build-clauder/lets-build-clauder" status`: you want `device.connected: true` and `canHold: true`.

Sessions never start the daemon themselves while the LaunchAgent exists; they only ask launchd to start it. A copy started from a session would not have the permission grants.

### Linux

`go build -o build/lets-build-clauder ./cmd/lets-build-clauder`, install the plugin as above, and run `/lets-build-clauder`; it prints the udev rule (`udev/70-lets-build-clauder.rules`) if the pad can't be opened. Holding keys for dictation is macOS only for now.

## The controls

Agent key colors:

| Color | Meaning |
| --- | --- |
| White | idle |
| Blue | working |
| Amber, pulsing | waiting for you to approve something |
| Green | finished; clears when you press its key or type in the chat |
| Red | the last turn ended in an error |
| Off | no chat |

Press an agent key to bring its chat forward. By default a new chat takes the first free key. With `[agents] manual = true`, only pinned chats get keys and keys never move on their own: hold the send key (the logo key) and press an agent key to pin the chat you last typed in. Pins survive restarts. A key whose pinned chat is closed stays dark, and pressing it reopens the chat.

Default actions for the other controls:

| Control | Action |
| --- | --- |
| Lightning | toggle fast mode |
| Check | Allow the permission prompt waiting on you; with none, Enter if the Claude app is in front |
| Cross | deny it; with none waiting, stop the running turn |
| Branch | fork the conversation into a new terminal window |
| Microphone | toggle Claude Code's voice mode |
| Logo key | send what is typed in the prompt |
| Dial | turn to step reasoning effort, press to open `/effort` |
| Joystick | up: cycle permission modes; down: go to the chat that needs you; left and right: previous and next chat |

Approve and deny go to the chat waiting on you, the focused one first. Everything else acts on the chat whose window has focus, or the one you last typed in.

In terminal sessions, each shows its key and the pad's battery under the prompt, such as `key 2 · 🔋61% · 1 waiting`.

### A desktop-app layout

The layout the fork was built for. Put it in `~/.config/lets-build-clauder/config.toml`:

```toml
[keys]
fast = "/new"                      # new chat
approve = "approve"                # Allow; with nothing waiting, go where you're needed
deny = "interrupt"                 # stop the reply
branch = "model-next"              # Opus -> Fable -> Sonnet
mic = { hold = "cmd+ctrl+space", tap = "ctrl+space" }   # Wispr Flow: hold to talk, tap to toggle
send = { double = "enter" }        # double-tap sends; one tap does nothing

[dial]
left = "chat-prev"                 # walk the sidebar, from the chat on screen
right = "chat-next"

[stick]
left = "model-prev"
right = "model-next"
up = "effort-up"
down = "effort-down"

[agents]
manual = true                      # keys only for pinned chats
hide = ["Archive"]                 # sidebar groups the dial skips
```

## Dictation

Have the mic key hold your dictation app's shortcut for as long as you hold the key:

```toml
[keys]
mic = { hold = "fn" }
```

`fn` is Wispr Flow's default; use a chord such as `"right-alt"` or `"ctrl+space"` for other apps. An app with a hands-free toggle too can have both: `mic = { hold = "cmd+ctrl+space", tap = "ctrl+space" }` holds the first and, on a quick tap, presses the second. Or ask Claude to *"set up my mic key for Wispr Flow"*; the bundled `lets-build-clauder-config` skill does it. Holding keys needs the daemon allowed under Accessibility; `/lets-build-clauder` checks it.

## Config options

The config file is `~/.config/lets-build-clauder/config.toml` (or `$XDG_CONFIG_HOME/lets-build-clauder/config.toml`). It's optional; anything left out keeps its default. Changes apply on the next press. [`config.example.toml`](config.example.toml) lists everything with comments.

| Section | Keys |
| --- | --- |
| `[keys]` | `fast`, `approve`, `deny`, `branch`, `mic`, `send` |
| `[dial]` | `left`, `right`, `press`, `hold` (half a second or longer) |
| `[stick]` | `up`, `down`, `left`, `right` |
| `[agents]` | `manual` (keys only for pinned chats), `hide` (sidebar groups `chat-next`/`chat-prev` skip) |
| `[terminal]` | `command` the `fork` action opens a new terminal with |

Each control takes one of:

| Value | Effect |
| --- | --- |
| `"approve"`, `"deny"`, `"interrupt"`, `"submit"` | answer the prompt, stop the turn, send the prompt box |
| `"fast"`, `"voice"`, `"fork"` | toggle fast mode, voice mode; fork into a new window |
| `"effort-up"`, `"effort-down"` | step effort through low, medium, high, xhigh, max |
| `"cycle-mode"` | cycle permission modes, as Shift+Tab does |
| `"attention"`, `"next-session"`, `"prev-session"` | switch chats |
| `"chat-next"`, `"chat-prev"` | step the desktop app's sidebar from the chat on screen, opening chats that aren't running |
| `"model-next"`, `"model-prev"` | step the model through Opus, Fable, Sonnet |
| `"/command args"` | run a slash command |
| `{ text = "...", submit = true }` | type into the prompt box, optionally send |
| `{ keys = "ctrl+r" }` | press a chord in the session's terminal |
| `{ hold = "fn" }`, `{ hold = "...", tap = "..." }` | hold a key while the pad key is held; `tap` on a quick press (`[keys]`, macOS) |
| `{ double = "enter" }` | press a key on a double tap within 0.4 s (`send` only, macOS) |
| `{ run = ["program", "arg"] }`, `{ run = "shell command" }` | run something |
| `"none"` | nothing |

Key names: letters, digits, `escape`, `tab`, `enter`, `space`, `backspace`, `f1` to `f20`, the modifiers `ctrl`, `shift`, `alt`, `cmd`, `fn`, and `right-` forms such as `right-alt`. Join a chord with `+`.

## Where things live (macOS)

| What | Path |
| --- | --- |
| Daemon binary | `~/Library/Application Support/lets-build-clauder/lets-build-clauder` |
| LaunchAgent | `~/Library/LaunchAgents/ai.endlesswinning.lets-build-clauder.plist` |
| Config | `~/.config/lets-build-clauder/config.toml` |
| Pins | `~/Library/Application Support/lets-build-clauder/pins.json` |
| Log | `~/Library/Logs/lets-build-clauder/daemon.log` |
| Socket | `$TMPDIR/lets-build-clauder-<uid>/daemon.sock` |

On Linux the log and pins are under `~/.local/state/lets-build-clauder/`.

## How it works

One daemon owns the pad and talks to it over HID. Each session runs `bin/lets-build-clauder attach`, which holds the session's place with the daemon for the life of the session, and the plugin's hook module (`hooks/register.ts`) reports the session's state over a local socket. The daemon reads the desktop app's own chat files to follow the sidebar order (`internal/daemon/sidebar.go`). Nothing is written to the pad's memory; its keymap stays as it is.

## Troubleshooting

- **`status` says macOS blocked access**: the daemon binary isn't allowed under Input Monitoring (for the pad) or Accessibility (for held keys). Add it, then `launchctl kickstart -k gui/$(id -u)/ai.endlesswinning.lets-build-clauder`. If it was already listed, remove it with `-` and re-add it; switching it off and on isn't enough. This also happens after signing with a different identity.
- **Still blocked after a restart**: an old session may have started its own daemon copy. Kill any `lets-build-clauder daemon` process that isn't the LaunchAgent's, then kickstart again. Quitting and reopening the Claude app clears it too.
- **`pad asleep` under the prompt**: the pad is asleep, out of range or paired with another computer. Press any key to wake it.
- **Agent keys don't switch windows**: on Linux, terminals that serve several windows from one process can't be told apart. On macOS the first switch may ask to let your terminal control another app; allow it.
- **Keys never turn amber**: managed settings that restrict plugin hooks stop the hook that sees permission prompts. Everything else works.
- **Allow/Deny from the pad** works in chats that loaded this version of the plugin (new chats, or after `/reload-plugins`); older chats show a notice to click Allow.
- Anything else: run `/lets-build-clauder`, then read the log.

## Rebuilding after a change

```sh
go test ./...
scripts/install-macos.sh            # rebuild, sign, reinstall, restart the daemon
claude plugin update lets-build-clauder@lets-build-clauder # or reinstall, to refresh the plugin copy
```

Checks: `gofmt -l .`, `go vet ./...`, `go test -race ./...`, `claude plugin validate .`, `claude plugin test .`.

## Changes from upstream

See [CHANGES.md](CHANGES.md).

## Credits

- [codex-micro-for-claude-code](https://github.com/dustinwdev/codex-micro-for-claude-code) by dustinwdev, which Let's Build Clauder forks.
- The device protocol builds on the reverse-engineering notes of [creator-micro-agent-status](https://github.com/coeckmath-maker/creator-micro-agent-status) by Mathieu Coeck.

## License

MIT. See [LICENSE](LICENSE).
