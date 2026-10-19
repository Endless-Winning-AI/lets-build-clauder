//go:build darwin

// Package macos binds the parts of macOS's C frameworks this program uses,
// through purego, so it builds without cgo.
package macos

import (
	"bytes"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	utf8   = 0x08000100 // kCFStringEncodingUTF8
	sInt32 = 3          // kCFNumberSInt32Type
)

// CoreFoundation
var (
	CFRelease            func(ref uintptr)
	CFRunLoopGetCurrent  func() uintptr
	CFRunLoopRun         func()
	CFRunLoopStop        func(loop uintptr)
	CFSetGetCount        func(set uintptr) int
	CFSetGetValues       func(set uintptr, values *uintptr)
	cfGetTypeID          func(ref uintptr) uintptr
	cfStringGetTypeID    func() uintptr
	cfNumberGetTypeID    func() uintptr
	cfStringCreate       func(alloc uintptr, text string, encoding uint32) uintptr
	cfStringGetCString   func(ref uintptr, buf *byte, size int, encoding uint32) bool
	cfNumberCreate       func(alloc uintptr, kind int, value *int32) uintptr
	cfNumberGetValue     func(ref uintptr, kind int, value *int32) bool
	cfDictionaryCreate   func(alloc uintptr, capacity int, keyCallbacks, valueCallbacks uintptr) uintptr
	cfDictionarySetValue func(dict, key, value uintptr)

	// RunLoopDefaultMode is kCFRunLoopDefaultMode.
	RunLoopDefaultMode uintptr
	keyCallbacks       uintptr
	valueCallbacks     uintptr
)

// IOKit
var (
	IOHIDManagerCreate                     func(alloc uintptr, options uint32) uintptr
	IOHIDManagerSetDeviceMatching          func(manager, matching uintptr)
	IOHIDManagerCopyDevices                func(manager uintptr) uintptr
	IOHIDDeviceGetProperty                 func(device, key uintptr) uintptr
	IOHIDDeviceOpen                        func(device uintptr, options uint32) int32
	IOHIDDeviceClose                       func(device uintptr, options uint32) int32
	IOHIDDeviceScheduleWithRunLoop         func(device, loop, mode uintptr)
	IOHIDDeviceUnscheduleFromRunLoop       func(device, loop, mode uintptr)
	IOHIDDeviceRegisterInputReportCallback func(device uintptr, report *byte, length int, callback, context uintptr)
	IOHIDDeviceRegisterRemovalCallback     func(device, callback, context uintptr)
	IOHIDDeviceSetReport                   func(device uintptr, kind int, id int, report *byte, length int) int32
)

// CoreGraphics and Accessibility
var (
	CGEventSourceCreate        func(state int32) uintptr
	CGEventCreateKeyboardEvent func(source uintptr, key uint16, down bool) uintptr
	CGEventSetType             func(event uintptr, kind uint32)
	CGEventSetFlags            func(event uintptr, flags uint64)
	CGEventPost                func(tap uint32, event uintptr)
	AXIsProcessTrusted         func() bool
)

func init() {
	cf := load("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	for name, fn := range map[string]any{
		"CFRelease":                 &CFRelease,
		"CFRunLoopGetCurrent":       &CFRunLoopGetCurrent,
		"CFRunLoopRun":              &CFRunLoopRun,
		"CFRunLoopStop":             &CFRunLoopStop,
		"CFSetGetCount":             &CFSetGetCount,
		"CFSetGetValues":            &CFSetGetValues,
		"CFGetTypeID":               &cfGetTypeID,
		"CFStringGetTypeID":         &cfStringGetTypeID,
		"CFNumberGetTypeID":         &cfNumberGetTypeID,
		"CFStringCreateWithCString": &cfStringCreate,
		"CFStringGetCString":        &cfStringGetCString,
		"CFNumberCreate":            &cfNumberCreate,
		"CFNumberGetValue":          &cfNumberGetValue,
		"CFDictionaryCreateMutable": &cfDictionaryCreate,
		"CFDictionarySetValue":      &cfDictionarySetValue,
	} {
		purego.RegisterLibFunc(fn, cf, name)
	}
	RunLoopDefaultMode = *(*uintptr)(symbol(cf, "kCFRunLoopDefaultMode"))
	keyCallbacks = uintptr(symbol(cf, "kCFTypeDictionaryKeyCallBacks"))
	valueCallbacks = uintptr(symbol(cf, "kCFTypeDictionaryValueCallBacks"))

	iokit := load("/System/Library/Frameworks/IOKit.framework/IOKit")
	for name, fn := range map[string]any{
		"IOHIDManagerCreate":                     &IOHIDManagerCreate,
		"IOHIDManagerSetDeviceMatching":          &IOHIDManagerSetDeviceMatching,
		"IOHIDManagerCopyDevices":                &IOHIDManagerCopyDevices,
		"IOHIDDeviceGetProperty":                 &IOHIDDeviceGetProperty,
		"IOHIDDeviceOpen":                        &IOHIDDeviceOpen,
		"IOHIDDeviceClose":                       &IOHIDDeviceClose,
		"IOHIDDeviceScheduleWithRunLoop":         &IOHIDDeviceScheduleWithRunLoop,
		"IOHIDDeviceUnscheduleFromRunLoop":       &IOHIDDeviceUnscheduleFromRunLoop,
		"IOHIDDeviceRegisterInputReportCallback": &IOHIDDeviceRegisterInputReportCallback,
		"IOHIDDeviceRegisterRemovalCallback":     &IOHIDDeviceRegisterRemovalCallback,
		"IOHIDDeviceSetReport":                   &IOHIDDeviceSetReport,
	} {
		purego.RegisterLibFunc(fn, iokit, name)
	}

	services := load("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
	for name, fn := range map[string]any{
		"CGEventSourceCreate":        &CGEventSourceCreate,
		"CGEventCreateKeyboardEvent": &CGEventCreateKeyboardEvent,
		"CGEventSetType":             &CGEventSetType,
		"CGEventSetFlags":            &CGEventSetFlags,
		"CGEventPost":                &CGEventPost,
		"AXIsProcessTrusted":         &AXIsProcessTrusted,
	} {
		purego.RegisterLibFunc(fn, services, name)
	}
}

var procPidinfo func(pid int32, flavor int32, arg uint64, buffer *byte, size int32) int32

func init() {
	purego.RegisterLibFunc(&procPidinfo, load("/usr/lib/libSystem.B.dylib"), "proc_pidinfo")
}

// ProcessCWD is the working directory of a process, or "".
func ProcessCWD(pid int) string {
	// struct proc_vnodepathinfo opens with the working directory's
	// vnode_info_path: a 152-byte vnode_info, then the path.
	const (
		vnodePathInfo = 9 // PROC_PIDVNODEPATHINFO
		vnodeInfoSize = 152
		maxPath       = 1024
	)
	buf := make([]byte, 2*(vnodeInfoSize+maxPath))
	if procPidinfo(int32(pid), vnodePathInfo, 0, &buf[0], int32(len(buf))) <= 0 {
		return ""
	}
	path := buf[vnodeInfoSize : vnodeInfoSize+maxPath]
	return string(path[:bytes.IndexByte(path, 0)])
}

func load(path string) uintptr {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	return lib
}

func symbol(lib uintptr, name string) unsafe.Pointer {
	addr, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(err)
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// String makes a CFString; the caller releases it.
func String(text string) uintptr {
	return cfStringCreate(0, text, utf8)
}

// Int makes a CFNumber; the caller releases it.
func Int(value int32) uintptr {
	return cfNumberCreate(0, sInt32, &value)
}

// Dictionary makes a CFDictionary of the given pairs; the caller releases it.
func Dictionary(pairs map[string]int32) uintptr {
	dict := cfDictionaryCreate(0, 0, keyCallbacks, valueCallbacks)
	for k, v := range pairs {
		key, value := String(k), Int(v)
		cfDictionarySetValue(dict, key, value)
		CFRelease(key)
		CFRelease(value)
	}
	return dict
}

// Values lists a CFSet's members.
func Values(set uintptr) []uintptr {
	n := CFSetGetCount(set)
	if n == 0 {
		return nil
	}
	values := make([]uintptr, n)
	CFSetGetValues(set, &values[0])
	return values
}

// GoString reads a CFString.
func GoString(ref uintptr) (string, bool) {
	if ref == 0 || cfGetTypeID(ref) != cfStringGetTypeID() {
		return "", false
	}
	buf := make([]byte, 256)
	if !cfStringGetCString(ref, &buf[0], len(buf), utf8) {
		return "", false
	}
	return string(buf[:bytes.IndexByte(buf, 0)]), true
}

// GoInt reads a CFNumber.
func GoInt(ref uintptr) (int32, bool) {
	var n int32
	if ref == 0 || cfGetTypeID(ref) != cfNumberGetTypeID() {
		return 0, false
	}
	return n, cfNumberGetValue(ref, sInt32, &n)
}
