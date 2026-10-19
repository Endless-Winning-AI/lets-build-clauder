package pad

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Endless-Winning-AI/lets-build-clauder/internal/macos"
	"github.com/ebitengine/purego"
)

const (
	outputReport = 1          // kIOHIDReportTypeOutput
	notPermitted = 0xE00002E2 // kIOReturnNotPermitted: Input Monitoring is off
	exclusive    = 0xE00002C5 // kIOReturnExclusiveAccess: another app holds it
)

// devices lists this vendor's HID devices. They belong to the returned
// manager and set, which the caller releases when done with them.
func devices() (manager, set uintptr, list []uintptr) {
	match := macos.Dictionary(map[string]int32{"VendorID": vendorID})
	defer macos.CFRelease(match)
	manager = macos.IOHIDManagerCreate(0, 0)
	macos.IOHIDManagerSetDeviceMatching(manager, match)
	set = macos.IOHIDManagerCopyDevices(manager)
	if set != 0 {
		list = macos.Values(set)
	}
	return manager, set, list
}

func release(manager, set uintptr) {
	if set != 0 {
		macos.CFRelease(set)
	}
	macos.CFRelease(manager)
}

func property(device uintptr, key string) uintptr {
	name := macos.String(key)
	defer macos.CFRelease(name)
	return macos.IOHIDDeviceGetProperty(device, name)
}

func intProperty(device uintptr, key string) int32 {
	n, _ := macos.GoInt(property(device, key))
	return n
}

// Find lists the supported pads attached to this computer.
func Find() []Found {
	manager, set, list := devices()
	defer release(manager, set)
	var found []Found
	for _, device := range list {
		model, ok := models[uint16(intProperty(device, "ProductID"))]
		if !ok {
			continue
		}
		kind, _ := macos.GoString(property(device, "Transport"))
		transport := "usb"
		if strings.Contains(kind, "Bluetooth") {
			transport = "bluetooth"
		}
		location := strconv.Itoa(int(intProperty(device, "LocationID")))
		found = append(found, Found{Model: model, Transport: transport, path: location})
	}
	return found
}

// AccessProblem says why this user can't open the pad, or "" when they can.
// macOS tells only on opening it.
func AccessProblem(Found) string { return "" }

// IOKit calls back through C function pointers, of which a process can make
// only so many: two serve every transport, which the context picks out.
var (
	callbacks struct {
		sync.Once
		report, removal uintptr
	}
	opened   sync.Map // context -> *iokit
	contexts atomic.Uintptr
)

type iokit struct {
	device   uintptr
	loop     uintptr
	buffer   []byte
	onReport func([]byte)
	onLost   func()
	closed   sync.Once

	using    sync.RWMutex // held by writes, and by the release of device
	released bool
}

func open(f Found, onReport func([]byte), onLost func()) (transport, error) {
	callbacks.Do(func() {
		callbacks.report = purego.NewCallback(func(context, result, sender, kind, id, report, length uintptr) {
			if t, ok := opened.Load(context); ok {
				h := t.(*iokit)
				h.onReport(append([]byte(nil), h.buffer[:min(int(length), len(h.buffer))]...))
			}
		})
		callbacks.removal = purego.NewCallback(func(context, result, sender uintptr) {
			if t, ok := opened.Load(context); ok {
				t.(*iokit).lost()
			}
		})
	})
	context := contexts.Add(1)

	h := &iokit{buffer: make([]byte, 256), onReport: onReport, onLost: onLost}
	ready := make(chan error, 1)
	// The run loop delivers the pad's reports on the thread that runs it.
	go func() {
		runtime.LockOSThread()
		manager, set, list := devices()
		defer release(manager, set)
		for _, device := range list {
			if strconv.Itoa(int(intProperty(device, "LocationID"))) == f.path {
				h.device = device
			}
		}
		if h.device == 0 {
			ready <- errors.New("the pad went away")
			return
		}
		if result := uint32(macos.IOHIDDeviceOpen(h.device, 0)); result != 0 {
			ready <- openError(result)
			return
		}
		opened.Store(context, h)
		defer opened.Delete(context)
		macos.IOHIDDeviceRegisterInputReportCallback(h.device, &h.buffer[0], len(h.buffer), callbacks.report, context)
		macos.IOHIDDeviceRegisterRemovalCallback(h.device, callbacks.removal, context)
		h.loop = macos.CFRunLoopGetCurrent()
		macos.IOHIDDeviceScheduleWithRunLoop(h.device, h.loop, macos.RunLoopDefaultMode)
		ready <- nil
		macos.CFRunLoopRun()
		h.using.Lock()
		defer h.using.Unlock()
		h.released = true
		macos.IOHIDDeviceUnscheduleFromRunLoop(h.device, h.loop, macos.RunLoopDefaultMode)
		macos.IOHIDDeviceClose(h.device, 0)
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return h, nil
}

func openError(result uint32) error {
	switch result {
	case notPermitted:
		return errors.New("macOS blocked access: allow the lets-build-clauder daemon program under " +
			"System Settings > Privacy & Security > Input Monitoring")
	case exclusive:
		return errors.New("another app holds the pad: quit the ChatGPT app")
	}
	return fmt.Errorf("IOHIDDeviceOpen failed: %#x", result)
}

func (h *iokit) write(report []byte) error {
	h.using.RLock()
	defer h.using.RUnlock()
	if h.released {
		return ErrLost
	}
	if result := uint32(macos.IOHIDDeviceSetReport(h.device, outputReport, int(report[0]), &report[0], len(report))); result != 0 {
		return fmt.Errorf("IOHIDDeviceSetReport failed: %#x", result)
	}
	return nil
}

func (h *iokit) lost() {
	h.close()
	h.onLost()
}

func (h *iokit) close() {
	h.closed.Do(func() { macos.CFRunLoopStop(h.loop) })
}
