---
name: lets-build-clauder-config
description: Change what the keys, dial and joystick on a Codex Micro or Creator Micro 2 do in Claude Code, by editing the Let's Build Clauder plugin's config file. Use when the user asks to remap, rebind or reassign a pad control, asks what a control does, or wants to dictate with the mic key through Wispr Flow, Superwhisper, macOS Dictation or another dictation app.
---

# Configuring the pad with Let's Build Clauder

The plugin reads `~/.config/lets-build-clauder/config.toml` (or `$XDG_CONFIG_HOME/lets-build-clauder/config.toml`). The file is optional; a control it leaves out keeps its default. A saved change applies on the next press, with nothing to restart.

## Controls and their defaults

| Section | Control | Default |
| --- | --- | --- |
| `[keys]` | `fast` (lightning bolt) | `"fast"` |
| | `approve` (check) | `"approve"` |
| | `deny` (cross) | `"deny"` |
| | `branch` (branch) | `"fork"` |
| | `mic` (microphone) | `"voice"` |
| | `send` (Codex logo) | `"submit"` |
| `[dial]` | `left`, `right` | `"effort-down"`, `"effort-up"` |
| | `press` | `"/effort"` |
| | `hold` (half a second or longer) | `"none"` |
| `[stick]` | `up` | `"cycle-mode"` |
| | `down` | `"attention"` |
| | `left`, `right` | `"prev-session"`, `"next-session"` |
| `[terminal]` | `command` | detected (the terminal `fork` opens) |
| `[agents]` | `manual` | `false` (sessions take free keys as they start) |
| | `hide` | `[]` (sidebar groups `chat-next` and `chat-prev` skip) |

The six agent keys are not configurable: each shows its session's state and switches to it. Holding the send key and pressing an agent key pins the session last typed in to that key; pins survive restarts. With `manual = true`, only pinned sessions get keys, and keys never move on their own.

## Actions

| Action | Effect |
| --- | --- |
| `"approve"` | Yes to the permission prompt waiting on the user |
| `"deny"` | No to it; with none waiting, stop the running turn |
| `"interrupt"` | stop the running turn |
| `"submit"` | send what is typed in the prompt box |
| `"fast"`, `"voice"` | toggle fast mode, voice mode |
| `"fork"` | copy the conversation into a new terminal window |
| `"effort-up"`, `"effort-down"` | step reasoning effort through low, medium, high, xhigh, max |
| `"cycle-mode"` | cycle permission modes, as Shift+Tab does |
| `"attention"` | switch to the session that most needs the user; with none, the next session |
| `"next-session"`, `"prev-session"` | switch between sessions in key order |
| `"chat-next"`, `"chat-prev"` | step down or up the Claude desktop app's sidebar from the chat on screen, opening chats that aren't running; skips groups in `[agents] hide` |
| `"model-next"`, `"model-prev"` | step the model through Opus, Fable, Sonnet |
| `"none"` | nothing |
| `"/command args"` | run a slash command, e.g. `"/model sonnet"` |
| `{ text = "...", submit = true }` | type into the prompt box; `submit` sends it |
| `{ keys = "ctrl+r" }` | press a key chord in the session's terminal |
| `{ hold = "fn" }` | hold a key or chord down for the whole desktop while the pad key is held (`[keys]` only, macOS only) |
| `{ hold = "cmd+ctrl+space", tap = "ctrl+space" }` | the same, but a press shorter than a quarter second presses `tap` once instead |
| `{ double = "enter" }` | press a key or chord for the whole desktop on a double tap within 0.4 s; one tap does nothing (`send` only, macOS only) |
| `{ run = ["program", "arg"] }` | run a program |
| `{ run = "shell command" }` | run a shell command |

Key names: letters, digits, `escape`, `tab`, `enter`, `space`, `backspace`, `f1` to `f20`, and the modifiers `ctrl`, `shift`, `alt`, `cmd`, `fn`, with `right-` forms such as `right-alt`. Join a chord with `+`: `"ctrl+space"`.

Controls act on the Claude Code session whose terminal has focus, or else the one a control last went to.

## Dictation apps

`hold` mirrors the pad key: the app sees its shortcut pressed for as long as the user holds the key, so hold-to-talk, double-tap and quick-tap shortcuts all work as they would on the keyboard.

1. Find which dictation apps are installed: `ls /Applications | grep -iE 'wispr|superwhisper|voiceink|macwhisper|aqua'`. Ask which one the user dictates with if there are several, or if none shows.
2. Find the app's shortcut. Wispr Flow's default is holding `fn`. For any other app, or if the user changed Wispr's, ask the user which key or chord their app listens for rather than guessing. macOS Dictation's shortcut is under System Settings > Keyboard > Dictation.
3. Set `mic = { hold = "<key>" }` under `[keys]`, as in Making a change below. If the app also has a hands-free toggle shortcut, add it as `tap`: `mic = { hold = "<key>", tap = "<toggle key>" }`.
4. Holding keys needs the program that runs the lets-build-clauder daemon allowed under System Settings > Privacy & Security > Accessibility: the terminal app that runs Claude Code, or, when the daemon runs as a LaunchAgent, the daemon binary itself. Doctor (below) reports whether it is. If it isn't, walk the user through allowing it, then restart the daemon (quit and reopen the terminal app, or `launchctl kickstart -k` the agent).
5. Have the user hold the mic key, speak into the prompt box, and let go.

On Linux, `hold` is not supported yet. Most Linux dictation tools have a toggle command: map the mic key to it with `run`.

## Making a change

1. Read the current file if it exists. Change only the entries the user asked about, and keep everything else, comments included.
2. Write the file, creating `~/.config/lets-build-clauder/` if needed.
3. Check it with doctor: run `bin/lets-build-clauder doctor`, found two folders above this skill's base directory (`<base directory>/../../bin/lets-build-clauder`). It lists any mistake in the config, line by line. A mistaken entry keeps its default; a file that does not parse is ignored as a whole.
4. Tell the user what each changed control now does. The change applies on the next press.

The user can run the same check as `/lets-build-clauder`.
