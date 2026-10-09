//go:build windows

//go:generate rsrc -manifest assets/app.manifest -ico assets/icon.ico -arch amd64 -o rsrc_windows_amd64.syso
//go:generate rsrc -manifest assets/app.manifest -ico assets/icon.ico -arch arm64 -o rsrc_windows_arm64.syso

// Download Keep-Awake: a tiny Windows tray app that stops the PC from sleeping
// or hibernating while a download manager is actively downloading.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var version = "1.0.0" // overridden at build time with -ldflags "-X main.version=..."

// repoURL is set at build time by the GitHub workflow to this repository's page.
var repoURL = "https://github.com/konohafiles/download-keep-awake"

const (
	appName   = "Download Keep-Awake"
	timerID   = 1
	trayID    = 1
	className = "DownloadKeepAwakeWindow"
)

const (
	menuStatus = iota + 1
	menuKeepAwakeNow
	menuPause
	menuStartup
	menuSettings
	menuAbout
	menuExit
)

type state int

const (
	stIdle        state = iota // no download manager running
	stDMIdle                   // download manager open, nothing downloading
	stDownloading              // keeping the PC awake for a download
	stManual                   // user chose "keep awake now"
	stPaused                   // user paused the app
)

type app struct {
	hwnd           uintptr
	taskbarCreated uint32
	icons          map[state]uintptr
	cfg            Config
	cfgPath        string
	cfgMod         time.Time
	mon            *ioMonitor
	lastActive     time.Time
	st             state
	stInit         bool
	manual         bool
	paused         bool
	status         string
	awakeApplied   uintptr
}

var (
	a          = &app{}
	wndProcPtr = syscall.NewCallback(wndProc)
	mutex      uintptr
)

func init() {
	// The window, timer and SetThreadExecutionState are all tied to one OS thread.
	runtime.LockOSThread()
}

func main() {
	name := "Local\\DownloadKeepAwake_SingleInstance"
	h, _, err := procCreateMutexW.Call(0, 0, wptr(name))
	mutex = h
	if errno, ok := err.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
		messageBox(appName+" is already running.\n\nLook for its round icon in the system tray (click the ^ arrow next to the clock).",
			appName, MB_OK|MB_ICONINFORMATION)
		return
	}

	a.cfgPath = configPath()
	cfg, firstRun, cfgErr := loadConfig(a.cfgPath)
	a.cfg = cfg
	a.cfgMod = modTime(a.cfgPath)
	a.mon = newIOMonitor()
	a.status = "Starting..."

	if err := createWindow(); err != nil {
		messageBox("Could not start: "+err.Error(), appName, MB_OK|MB_ICONWARNING)
		return
	}

	size := trayIconSize()
	a.icons = map[state]uintptr{
		stIdle:        makeIcon(size, colorIdle),
		stDMIdle:      makeIcon(size, colorIdle),
		stDownloading: makeIcon(size, colorActive),
		stManual:      makeIcon(size, colorManual),
		stPaused:      makeIcon(size, colorPaused),
	}
	addTrayIcon()

	if firstRun {
		if messageBox(appName+" is now running in your system tray.\n\n"+
			"Whenever your download manager is downloading, your PC won't go to sleep or hibernate. "+
			"When downloads finish, your normal sleep settings come back automatically.\n\n"+
			"Start "+appName+" automatically when Windows starts?",
			appName, MB_YESNO|MB_ICONQUESTION) == IDYES {
			setStartup(true)
		}
	} else if startupEnabled() {
		setStartup(true) // refresh the path in case the .exe was moved
	}
	if cfgErr != nil {
		notify("Settings file problem", "Couldn't read config.json, using defaults: "+cfgErr.Error())
	}

	tick()
	procSetTimer.Call(a.hwnd, timerID, uintptr(a.cfg.CheckSeconds*1000), 0)

	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	setAwake(false, false)
}

func createWindow() error {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   wndProcPtr,
		HInstance:     hInst,
		LpszClassName: wstr(className),
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	// A hidden top-level window (not message-only) so it also receives the
	// "TaskbarCreated" broadcast and can restore the icon if Explorer restarts.
	hwnd, _, err := procCreateWindowExW.Call(0, wptr(className), wptr(appName), 0,
		0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		return err
	}
	a.hwnd = hwnd
	tc, _, _ := procRegisterWindowMessageW.Call(wptr("TaskbarCreated"))
	a.taskbarCreated = uint32(tc)
	return nil
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_TRAYCALLBACK:
		switch uint32(lParam & 0xFFFF) {
		case WM_RBUTTONUP, WM_LBUTTONUP:
			showMenu()
		}
		return 0
	case WM_TIMER:
		if wParam == timerID {
			tick()
		}
		return 0
	case WM_ENDSESSION:
		if wParam != 0 {
			setAwake(false, false)
			removeTrayIcon()
		}
		return 0
	case WM_DESTROY:
		procKillTimer.Call(hwnd, timerID)
		setAwake(false, false)
		removeTrayIcon()
		procPostQuitMessage.Call(0)
		return 0
	}
	if a.taskbarCreated != 0 && uint32(msg) == a.taskbarCreated {
		addTrayIcon()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// ---------- core logic ----------

func tick() {
	reloadConfigIfChanged()

	running, bps := a.mon.poll(a.cfg.DownloadManagers)
	now := time.Now()
	if len(running) > 0 && bps >= a.cfg.MinSpeedKBps*1024 {
		a.lastActive = now
	}
	downloading := len(running) > 0 && !a.lastActive.IsZero() &&
		now.Sub(a.lastActive) < time.Duration(a.cfg.GraceSeconds)*time.Second

	var next state
	switch {
	case a.paused:
		next = stPaused
		a.status = "Paused - normal sleep settings apply"
	case a.manual:
		next = stManual
		a.status = "Keeping PC awake (turned on manually)"
	case downloading:
		next = stDownloading
		if bps >= a.cfg.MinSpeedKBps*1024 {
			a.status = fmt.Sprintf("Keeping PC awake - %s downloading at %s", strings.Join(running, ", "), formatSpeed(bps))
		} else {
			left := time.Duration(a.cfg.GraceSeconds)*time.Second - now.Sub(a.lastActive)
			a.status = fmt.Sprintf("Keeping PC awake - download paused/stalled (%ds grace left)", int(left.Seconds()+0.5))
		}
	case len(running) > 0:
		next = stDMIdle
		a.status = strings.Join(running, ", ") + " is open - no active download"
	default:
		next = stIdle
		a.status = "Idle - no download manager running"
	}

	awake := next == stDownloading || next == stManual
	setAwake(awake, a.cfg.KeepScreenOn)

	if !a.stInit || next != a.st {
		prev := a.st
		a.st = next
		if a.stInit {
			if next == stDownloading && prev != stManual {
				notify("Download detected", "Your PC will stay awake until the download finishes.")
			} else if prev == stDownloading && (next == stIdle || next == stDMIdle) {
				notify("Downloads finished", "Your normal sleep settings are back.")
			}
		}
		a.stInit = true
	}
	updateTrayIcon()
}

func setAwake(on, screen bool) {
	flags := uintptr(ES_CONTINUOUS)
	if on {
		flags |= ES_SYSTEM_REQUIRED
		if screen {
			flags |= ES_DISPLAY_REQUIRED
		}
	}
	if flags != a.awakeApplied {
		procSetThreadExecutionState.Call(flags)
		a.awakeApplied = flags
	}
}

func reloadConfigIfChanged() {
	mt := modTime(a.cfgPath)
	if mt.Equal(a.cfgMod) {
		return
	}
	a.cfgMod = mt
	cfg, _, err := loadConfig(a.cfgPath)
	if err != nil {
		notify("Settings not applied", "config.json has a mistake, so the previous settings are still in use. "+err.Error())
		return
	}
	oldInterval := a.cfg.CheckSeconds
	a.cfg = cfg
	if cfg.CheckSeconds != oldInterval && a.hwnd != 0 {
		procSetTimer.Call(a.hwnd, timerID, uintptr(cfg.CheckSeconds*1000), 0)
	}
}

func formatSpeed(bps float64) string {
	kb := bps / 1024
	if kb >= 1024 {
		return fmt.Sprintf("%.1f MB/s", kb/1024)
	}
	return fmt.Sprintf("%.0f KB/s", kb)
}

// ---------- tray icon ----------

func baseNID() NOTIFYICONDATAW {
	return NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   a.hwnd,
		UID:    trayID,
	}
}

func addTrayIcon() {
	nid := baseNID()
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYCALLBACK
	nid.HIcon = a.icons[a.st]
	copyUTF16(nid.SzTip[:], tooltip())
	procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
}

func updateTrayIcon() {
	nid := baseNID()
	nid.UFlags = NIF_ICON | NIF_TIP
	nid.HIcon = a.icons[a.st]
	copyUTF16(nid.SzTip[:], tooltip())
	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

func removeTrayIcon() {
	nid := baseNID()
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}

func tooltip() string {
	t := appName + "\n" + a.status
	if len([]rune(t)) > 127 {
		t = string([]rune(t)[:124]) + "..."
	}
	return t
}

func notify(title, text string) {
	if !a.cfg.Notifications || a.hwnd == 0 {
		return
	}
	nid := baseNID()
	nid.UFlags = NIF_INFO
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], text)
	nid.DwInfoFlags = NIIF_INFO | NIIF_NOSOUND
	procShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

func showMenu() {
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)

	add := func(id int, text string, flags uintptr) {
		procAppendMenuW.Call(m, MF_STRING|flags, uintptr(id), wptr(text))
	}
	checked := func(b bool) uintptr {
		if b {
			return MF_CHECKED
		}
		return 0
	}
	sep := func() { procAppendMenuW.Call(m, MF_SEPARATOR, 0, 0) }

	add(menuStatus, a.status, MF_GRAYED)
	sep()
	add(menuKeepAwakeNow, "Keep awake now (ignore downloads)", checked(a.manual))
	add(menuPause, "Pause (allow sleep)", checked(a.paused))
	sep()
	add(menuStartup, "Start with Windows", checked(startupEnabled()))
	add(menuSettings, "Settings...", 0)
	add(menuAbout, "About", 0)
	sep()
	add(menuExit, "Exit", 0)

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(a.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(m, TPM_RIGHTBUTTON|TPM_RETURNCMD|TPM_BOTTOMALIGN,
		uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	procPostMessageW.Call(a.hwnd, WM_NULL, 0, 0)

	switch int(cmd) {
	case menuKeepAwakeNow:
		a.manual = !a.manual
		if a.manual {
			a.paused = false
		}
		tick()
	case menuPause:
		a.paused = !a.paused
		if a.paused {
			a.manual = false
		}
		tick()
	case menuStartup:
		if err := setStartup(!startupEnabled()); err != nil {
			messageBox("Couldn't change the startup setting:\n"+err.Error(), appName, MB_OK|MB_ICONWARNING)
		}
	case menuSettings:
		openWith("notepad.exe", `"`+a.cfgPath+`"`)
	case menuAbout:
		about := fmt.Sprintf("%s %s\n\nKeeps your PC from sleeping or hibernating while your download manager is downloading.\n\n"+
			"Icon colours:\n  Green - keeping PC awake for a download\n  Blue - kept awake manually\n  Grey - idle, normal sleep allowed\n  Amber - paused\n\n"+
			"Settings file:\n%s\n\nOpen the project page on GitHub?", appName, version, a.cfgPath)
		if messageBox(about, "About "+appName, MB_YESNO|MB_ICONINFORMATION) == IDYES {
			openURL(repoURL)
		}
	case menuExit:
		procDestroyWindow.Call(a.hwnd)
	}
}
