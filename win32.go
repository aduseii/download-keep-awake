//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procGetMessageW             = user32.NewProc("GetMessageW")
	procTranslateMessage        = user32.NewProc("TranslateMessage")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procPostQuitMessage         = user32.NewProc("PostQuitMessage")
	procPostMessageW            = user32.NewProc("PostMessageW")
	procSetTimer                = user32.NewProc("SetTimer")
	procKillTimer               = user32.NewProc("KillTimer")
	procCreatePopupMenu         = user32.NewProc("CreatePopupMenu")
	procAppendMenuW             = user32.NewProc("AppendMenuW")
	procTrackPopupMenu          = user32.NewProc("TrackPopupMenu")
	procDestroyMenu             = user32.NewProc("DestroyMenu")
	procSetMenuDefaultItem      = user32.NewProc("SetMenuDefaultItem")
	procSetForegroundWindow     = user32.NewProc("SetForegroundWindow")
	procGetCursorPos            = user32.NewProc("GetCursorPos")
	procMessageBoxW             = user32.NewProc("MessageBoxW")
	procRegisterWindowMessageW  = user32.NewProc("RegisterWindowMessageW")
	procCreateIconIndirect      = user32.NewProc("CreateIconIndirect")
	procDestroyIcon             = user32.NewProc("DestroyIcon")
	procGetSystemMetrics        = user32.NewProc("GetSystemMetrics")
	procSetProcessDpiAwareCtx   = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetModuleHandleW        = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW            = kernel32.NewProc("CreateMutexW")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
	procGetProcessIoCounters    = kernel32.NewProc("GetProcessIoCounters")
	procShellNotifyIconW        = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW           = shell32.NewProc("ShellExecuteW")
	procCreateDIBSection        = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap            = gdi32.NewProc("CreateBitmap")
	procDeleteObject            = gdi32.NewProc("DeleteObject")
	procRegCreateKeyExW         = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueExW          = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW         = advapi32.NewProc("RegDeleteValueW")
)

const (
	WM_DESTROY      = 0x0002
	WM_CLOSE        = 0x0010
	WM_ENDSESSION   = 0x0016
	WM_NULL         = 0x0000
	WM_TIMER        = 0x0113
	WM_LBUTTONUP    = 0x0202
	WM_RBUTTONUP    = 0x0205
	WM_APP          = 0x8000
	WM_TRAYCALLBACK = WM_APP + 1

	NIN_BALLOONUSERCLICK = 0x0400 + 5

	NIM_ADD    = 0
	NIM_MODIFY = 1
	NIM_DELETE = 2

	NIF_MESSAGE = 0x01
	NIF_ICON    = 0x02
	NIF_TIP     = 0x04
	NIF_INFO    = 0x10

	NIIF_INFO    = 0x01
	NIIF_NOSOUND = 0x10

	MF_STRING    = 0x0000
	MF_GRAYED    = 0x0001
	MF_CHECKED   = 0x0008
	MF_SEPARATOR = 0x0800

	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100
	TPM_BOTTOMALIGN = 0x0020

	MB_OK              = 0x00
	MB_YESNO           = 0x04
	MB_ICONINFORMATION = 0x40
	MB_ICONQUESTION    = 0x20
	MB_ICONWARNING     = 0x30
	IDYES              = 6

	SM_CXSMICON = 49

	ES_CONTINUOUS       = 0x80000000
	ES_SYSTEM_REQUIRED  = 0x00000001
	ES_DISPLAY_REQUIRED = 0x00000002

	ERROR_ALREADY_EXISTS = 183

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

	KEY_SET_VALUE = 0x0002
	REG_SZ        = 1

	SW_SHOWNORMAL = 1
)

type POINT struct{ X, Y int32 }

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutVersion  uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         GUID
	HBalloonIcon     uintptr
}

type IO_COUNTERS struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

func wstr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func wptr(s string) uintptr { return uintptr(unsafe.Pointer(wstr(s))) }

// copyUTF16 writes s into a fixed-size UTF-16 buffer, truncating if needed.
func copyUTF16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	for i := range dst {
		dst[i] = 0
	}
	copy(dst, u)
}

func messageBox(text, title string, flags uintptr) int {
	r, _, _ := procMessageBoxW.Call(0, wptr(text), wptr(title), flags)
	return int(r)
}

func openURL(url string) {
	procShellExecuteW.Call(0, wptr("open"), wptr(url), 0, 0, SW_SHOWNORMAL)
}

func openWith(program, args string) {
	procShellExecuteW.Call(0, wptr("open"), wptr(program), wptr(args), 0, SW_SHOWNORMAL)
}
