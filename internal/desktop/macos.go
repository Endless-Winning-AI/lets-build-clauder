//go:build darwin

package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/keys"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/macos"
	"github.com/Endless-Winning-AI/lets-build-clauder/internal/proc"
)

// MacOS drives terminal apps through their AppleScript. Ghostty and iTerm2
// can focus a given terminal and type into it without focusing it;
// Terminal can focus a tab. Windows are "<app>:<id>".
type MacOS struct {
	mu   sync.Mutex
	held uint64 // modifier flags Hold holds down
}

// Detect returns the desktop this session runs on.
func Detect(string) Desktop {
	return &MacOS{}
}

func (*MacOS) Name() string { return "macOS" }

// hostSessionID is a Claude desktop session id the app's URL scheme takes.
var hostSessionID = regexp.MustCompile(`^local_[A-Za-z0-9-]{1,64}$`)

var apps = []struct{ needle, name string }{
	{"ghostty", "Ghostty"}, {"iterm2", "iTerm2"}, {"terminal", "Terminal"},
}

// HostApp names the terminal app a process runs under: "Ghostty", "iTerm2",
// "Terminal", or "".
func HostApp(pid int) string {
	for _, p := range proc.Ancestors(pid) {
		name := strings.ToLower(filepath.Base(proc.Command(p)))
		for _, app := range apps {
			if strings.HasPrefix(name, app.needle) {
				return app.name
			}
		}
	}
	return ""
}

func osascript(script string) (string, bool) {
	return output(5*time.Second, "osascript", "-e", script)
}

// quote makes an AppleScript string literal.
func quote(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text) + `"`
}

type ghosttyTerminal struct{ id, cwd, title string }

func ghosttyTerminals() []ghosttyTerminal {
	// Inside "tell Ghostty", "tab" names Ghostty's class, not the character.
	out, _ := osascript(`
		set sep to character id 9
		tell application "Ghostty"
			set out to ""
			repeat with t in terminals
				set out to out & (id of t) & sep & (working directory of t) & sep & (name of t) & linefeed
			end repeat
			return out
		end tell`)
	var terminals []ghosttyTerminal
	for _, line := range strings.Split(out, "\n") {
		if parts := strings.Split(line, "\t"); len(parts) == 3 {
			terminals = append(terminals, ghosttyTerminal{parts[0], parts[1], parts[2]})
		}
	}
	return terminals
}

func realpath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// Locate finds a session's terminal. Ghostty's are known by working
// directory, so when several share one it returns "" (see Titled).
func (*MacOS) Locate(pid int, cwd string) string {
	switch app := HostApp(pid); app {
	case "Ghostty":
		var same []string
		for _, t := range ghosttyTerminals() {
			if realpath(t.cwd) == realpath(cwd) {
				same = append(same, t.id)
			}
		}
		if len(same) == 1 {
			return "Ghostty:" + same[0]
		}
	case "iTerm2":
		if tty := proc.TTY(pid); tty != "" {
			if id, ok := osascript(iterm(`if tty of s is ` + quote(tty) + ` then return id of s`)); ok && id != "" {
				return "iTerm2:" + id
			}
		}
	case "Terminal":
		if tty := proc.TTY(pid); tty != "" {
			if found, _ := osascript(terminal(`if tty of t is ` + quote(tty) + ` then return "yes"`)); found == "yes" {
				return "Terminal:" + tty
			}
		}
	}
	return ""
}

// iterm wraps a statement in a loop over iTerm2's sessions, s.
func iterm(body string) string {
	return `
		tell application "iTerm2"
			repeat with w in windows
				repeat with t in tabs of w
					repeat with s in sessions of t
						` + body + `
					end repeat
				end repeat
			end repeat
		end tell`
}

// terminal wraps a statement in a loop over Terminal's tabs, t.
func terminal(body string) string {
	return `
		tell application "Terminal"
			repeat with w in windows
				repeat with t in tabs of w
					` + body + `
				end repeat
			end repeat
		end tell`
}

func (*MacOS) Titled(title string) string {
	var found []string
	for _, t := range ghosttyTerminals() {
		if t.title == title {
			found = append(found, t.id)
		}
	}
	if len(found) == 1 {
		return "Ghostty:" + found[0]
	}
	return ""
}

func (*MacOS) Focus(window string) bool {
	app, id, _ := strings.Cut(window, ":")
	var script string
	switch app {
	case "Ghostty":
		script = `tell application "Ghostty"
			activate
			focus terminal id ` + quote(id) + `
		end tell`
	case "iTerm2":
		script = iterm(`if id of s is ` + quote(id) + ` then
			select w
			select t
			select s
			activate
			return
		end if`)
	case "Terminal":
		script = terminal(`if tty of t is ` + quote(id) + ` then
			set selected of t to true
			set index of w to 1
			activate
			return
		end if`)
	case "Claude":
		// The Claude desktop app opens a session by its host id through its
		// URL scheme; without one, just bring the app forward.
		if hostSessionID.MatchString(id) {
			_, ok := output(5*time.Second, "open", "claude://code/continue?session="+id+"&source=desktop_action")
			return ok
		}
		_, ok := output(5*time.Second, "open", "-b", "com.anthropic.claudefordesktop")
		return ok
	default:
		return false
	}
	_, ok := osascript(script)
	return ok
}

func (*MacOS) Press(window string, c keys.Chord) bool {
	app, id, _ := strings.Cut(window, ":")
	switch app {
	case "Ghostty":
		script := `tell application "Ghostty" to send key ` + quote(c.Key)
		if len(c.Mods) > 0 {
			names := map[string]string{"ctrl": "control", "alt": "option", "cmd": "command"}
			var mods []string
			for _, m := range c.Mods {
				if name, ok := names[m]; ok {
					m = name
				}
				mods = append(mods, m)
			}
			script += ` modifiers ` + quote(strings.Join(mods, ","))
		}
		_, ok := osascript(script + ` to terminal id ` + quote(id))
		return ok
	case "iTerm2":
		text := map[string]string{"escape": "(ASCII character 27)", "tab": "tab", "enter": "return"}[c.Key]
		if text == "" {
			text = quote(c.Text())
		}
		_, ok := osascript(iterm(`if id of s is ` + quote(id) + ` then
			tell s to write text ` + text + ` newline no
			return
		end if`))
		return ok
	}
	return false
}

func (m *MacOS) Approve(window string) bool {
	// Ghostty sends no text for printable keys, so take the highlighted
	// option, Yes unless the selection was moved, with Enter.
	if strings.HasPrefix(window, "Ghostty:") {
		return m.Press(window, keys.Chord{Key: "enter"})
	}
	return m.Press(window, keys.Chord{Key: "1"})
}

func (*MacOS) ClaudeInFront() bool {
	app, ok := output(2*time.Second, "lsappinfo", "front")
	if !ok || app == "" {
		return false
	}
	id, _ := output(2*time.Second, "lsappinfo", "info", "-only", "bundleid", app)
	return strings.Contains(id, `"com.anthropic.claudefordesktop"`)
}

// front names the terminal in front, or "".
func front() string {
	out, _ := osascript(`
		if application "Ghostty" is running then
			tell application "Ghostty"
				if frontmost then return "Ghostty:" & (id of focused terminal of selected tab of front window)
			end tell
		end if
		if application "iTerm2" is running then
			tell application "iTerm2"
				if frontmost then return "iTerm2:" & (id of current session of current window)
			end tell
		end if
		return ""`)
	return out
}

// Follow polls the terminal in front: there is no focus event to subscribe to.
func (*MacOS) Follow(ctx context.Context, onFocus func(string)) {
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
		if window := front(); window != last {
			last = window
			onFocus(window)
		}
	}
}

// Raise brings forward the app a process runs under: its front window, not
// necessarily the session's.
func (*MacOS) Raise(pid int) bool {
	for _, p := range proc.Ancestors(pid) {
		if app, _, ok := strings.Cut(proc.Command(p), ".app/Contents/MacOS/"); ok {
			_, ok := output(5*time.Second, "open", "-a", app+".app")
			return ok
		}
	}
	return false
}

func (*MacOS) Open(cwd string, argv []string, near int) bool {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	line := "cd " + shellQuote(cwd) + " && exec " + shellJoin(argv)
	login := shell + " -lic " + shellQuote(line)
	var script string
	switch HostApp(near) {
	case "Ghostty":
		script = `tell application "Ghostty" to new window with configuration ` +
			`{command:` + quote(login) + `, initial working directory:` + quote(cwd) + `}`
	case "iTerm2":
		script = `tell application "iTerm2" to create window with default profile command ` + quote(login)
	default:
		script = `tell application "Terminal"
			activate
			do script ` + quote(line) + `
		end tell`
	}
	_, ok := osascript(script)
	return ok
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// Virtual key codes, by US layout position, and the flag each modifier sets.
var (
	keyCodes = map[string]uint16{
		"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
		"b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "1": 18, "2": 19,
		"3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28,
		"0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "enter": 36,
		"l": 37, "j": 38, "'": 39, "k": 40, ";": 41, `\`: 42, ",": 43, "/": 44, "n": 45,
		"m": 46, ".": 47, "tab": 48, "space": 49, "`": 50, "backspace": 51, "escape": 53,
		"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100,
		"f9": 101, "f10": 109, "f11": 103, "f12": 111, "f13": 105, "f14": 107, "f15": 113,
		"f16": 106, "f17": 64, "f18": 79, "f19": 80, "f20": 90,
		"right-cmd": 54, "right-shift": 60, "right-alt": 61, "right-ctrl": 62,
	}
	modifiers = map[string]struct {
		code uint16
		flag uint64
	}{
		"cmd": {55, 0x100000}, "shift": {56, 0x20000}, "alt": {58, 0x80000},
		"ctrl": {59, 0x40000}, "fn": {63, 0x800000},
		"right-cmd": {54, 0x100000}, "right-shift": {60, 0x20000},
		"right-alt": {61, 0x80000}, "right-ctrl": {62, 0x40000},
	}
)

const (
	flagsChanged = 12 // kCGEventFlagsChanged
	hidEventTap  = 0  // kCGHIDEventTap
)

// Hold posts the chord's keys to the system as a keyboard would: down in
// order, or up in reverse. It needs the Accessibility permission.
func (m *MacOS) Hold(c keys.Chord, down bool) error {
	if !macos.AXIsProcessTrusted() {
		return errors.New("macOS blocked synthetic keys: allow the lets-build-clauder daemon program under " +
			"System Settings > Privacy & Security > Accessibility")
	}
	names := append(append([]string(nil), c.Mods...), c.Key)
	for _, name := range names {
		if _, ok := modifiers[name]; !ok {
			if _, ok := keyCodes[name]; !ok {
				return errors.New("no key named " + quote(name))
			}
		}
	}
	if !down {
		for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
			names[i], names[j] = names[j], names[i]
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range names {
		if mod, ok := modifiers[name]; ok {
			if down {
				m.held |= mod.flag
			} else {
				m.held &^= mod.flag
			}
			post(mod.code, down, m.held, true)
		} else {
			post(keyCodes[name], down, m.held, false)
		}
	}
	return nil
}

func post(code uint16, down bool, flags uint64, modifier bool) {
	event := macos.CGEventCreateKeyboardEvent(0, code, down)
	if event == 0 {
		return
	}
	defer macos.CFRelease(event)
	if modifier {
		macos.CGEventSetType(event, flagsChanged)
	}
	macos.CGEventSetFlags(event, flags)
	macos.CGEventPost(hidEventTap, event)
}

// Trusted reports whether this process may post synthetic keys.
func Trusted() bool { return macos.AXIsProcessTrusted() }

func (*MacOS) CanHold() bool { return macos.AXIsProcessTrusted() }
