package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// pins remembers which agent key a Claude desktop session was pinned to, by
// the session's host id ("local_..."), across daemon and app restarts.
type pins struct {
	path string
	keys map[string]int // host id -> slot
}

// loadPins reads the pins kept at path; unreadable, there are none.
func loadPins(path string) *pins {
	p := &pins{path: path, keys: map[string]int{}}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &p.keys)
	}
	return p
}

// slot is the key pinned to a host id, or -1.
func (p *pins) slot(host string) int {
	if s, ok := p.keys[host]; ok && host != "" {
		return s
	}
	return -1
}

// host is the host id pinned to a key, or "".
func (p *pins) host(slot int) string {
	for h, s := range p.keys {
		if s == slot {
			return h
		}
	}
	return ""
}

// set pins a host id to a key, unpinning whatever else had the key, and saves.
func (p *pins) set(host string, slot int) error {
	for h, s := range p.keys {
		if s == slot {
			delete(p.keys, h)
		}
	}
	p.keys[host] = slot
	data, _ := json.MarshalIndent(p.keys, "", "  ")
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o644)
}
