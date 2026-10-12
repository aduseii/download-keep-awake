//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Per-user install, the same way Crave's installer works: no admin rights,
// the app lives in %LOCALAPPDATA%\Programs\Download Keep-Awake where it can
// update itself, gets a Start menu shortcut and shows up in
// Settings > Apps > Installed apps with an Uninstall button.
//
// The downloaded .exe is the installer: running it copies itself into place
// and starts the installed copy. Running it again later only installs it if
// it's newer than what's installed.

const (
	installDirName = "Download Keep-Awake"
	installExeName = "DownloadKeepAwake.exe"
	uninstallKey   = `Software\Microsoft\Windows\CurrentVersion\Uninstall\DownloadKeepAwake`
	publisher      = "aduseii"

	uninstallFlag = "--uninstall"
	installedFlag = "--installed"
	portableFlag  = "--portable"
)

var (
	ole32Install         = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32Install.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32Install.NewProc("CoCreateInstance")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procRegSetDWORD      = advapi32.NewProc("RegSetValueExW")
	procRegDeleteTreeW   = advapi32.NewProc("RegDeleteTreeW")
	procRegDeleteKeyW    = advapi32.NewProc("RegDeleteKeyW")

	clsidShellLink  = GUID{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = GUID{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = GUID{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

func hasFlag(flag string) bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, flag) {
			return true
		}
	}
	return false
}

func installDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(base, "Programs", installDirName)
}

func installedExe() string { return filepath.Join(installDir(), installExeName) }

func startMenuShortcut() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, appName+".lnk")
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func runningFromInstallDir() bool {
	exe, err := exePath()
	return err == nil && samePath(filepath.Dir(exe), installDir())
}

// ---------- registry helpers ----------

func regOpenCreate(path string) (syscall.Handle, error) {
	var k syscall.Handle
	r, _, _ := procRegCreateKeyExW.Call(uintptr(syscall.HKEY_CURRENT_USER), wptr(path), 0, 0, 0,
		KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&k)), 0)
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return k, nil
}

func regSetString(k syscall.Handle, name, value string) {
	v, _ := syscall.UTF16FromString(value)
	procRegSetValueExW.Call(uintptr(k), wptr(name), 0, REG_SZ, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
}

func regSetDWORD(k syscall.Handle, name string, value uint32) {
	const REG_DWORD = 4
	procRegSetDWORD.Call(uintptr(k), wptr(name), 0, REG_DWORD, uintptr(unsafe.Pointer(&value)), 4)
}

func regGetString(path, name string) string {
	var k syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wstr(path), 0, syscall.KEY_READ, &k) != nil {
		return ""
	}
	defer syscall.RegCloseKey(k)
	buf := make([]uint16, 256)
	n := uint32(len(buf) * 2)
	var typ uint32
	if syscall.RegQueryValueEx(k, wstr(name), nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil || typ != REG_SZ {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// registerInstall (re)writes the Apps & features entry and Start menu shortcut.
// Called on every start of the installed copy so it always shows the current version.
func registerInstall() {
	exe := installedExe()
	if k, err := regOpenCreate(uninstallKey); err == nil {
		regSetString(k, "DisplayName", appName)
		regSetString(k, "DisplayVersion", version)
		regSetString(k, "Publisher", publisher)
		regSetString(k, "DisplayIcon", exe+",0")
		regSetString(k, "InstallLocation", installDir())
		regSetString(k, "UninstallString", `"`+exe+`" `+uninstallFlag)
		regSetString(k, "QuietUninstallString", `"`+exe+`" `+uninstallFlag+" --quiet")
		regSetString(k, "URLInfoAbout", repoURL)
		regSetString(k, "HelpLink", repoURL)
		regSetDWORD(k, "NoModify", 1)
		regSetDWORD(k, "NoRepair", 1)
		if st, err := os.Stat(exe); err == nil {
			regSetDWORD(k, "EstimatedSize", uint32(st.Size()/1024))
		}
		syscall.RegCloseKey(k)
	}
	if _, err := os.Stat(startMenuShortcut()); err != nil {
		createShortcut(startMenuShortcut(), exe, installDir(),
			"Keeps your PC awake while anything is downloading")
	}
}

// ---------- Start menu shortcut via IShellLinkW ----------

func comCall(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func createShortcut(lnkPath, target, workDir, description string) error {
	const (
		COINIT_APARTMENTTHREADED = 0x2
		CLSCTX_INPROC_SERVER     = 0x1
		// IShellLinkW vtable
		slSetDescription      = 7
		slSetWorkingDirectory = 9
		slSetIconLocation     = 17
		slSetPath             = 20
		// IUnknown / IPersistFile vtable
		unkQueryInterface = 0
		unkRelease        = 2
		pfSave            = 6
	)
	procCoInitializeEx.Call(0, COINIT_APARTMENTTHREADED) // fine if already initialised

	var link unsafe.Pointer
	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&link))); r != 0 || link == nil {
		return errors.New("couldn't create shortcut object")
	}
	defer comCall(link, unkRelease)

	comCall(link, slSetPath, wptr(target))
	comCall(link, slSetWorkingDirectory, wptr(workDir))
	comCall(link, slSetDescription, wptr(description))
	comCall(link, slSetIconLocation, wptr(target), 0)

	var pf unsafe.Pointer
	if r := comCall(link, unkQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pf))); r != 0 || pf == nil {
		return errors.New("couldn't save shortcut")
	}
	defer comCall(pf, unkRelease)
	os.MkdirAll(filepath.Dir(lnkPath), 0o755)
	if r := comCall(pf, pfSave, wptr(lnkPath), 1); r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// ---------- install ----------

// closeRunningCopy asks a running instance to exit and waits for it.
func closeRunningCopy() {
	hwnd, _, _ := procFindWindowW.Call(wptr(className), 0)
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		if h, _, _ := procFindWindowW.Call(wptr(className), 0); h == 0 {
			return
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	old := dst + ".old"
	os.Remove(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Rename(old, dst)
		return err
	}
	return nil
}

// maybeInstall runs when the .exe is started from anywhere other than the
// install folder. It returns true if the installed copy was started and this
// process should exit.
func maybeInstall() bool {
	if runningFromInstallDir() || hasFlag(portableFlag) {
		return false
	}
	self, err := exePath()
	if err != nil {
		return false
	}
	target := installedExe()

	installedVer, haveInstalled := parseVersion(regGetString(uninstallKey, "DisplayVersion"))
	if _, err := os.Stat(target); err != nil {
		haveInstalled = false
	}
	ourVer, ourOK := parseVersion(version)

	if !haveInstalled || (ourOK && newer(ourVer, installedVer)) {
		closeRunningCopy()
		if err := os.MkdirAll(installDir(), 0o755); err != nil {
			return false // can't install: just run from where we are
		}
		if err := copyFile(self, target); err != nil {
			messageBox("Couldn't install to "+installDir()+":\n"+err.Error()+"\n\nRunning from the current folder instead (automatic updates may not work there).",
				appName, MB_OK|MB_ICONWARNING)
			return false
		}
	}
	// Installed copy is current (or newer): hand over to it. If it's already
	// running it will say so.
	cmd := exec.Command(target, installedFlag)
	cmd.Dir = installDir()
	if err := cmd.Start(); err != nil {
		return false
	}
	return true
}

// ---------- uninstall ----------

func uninstall() {
	quiet := hasFlag("--quiet")
	if !quiet && messageBox("Remove "+appName+" from this PC?\n\nYour normal Windows sleep settings will apply again.",
		appName, MB_YESNO|MB_ICONQUESTION) != IDYES {
		return
	}
	closeRunningCopy()
	setStartup(false)
	os.Remove(startMenuShortcut())
	procRegDeleteTreeW.Call(uintptr(syscall.HKEY_CURRENT_USER), wptr(uninstallKey))
	procRegDeleteKeyW.Call(uintptr(syscall.HKEY_CURRENT_USER), wptr(uninstallKey))

	if !quiet {
		messageBox(appName+" has been removed.", appName, MB_OK|MB_ICONINFORMATION)
	}
	// A running .exe can't delete its own folder, so let cmd do it a moment after we exit.
	dir := installDir()
	if exe, err := exePath(); err == nil && samePath(filepath.Dir(exe), dir) {
		script := `for /l %i in (1,1,15) do (ping 127.0.0.1 -n 2 >nul & rmdir /s /q "` + dir + `" 2>nul & if not exist "` + dir + `" exit)`
		cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
		// Raw command line: Go's default quoting (\") isn't understood by cmd.exe.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /c ` + script, HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
		cmd.Start()
	}
}
