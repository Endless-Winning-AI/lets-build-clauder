// Package keys names keys and chords, as configured ("shift+tab") and as a
// terminal receives them.
package keys

import "strings"

// Chord is a key with the modifiers held for it.
type Chord struct {
	Mods []string // "ctrl", "shift", "alt", "cmd", "fn"
	Key  string   // "escape", "tab", "enter", "space", "a", "1", ...
}

// Parse reads a chord such as "shift+Tab" or "ctrl+r".
func Parse(spec string) Chord {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(spec)), "+")
	return Chord{Mods: parts[:len(parts)-1], Key: parts[len(parts)-1]}
}

// Has reports whether the chord holds a modifier.
func (c Chord) Has(mod string) bool {
	for _, m := range c.Mods {
		if m == mod {
			return true
		}
	}
	return false
}

// Only reports whether the chord holds exactly this modifier.
func (c Chord) Only(mod string) bool {
	return len(c.Mods) == 1 && c.Mods[0] == mod
}

// Text is what a terminal reads for the chord.
func (c Chord) Text() string {
	if c.Key == "tab" && c.Only("shift") {
		return "\x1b[Z"
	}
	switch c.Key {
	case "escape":
		return "\x1b"
	case "tab":
		return "\t"
	case "enter":
		return "\r"
	case "space":
		return " "
	}
	if c.Only("ctrl") && len(c.Key) == 1 && 'a' <= c.Key[0] && c.Key[0] <= 'z' {
		return string(rune(c.Key[0] - 'a' + 1))
	}
	return c.Key
}
