//go:build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Windows taskbar progress via the ITaskbarList3 COM interface — the same
// green fill mpv shows on its taskbar button while playing. COM objects are
// apartment threaded, so all COM use happens on one locked OS thread;
// setTaskbarProgress just posts the latest value to it.

var (
	tbOle32                     = syscall.NewLazyDLL("ole32.dll")
	tbProcCoInitialize          = tbOle32.NewProc("CoInitialize")
	tbProcCoCreateInstance      = tbOle32.NewProc("CoCreateInstance")
	tbUser32                    = syscall.NewLazyDLL("user32.dll")
	tbProcEnumWindows           = tbUser32.NewProc("EnumWindows")
	tbProcGetWindowThreadProcID = tbUser32.NewProc("GetWindowThreadProcessId")
	tbProcIsWindowVisible       = tbUser32.NewProc("IsWindowVisible")
	tbKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	tbProcGetCurrentProcessID   = tbKernel32.NewProc("GetCurrentProcessId")
)

type tbGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	// CLSID_TaskbarList / IID_ITaskbarList3.
	tbCLSIDTaskbarList = tbGUID{0x56FDF344, 0xFD6D, 0x11D0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	tbIIDTaskbarList3  = tbGUID{0xEA1AFB91, 0x9E28, 0x4B86, [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
)

const (
	tbClsctxInprocServer = 0x1
	tbpfNoProgress       = 0x0
	tbpfNormal           = 0x2
)

// tbVtbl is the ITaskbarList3 vtable up to the two methods used here
// (IUnknown + ITaskbarList + ITaskbarList2 precede them; later methods are
// irrelevant and omitted).
type tbVtbl struct {
	QueryInterface       uintptr
	AddRef               uintptr
	Release              uintptr
	HrInit               uintptr
	AddTab               uintptr
	DeleteTab            uintptr
	ActivateTab          uintptr
	SetActiveAlt         uintptr
	MarkFullscreenWindow uintptr
	SetProgressValue     uintptr
	SetProgressState     uintptr
}

type tbTaskbarList struct{ vtbl *tbVtbl }

var (
	tbOnce sync.Once
	tbCh   chan float64
)

// setTaskbarProgress shows overall progress on the app's taskbar button:
// 0..1 fills the bar, a negative value clears it. Safe to call from any
// goroutine; when updates outpace the COM worker only the latest value is
// kept. A no-op if the taskbar COM object is unavailable.
func setTaskbarProgress(f float64) {
	tbOnce.Do(func() {
		tbCh = make(chan float64, 1)
		go tbWorker()
	})
	for {
		select {
		case tbCh <- f:
			return
		default:
		}
		// Full: drop the stale value and retry with the fresh one.
		select {
		case <-tbCh:
		default:
		}
	}
}

// tbFoundWindow receives the enumeration result; only the COM worker thread
// touches it.
var tbFoundWindow uintptr

var tbEnumCallback = syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
	var pid uint32
	tbProcGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	self, _, _ := tbProcGetCurrentProcessID.Call()
	if uintptr(pid) != self {
		return 1 // keep enumerating
	}
	if vis, _, _ := tbProcIsWindowVisible.Call(hwnd); vis == 0 {
		return 1
	}
	tbFoundWindow = hwnd
	return 0 // stop
})

func tbWorker() {
	runtime.LockOSThread()
	if hr, _, _ := tbProcCoInitialize.Call(0); int32(hr) < 0 {
		return
	}
	var tl *tbTaskbarList
	hr, _, _ := tbProcCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&tbCLSIDTaskbarList)),
		0,
		tbClsctxInprocServer,
		uintptr(unsafe.Pointer(&tbIIDTaskbarList3)),
		uintptr(unsafe.Pointer(&tl)),
	)
	if int32(hr) < 0 || tl == nil {
		return
	}
	self := uintptr(unsafe.Pointer(tl))
	syscall.SyscallN(tl.vtbl.HrInit, self)

	// The Wails window is found lazily (it may not exist yet on the very
	// first update) and cached for the app's lifetime.
	var hwnd uintptr
	for f := range tbCh {
		if hwnd == 0 {
			tbFoundWindow = 0
			tbProcEnumWindows.Call(tbEnumCallback, 0)
			hwnd = tbFoundWindow
			if hwnd == 0 {
				continue // not created yet; the next update retries
			}
		}
		if f < 0 {
			syscall.SyscallN(tl.vtbl.SetProgressState, self, hwnd, tbpfNoProgress)
			continue
		}
		if f > 1 {
			f = 1
		}
		syscall.SyscallN(tl.vtbl.SetProgressState, self, hwnd, tbpfNormal)
		syscall.SyscallN(tl.vtbl.SetProgressValue, self, hwnd, uintptr(uint64(f*1000)), 1000)
	}
}
