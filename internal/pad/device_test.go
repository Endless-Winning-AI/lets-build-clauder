package pad

import "testing"

func TestPickKeepsToTheChannel(t *testing.T) {
	one := Found{Model: "Creator Micro 2", Transport: "bluetooth", Name: "Creator Micro 2 #1", path: "1"}
	two := Found{Model: "Creator Micro 2", Transport: "bluetooth", Name: "Creator Micro 2 #2", path: "2"}
	usb := Found{Model: "Creator Micro 2", Transport: "usb", Name: "Creator Micro 2", path: "3"}
	for _, c := range []struct {
		found   []Found
		channel int
		want    string
	}{
		{[]Found{one, two}, 0, "1"},
		{[]Found{one, two}, 2, "2"},
		{[]Found{one}, 2, ""},
		{[]Found{one, usb}, 2, "3"},
		{nil, 0, ""},
	} {
		got, ok := Pick(c.found, c.channel)
		if ok != (c.want != "") || got.path != c.want {
			t.Errorf("Pick(%v, %d) = %v, %v; want %q", c.found, c.channel, got, ok, c.want)
		}
	}
}
