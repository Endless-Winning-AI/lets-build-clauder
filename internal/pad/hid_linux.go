package pad

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Find lists the supported pads attached to this computer.
func Find() []Found {
	nodes, _ := filepath.Glob("/sys/class/hidraw/hidraw*")
	var found []Found
	for _, node := range nodes {
		uevent, err := os.ReadFile(filepath.Join(node, "device", "uevent"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(uevent), "\n") {
			id, ok := strings.CutPrefix(line, "HID_ID=")
			if !ok {
				continue
			}
			// HID_ID=0005:0000303A:00008360: bus, vendor, product.
			parts := strings.Split(id, ":")
			if len(parts) != 3 {
				continue
			}
			bus, _ := strconv.ParseUint(parts[0], 16, 16)
			vid, _ := strconv.ParseUint(parts[1], 16, 32)
			pid, _ := strconv.ParseUint(parts[2], 16, 32)
			model, ok := models[uint16(pid)]
			if vid != vendorID || !ok {
				continue
			}
			kind := map[uint64]string{3: "usb", 5: "bluetooth"}[bus]
			found = append(found, Found{Model: model, Transport: kind, path: "/dev/" + filepath.Base(node)})
		}
	}
	return found
}

// AccessProblem says why this user can't open the pad, or "" when they can.
func AccessProblem(f Found) string {
	if unix.Access(f.path, unix.R_OK|unix.W_OK) == nil {
		return ""
	}
	return fmt.Sprintf("no permission to open %s", f.path)
}

type hidraw struct {
	file   *os.File
	closed sync.Once
}

func open(f Found, onReport func([]byte), onLost func()) (transport, error) {
	file, err := os.OpenFile(f.path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	h := &hidraw{file: file}
	go func() {
		buf := make([]byte, reportSize)
		for {
			n, err := file.Read(buf)
			if err != nil {
				// Unplugged, asleep or out of range; or closed by us.
				h.close()
				onLost()
				return
			}
			onReport(buf[:n])
		}
	}()
	return h, nil
}

func (h *hidraw) write(report []byte) error {
	_, err := h.file.Write(report)
	return err
}

func (h *hidraw) close() {
	h.closed.Do(func() { h.file.Close() })
}
