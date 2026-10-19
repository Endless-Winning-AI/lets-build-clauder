// Package pad talks to the pad: JSON-RPC 2.0 carried in 64-byte HID reports.
//
//	byte 0    report id, always 0x06
//	byte 1    channel: 1 is the firmware's debug log, 2 is JSON-RPC
//	byte 2    payload length in this report, at most 61
//	byte 3..  UTF-8 payload
//
// Replies carry no delimiter, so the stream is cut apart by balancing braces.
// These notes follow creator-micro-agent-status by Mathieu Coeck (MIT):
// https://github.com/coeckmath-maker/creator-micro-agent-status
package pad

const (
	vendorID   = 0x303A
	reportID   = 0x06
	channelRPC = 2
	maxPayload = 61
	reportSize = 64
)

// models names the supported pads by product id.
var models = map[uint16]string{
	0x8360: "Codex Micro",
	0x8298: "Creator Micro 2",
	0x8297: "Creator Micro 2",
}

// frames splits a message into the reports that carry it.
func frames(msg []byte) [][]byte {
	var out [][]byte
	for off := 0; ; {
		n := min(maxPayload, len(msg)-off)
		r := make([]byte, reportSize)
		r[0], r[1], r[2] = reportID, channelRPC, byte(n)
		copy(r[3:], msg[off:off+n])
		out = append(out, r)
		off += n
		if off >= len(msg) {
			return out
		}
	}
}

// payload is the JSON-RPC text a report carries, or nil for any other report.
func payload(r []byte) []byte {
	if len(r) < 3 || r[0] != reportID || r[1] != channelRPC {
		return nil
	}
	return r[3 : 3+min(int(r[2]), len(r)-3)]
}

// splitJSON cuts a delimiter-free stream of JSON objects apart by balancing
// braces. It returns the complete objects and the unfinished remainder.
func splitJSON(buf []byte) (objs [][]byte, rest []byte) {
	depth, start := 0, -1
	inString, escaped := false, false
	for i, c := range buf {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 {
				objs = append(objs, buf[start:i+1])
				start = -1
			}
		}
	}
	if depth > 0 && start >= 0 {
		rest = buf[start:]
	}
	return objs, rest
}
