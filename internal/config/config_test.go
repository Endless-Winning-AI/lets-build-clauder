package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, text string) string {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMissingFileGivesDefaults(t *testing.T) {
	c, problems := Load(filepath.Join(t.TempDir(), "none.toml"))
	if problems != nil || !reflect.DeepEqual(c, Defaults()) {
		t.Errorf("Load = %+v, %v", c, problems)
	}
}

func TestLoadLaysConfigOverDefaults(t *testing.T) {
	c, problems := Load(write(t, `
[keys]
mic = { hold = "fn" }
send = { text = "continue", submit = true }
branch = "/model sonnet"

[stick]
up = { keys = "ctrl+r" }
down = { run = ["notify-send", "hi"] }

[terminal]
command = "foot -e"
`))
	if problems != nil {
		t.Fatalf("problems: %v", problems)
	}
	want := map[[2]string]Action{
		{"keys", "mic"}:     {Hold: "fn"},
		{"keys", "send"}:    {Text: "continue", Submit: true},
		{"keys", "branch"}:  {Name: "/model sonnet"},
		{"keys", "approve"}: {Name: "approve"},
		{"stick", "up"}:     {Keys: "ctrl+r"},
		{"stick", "down"}:   {Run: []string{"notify-send", "hi"}},
		{"dial", "right"}:   {Name: "effort-up"},
	}
	for at, action := range want {
		if got := c.Action(at[0], at[1]); !reflect.DeepEqual(got, action) {
			t.Errorf("%s.%s = %+v, want %+v", at[0], at[1], got, action)
		}
	}
	if !reflect.DeepEqual(c.Terminal, []string{"foot", "-e"}) {
		t.Errorf("terminal = %q", c.Terminal)
	}
}

func TestLoadNamesEachProblem(t *testing.T) {
	c, problems := Load(write(t, `
[keys]
mic = "dictate"
send = { text = "x", keys = "enter" }
lightning = "fast"

[dial]
press = { hold = "fn" }

[pedals]
left = "fast"
`))
	got := strings.Join(problems, "\n")
	for _, want := range []string{
		`keys.mic: unknown action "dictate"`,
		"keys.send: expected one of",
		"keys.lightning: no such control",
		"dial.press: hold works on keys only",
		"[pedals]: no such section",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
	if !reflect.DeepEqual(c.Action("keys", "mic"), Action{Name: "voice"}) {
		t.Errorf("a wrong entry should keep its default, got %+v", c.Action("keys", "mic"))
	}
}

func TestLoadReportsSyntaxErrors(t *testing.T) {
	_, problems := Load(write(t, "[keys\n"))
	if len(problems) != 1 {
		t.Errorf("problems = %v", problems)
	}
}

func TestTapGoesWithHold(t *testing.T) {
	c, problems := Load(write(t, `
[keys]
mic = { hold = "cmd+ctrl+space", tap = "ctrl+space" }
`))
	if problems != nil {
		t.Fatalf("problems: %v", problems)
	}
	want := Action{Hold: "cmd+ctrl+space", Tap: "ctrl+space"}
	if got := c.Action("keys", "mic"); !reflect.DeepEqual(got, want) {
		t.Errorf("mic = %+v, want %+v", got, want)
	}
	if s := want.String(); s != "hold cmd+ctrl+space, tap ctrl+space" {
		t.Errorf("String = %q", s)
	}

	_, problems = Load(write(t, "[keys]\nmic = { tap = \"ctrl+space\" }\n"))
	if len(problems) != 1 || !strings.Contains(problems[0], "tap goes with hold") {
		t.Errorf("tap alone: problems = %v", problems)
	}
	_, problems = Load(write(t, "[dial]\npress = { hold = \"fn\", tap = \"f13\" }\n"))
	if len(problems) != 1 || !strings.Contains(problems[0], "hold works on keys only") {
		t.Errorf("tap on dial: problems = %v", problems)
	}
}

func TestDoubleWorksOnKeysOnly(t *testing.T) {
	c, problems := Load(write(t, "[keys]\nsend = { double = \"return\" }\n"))
	if problems != nil {
		t.Fatalf("problems: %v", problems)
	}
	if got := c.Action("keys", "send"); got.Double != "return" || got.String() != "double return" {
		t.Errorf("send = %+v", got)
	}
	if _, problems = Load(write(t, "[keys]\napprove = { double = \"return\" }\n")); problems != nil {
		t.Errorf("double on approve: problems = %v", problems)
	}
	_, problems = Load(write(t, "[dial]\npress = { double = \"return\" }\n"))
	if len(problems) != 1 || !strings.Contains(problems[0], "keys only") {
		t.Errorf("double on the dial: problems = %v", problems)
	}
}

func TestFieldsThatGoWithAnotherAreChecked(t *testing.T) {
	_, problems := Load(write(t, "[keys]\nsend = { text = \"go\", submit = \"yes\" }\n"))
	if len(problems) != 1 || !strings.Contains(problems[0], "submit: expected true or false") {
		t.Errorf("submit as a string: problems = %v", problems)
	}
	_, problems = Load(write(t, "[keys]\nmic = { hold = \"fn\", tap = 1 }\n"))
	if len(problems) != 1 || !strings.Contains(problems[0], "tap: ") {
		t.Errorf("tap as a number: problems = %v", problems)
	}
}

func TestAgents(t *testing.T) {
	c, problems := Load(write(t, "[agents]\nmanual = true\nhide = [\"Alpha\", \"This computer\"]\n"))
	if problems != nil || !c.Manual || !reflect.DeepEqual(c.Hide, []string{"Alpha", "This computer"}) {
		t.Errorf("Load = %+v, %v", c, problems)
	}
	c, problems = Load(write(t, "[agents]\nhide = \"This computer\"\n"))
	if problems != nil || c.Manual || !reflect.DeepEqual(c.Hide, []string{"This computer"}) {
		t.Errorf("one name: Load = %+v, %v", c, problems)
	}
	_, problems = Load(write(t, "[agents]\nmanual = \"yes\"\nother = 1\n"))
	if len(problems) != 2 || !strings.Contains(problems[0], "agents.manual") || !strings.Contains(problems[1], "agents.other") {
		t.Errorf("problems = %v", problems)
	}
}
