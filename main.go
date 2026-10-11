//go:build windows

//go:generate rsrc -manifest assets/app.manifest -ico assets/icon.ico -arch amd64 -o rsrc_windows_amd64.syso
//go:generate rsrc -manifest assets/app.manifest -ico assets/icon.ico -arch arm64 -o rsrc_windows_arm64.syso

// Download Keep-Awake: a tiny Windows tray app that stops the PC from sleeping
// or hibernating while anything is downloading.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var version = "1.1.0" // overridden at build time with -ldflags "-X main.version=..."

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
	menuDetectNetwork
	menuDetectFiles
	menuDetectApps
	menuKeepAwakeNow
	menuPause
	menuStartup
	menuSettings
	menuAbout
	menuExit
)

type state int

const (
	stIdle        state = iota // nothing downloading, normal sleep allowed
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
	apps           *ioMonitor
	net            *netMonitor
	files          *partialMonitor
	lastActive     time.Time
	lastReason     string
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
	a.apps = newIOMonitor()
	a.net = newNetMonitor()
	a.files = newPartialMonitor()
	a.status = "Starting..."

	if err := createWindow(); err != nil {
		messageBox("Could not start: "+err.Error(), appName, MB_OK|MB_ICONWARNING)
		return
	}

	size := trayIconSize()
	a.icons = map[state]uintptr{
		stIdle:        makeIcon(size, colorIdle),
		stDownloading: makeIcon(size, colorActive),
		stManual:      makeIcon(size, colorManual),
		stPaused:      makeIcon(size, colorPaused),
	}
	addTrayIcon()

	if firstRun {
		if messageBox(appName+" is now running in your system tray.\n\n"+
			"Whenever something is downloading (browser downloads, download managers, Steam and other game launchers, "+
			"torrents, Windows Update...), your PC won't go to sleep or hibernate. "+
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
	c := &a.cfg
	now := time.Now()

	// Always sample the network so the speed shown in the menu stays accurate
	// and the averaging window is already full when detection is switched on.
	netAvg, netNow := a.net.poll(time.Duration(c.NetworkWindowSeconds) * time.Second)

	var reasons []string
	if c.DetectNetwork && netAvg >= c.NetworkMinSpeedKBps*1024 {
		reasons = append(reasons, "downloading at "+formatSpeed(netNow))
	}
	if c.DetectPartialFiles {
		if files := a.files.poll(c.WatchFolders, c.PartialExtensions); len(files) > 0 {
			label := shorten(files[0], 40)
			if len(files) > 1 {
				label += fmt.Sprintf(" +%d more", len(files)-1)
			}
			reasons = append(reasons, "saving "+label)
		}
	}
	if c.DetectApps {
		running, bps := a.apps.poll(c.DownloadManagers)
		if len(running) > 0 && bps >= c.MinSpeedKBps*1024 {
			reasons = append(reasons, strings.Join(running, ", ")+" active")
		}
	}

	if len(reasons) > 0 {
		a.lastActive = now
		a.lastReason = strings.Join(reasons, " · ")
	}
	downloading := !a.lastActive.IsZero() && now.Sub(a.lastActive) < time.Duration(c.GraceSeconds)*time.Second

	var next state
	switch {
	case a.paused:
		next = stPaused
		a.status = "Paused - normal sleep settings apply"
	case a.manual:
		next = stManual
		a.status = "Keeping PC awake (turned on manually)"
	case downloading && len(reasons) > 0:
		next = stDownloading
		a.status = "Keeping PC awake - " + a.lastReason
	case downloading:
		next = stDownloading
		left := time.Duration(c.GraceSeconds)*time.Second - now.Sub(a.lastActive)
		a.status = fmt.Sprintf("Keeping PC awake - download stalled or between files (%ds left)", int(left.Seconds()+0.5))
	case !c.DetectNetwork && !c.DetectPartialFiles && !c.DetectApps:
		next = stIdle
		a.status = "All detection is switched off - normal sleep allowed"
	default:
		next = stIdle
		a.status = "Idle - no downloads (network " + formatSpeed(netNow) + ")"
	}

	setAwake(next == stDownloading || next == stManual, c.KeepScreenOn)

	if !a.stInit || next != a.st {
		prev := a.st
		a.st = next
		if a.stInit {
			if next == stDownloading && prev == stIdle {
				notify("Download detected", "Your PC will stay awake until downloads finish.\n"+shorten(a.lastReason, 120))
			} else if prev == stDownloading && next == stIdle {
				notify("Downloads finished", "Your normal sleep settings are back.")
			}
		}
		a.stInit = true
	}
	updateTrayIcon()
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
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
	cfg, _, err := loadConfig(a.cfgPath)
	a.cfgMod = modTime(a.cfgPath) // loadConfig may have re-saved a migrated file
	if err != nil {
		notify("Settings not applied", "config.json has a mistake, so the previous settings are still in use. "+err.Error())
		return
	}
	applyConfig(cfg)
}

func applyConfig(cfg Config) {
	oldInterval := a.cfg.CheckSeconds
	a.cfg = cfg
	if cfg.CheckSeconds != oldInterval && a.hwnd != 0 {
		procSetTimer.Call(a.hwnd, timerID, uintptr(cfg.CheckSeconds*1000), 0)
	}
}

// toggle flips one detection setting from the tray menu and saves it.
func toggle(field *bool) {
	*field = !*field
	if err := saveConfig(a.cfgPath, a.cfg); err != nil {
		messageBox("Couldn't save the setting:\n"+err.Error(), appName, MB_OK|MB_ICONWARNING)
	}
	a.cfgMod = modTime(a.cfgPath)
	tick()
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
	return shorten(appName+"\n"+a.status, 127)
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

	add(menuStatus, shorten(a.status, 100), MF_GRAYED)
	sep()
	add(menuDetectNetwork, "Detect any download (network traffic)", checked(a.cfg.DetectNetwork))
	add(menuDetectFiles, "Detect browser && torrent downloads (Downloads folder)", checked(a.cfg.DetectPartialFiles))
	add(menuDetectApps, "Detect download manager apps", checked(a.cfg.DetectApps))
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
	case menuDetectNetwork:
		toggle(&a.cfg.DetectNetwork)
	case menuDetectFiles:
		toggle(&a.cfg.DetectPartialFiles)
	case menuDetectApps:
		toggle(&a.cfg.DetectApps)
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
		about := fmt.Sprintf("%s %s\n\nKeeps your PC from sleeping or hibernating while anything is downloading: "+
			"browsers, download managers, Steam and other game launchers, torrents, Windows Update and more.\n\n"+
			"Icon colours:\n  Green - keeping PC awake for a download\n  Blue - kept awake manually\n  Grey - idle, normal sleep allowed\n  Amber - paused\n\n"+
			"Settings file:\n%s\n\nOpen the project page on GitHub?", appName, version, a.cfgPath)
		if messageBox(about, "About "+appName, MB_YESNO|MB_ICONINFORMATION) == IDYES {
			openURL(repoURL)
		}
	case menuExit:
		procDestroyWindow.Call(a.hwnd)
	}
}
