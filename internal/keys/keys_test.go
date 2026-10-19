package keys

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	got := Parse("shift+Tab")
	if want := (Chord{Mods: []string{"shift"}, Key: "tab"}); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %+v, want %+v", got, want)
	}
	if got := Parse("escape"); len(got.Mods) != 0 || got.Key != "escape" {
		t.Errorf("Parse(escape) = %+v", got)
	}
}

func TestText(t *testing.T) {
	for spec, want := range map[string]string{
		"shift+tab": "\x1b[Z",
		"escape":    "\x1b",
		"enter":     "\r",
		"ctrl+r":    "\x12",
		"1":         "1",
	} {
		if got := Parse(spec).Text(); got != want {
			t.Errorf("Parse(%q).Text() = %q, want %q", spec, got, want)
		}
	}
}
