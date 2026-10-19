package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Action is what a control does. Exactly one field is set.
type Action struct {
	Name   string   // a built-in action, or "/command args"
	Text   string   // typed into the prompt box
	Submit bool     //   and sent
	Keys   string   // pressed in the session's terminal
	Hold   string   // held for the whole desktop while the key is
	Tap    string   // with Hold: pressed once instead, when the key is let go quickly
	Double string   // pressed once for the whole desktop on a quick double tap; one tap does nothing
	Run    []string // a program, or a shell command when given as a string
}

func (a Action) String() string {
	switch {
	case a.Text != "":
		return fmt.Sprintf("text %q", a.Text)
	case a.Keys != "":
		return "keys " + a.Keys
	case a.Hold != "" && a.Tap != "":
		return "hold " + a.Hold + ", tap " + a.Tap
	case a.Hold != "":
		return "hold " + a.Hold
	case a.Double != "":
		return "double " + a.Double
	case a.Run != nil:
		return "run " + strings.Join(a.Run, " ")
	}
	return a.Name
}

// Actions are the built-in actions.
var Actions = []string{
	"approve", "deny", "interrupt", "submit", "fast", "voice", "fork",
	"effort-up", "effort-down", "cycle-mode", "attention", "next-session",
	"prev-session", "chat-next", "chat-prev", "model-next", "model-prev", "none",
}

// Config is what each control does.
type Config struct {
	Controls map[string]map[string]Action // section, then control
	Terminal []string                     // the command "fork" opens a terminal with
	Manual   bool                         // agent keys go only to sessions pinned to them
	Hide     []string                     // sidebar groups the knob skips
}

// Defaults is what each control does when config.toml says nothing of it.
func Defaults() Config {
	named := func(pairs ...string) map[string]Action {
		m := map[string]Action{}
		for i := 0; i < len(pairs); i += 2 {
			m[pairs[i]] = Action{Name: pairs[i+1]}
		}
		return m
	}
	return Config{Controls: map[string]map[string]Action{
		"keys": named("fast", "fast", "approve", "approve", "deny", "deny",
			"branch", "fork", "mic", "voice", "send", "submit"),
		"dial":  named("left", "effort-down", "right", "effort-up", "press", "/effort", "hold", "none"),
		"stick": named("up", "cycle-mode", "down", "attention", "left", "prev-session", "right", "next-session"),
	}}
}

// Action returns what a control does.
func (c Config) Action(section, control string) Action {
	return c.Controls[section][control]
}

// Load reads config.toml over the defaults. Missing, it gives the defaults.
// Wrong in places, it gives the defaults for those places and says what
// was wrong.
func Load(path string) (Config, []string) {
	c := Defaults()
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return c, []string{err.Error()}
	}
	var problems []string
	for _, section := range sortedKeys(raw) {
		entries, ok := raw[section].(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: expected a [%s] table", section, section))
			continue
		}
		if section == "agents" {
			for _, key := range sortedKeys(entries) {
				switch key {
				case "manual":
					manual, ok := entries[key].(bool)
					if !ok {
						problems = append(problems, "agents.manual: expected true or false")
					}
					c.Manual = manual
				case "hide":
					hide, err := argv(entries[key], oneName)
					if err != nil {
						problems = append(problems, "agents.hide: "+err.Error())
					}
					c.Hide = hide
				default:
					problems = append(problems, fmt.Sprintf("agents.%s: no such setting; expected manual or hide", key))
				}
			}
			continue
		}
		if section == "terminal" {
			command, err := argv(entries["command"], strings.Fields)
			if err != nil {
				problems = append(problems, "terminal.command: "+err.Error())
			}
			c.Terminal = command
			continue
		}
		controls, ok := c.Controls[section]
		if !ok {
			problems = append(problems, fmt.Sprintf("[%s]: no such section; expected [keys], [dial], [stick], [agents] or [terminal]", section))
			continue
		}
		for _, control := range sortedKeys(entries) {
			if _, ok := controls[control]; !ok {
				problems = append(problems, fmt.Sprintf("%s.%s: no such control; expected one of %s",
					section, control, strings.Join(sortedKeys(controls), ", ")))
				continue
			}
			action, err := parseAction(entries[control])
			if err == nil && action.Hold != "" && section != "keys" {
				err = errors.New("hold works on keys only")
			}
			if err == nil && action.Double != "" && (section != "keys" || control != "send") {
				err = errors.New("double works on the send key only")
			}
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s.%s: %v", section, control, err))
				continue
			}
			controls[control] = action
		}
	}
	return c, problems
}

func parseAction(v any) (Action, error) {
	switch v := v.(type) {
	case string:
		if !strings.HasPrefix(v, "/") && !slices.Contains(Actions, v) {
			return Action{}, fmt.Errorf("unknown action %q", v)
		}
		return Action{Name: v}, nil
	case map[string]any:
		var a Action
		var set []string // the fields that are actions in themselves
		for _, key := range sortedKeys(v) {
			var err error
			alone := true
			switch key {
			case "text":
				a.Text, err = text(v[key])
			case "submit":
				var ok bool
				if a.Submit, ok = v[key].(bool); !ok {
					err = errors.New("expected true or false")
				}
				alone = false // goes with text
			case "keys":
				a.Keys, err = text(v[key])
			case "hold":
				a.Hold, err = text(v[key])
			case "tap":
				a.Tap, err = text(v[key])
				alone = false // goes with hold
			case "double":
				a.Double, err = text(v[key])
			case "run":
				a.Run, err = argv(v[key], shell)
			default:
				return Action{}, fmt.Errorf("unknown field %q", key)
			}
			if err != nil {
				return Action{}, fmt.Errorf("%s: %v", key, err)
			}
			if alone {
				set = append(set, key)
			}
		}
		if a.Tap != "" && a.Hold == "" {
			return Action{}, errors.New("tap goes with hold")
		}
		if len(set) != 1 {
			return Action{}, errors.New("expected one of text, keys, hold, double or run")
		}
		if a.Submit && a.Text == "" {
			return Action{}, errors.New("submit goes with text")
		}
		return a, nil
	}
	return Action{}, fmt.Errorf("expected an action name or a table, not %v", v)
}

func text(v any) (string, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return "", errors.New("expected a string")
	}
	return s, nil
}

// argv reads a command: a list of arguments, or a string that split makes one.
func argv(v any, split func(string) []string) ([]string, error) {
	switch v := v.(type) {
	case nil:
		return nil, nil
	case string:
		if out := split(v); len(out) > 0 {
			return out, nil
		}
		return nil, errors.New("expected a command")
	case []any:
		out := make([]string, len(v))
		for i, a := range v {
			s, ok := a.(string)
			if !ok {
				return nil, errors.New("expected a list of strings")
			}
			out[i] = s
		}
		if len(out) == 0 {
			return nil, errors.New("expected a command")
		}
		return out, nil
	}
	return nil, errors.New("expected a list of strings or a string")
}

// Modified is when the config file last changed, to tell when to reload it.
func Modified(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// oneName takes a string as one name, spaces and all: how argv reads a
// group name given on its own rather than in a list.
func oneName(s string) []string {
	if s = strings.TrimSpace(s); s != "" {
		return []string{s}
	}
	return nil
}
