package pad

import (
	"bytes"
	"strings"
	"testing"
)

func TestSplitJSON(t *testing.T) {
	tests := []struct {
		name, in string
		objs     []string
		rest     string
	}{
		{"two objects", `{"a":1}{"b":{"c":2}}`, []string{`{"a":1}`, `{"b":{"c":2}}`}, ""},
		{"unfinished", `{"a":1}{"b":`, []string{`{"a":1}`}, `{"b":`},
		{"braces in strings", `{"s":"}{"}`, []string{`{"s":"}{"}`}, ""},
		{"escaped quote", `{"s":"a\"}"}`, []string{`{"s":"a\"}"}`}, ""},
		{"escaped backslash", `{"s":"a\\"}{"t":1}`, []string{`{"s":"a\\"}`, `{"t":1}`}, ""},
		{"noise between", `x{"a":1}y`, []string{`{"a":1}`}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs, rest := splitJSON([]byte(tt.in))
			var got []string
			for _, o := range objs {
				got = append(got, string(o))
			}
			if strings.Join(got, "|") != strings.Join(tt.objs, "|") || string(rest) != tt.rest {
				t.Errorf("splitJSON(%s) = %q, %q; want %q, %q", tt.in, got, rest, tt.objs, tt.rest)
			}
		})
	}
}

func TestFrames(t *testing.T) {
	msg := []byte(strings.Repeat("x", 130))
	reports := frames(msg)
	if len(reports) != 3 {
		t.Fatalf("got %d reports, want 3", len(reports))
	}
	var joined []byte
	for _, r := range reports {
		if len(r) != reportSize {
			t.Fatalf("report is %d bytes, want %d", len(r), reportSize)
		}
		joined = append(joined, payload(r)...)
	}
	if !bytes.Equal(joined, msg) {
		t.Errorf("payloads rejoin to %q", joined)
	}
}

func TestPayloadIgnoresOtherChannels(t *testing.T) {
	if p := payload([]byte{reportID, 1, 3, 'l', 'o', 'g'}); p != nil {
		t.Errorf("debug channel gave %q", p)
	}
}
