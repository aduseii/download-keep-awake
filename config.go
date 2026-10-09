//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

type Config struct {
	Help             string   `json:"_help"`
	DownloadManagers []string `json:"downloadManagers"`
	MinSpeedKBps     float64  `json:"minSpeedKBps"`
	GraceSeconds     int      `json:"graceSeconds"`
	CheckSeconds     int      `json:"checkSeconds"`
	KeepScreenOn     bool     `json:"keepScreenOn"`
	Notifications    bool     `json:"notifications"`
}

const configHelp = "downloadManagers: program names to watch (see Task Manager > Details, '.exe' optional). " +
	"minSpeedKBps: activity below this speed counts as idle. " +
	"graceSeconds: how long to stay awake after a download stops or stalls. " +
	"keepScreenOn: also stop the display from turning off. Save this file and changes apply within a few seconds."

func defaultConfig() Config {
	return Config{
		Help: configHelp,
		DownloadManagers: []string{
			"fdm",          // Free Download Manager
			"IDMan",        // Internet Download Manager
			"NeatDM",       // Neat Download Manager
			"xdm-app",      // Xtreme Download Manager
			"xdman",        // Xtreme Download Manager (older)
			"JDownloader2", // JDownloader
			"Motrix",       // Motrix
			"EagleGet",     // EagleGet
			"uget-gtk",     // uGet
			"FlashGet",     // FlashGet
			"aria2c",       // aria2
		},
		MinSpeedKBps:  64,
		GraceSeconds:  90,
		CheckSeconds:  3,
		KeepScreenOn:  false,
		Notifications: true,
	}
}

func (c *Config) sanitise() {
	d := defaultConfig()
	c.Help = configHelp
	if c.MinSpeedKBps <= 0 {
		c.MinSpeedKBps = d.MinSpeedKBps
	}
	if c.GraceSeconds < 5 {
		c.GraceSeconds = 5
	}
	if c.CheckSeconds < 1 {
		c.CheckSeconds = 1
	}
	if c.CheckSeconds > 60 {
		c.CheckSeconds = 60
	}
}

func configPath() string {
	dir, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "DownloadKeepAwake", "config.json")
}

// loadConfig returns the config, whether this is the first run, and any parse error.
func loadConfig(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := defaultConfig()
		return c, true, saveConfig(path, c)
	}
	if err != nil {
		return defaultConfig(), false, err
	}
	c := defaultConfig()
	if err := json.Unmarshal(data, &c); err != nil {
		return defaultConfig(), false, err
	}
	c.sanitise()
	return c, false, nil
}

func saveConfig(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func modTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// ---- "Start with Windows" (HKCU\...\Run) ----

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "DownloadKeepAwake"
)

func startupEnabled() bool {
	var k syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wstr(runKeyPath), 0, syscall.KEY_READ, &k) != nil {
		return false
	}
	defer syscall.RegCloseKey(k)
	var typ, size uint32
	return syscall.RegQueryValueEx(k, wstr(runValueName), nil, &typ, nil, &size) == nil
}

func setStartup(on bool) error {
	var k syscall.Handle
	r, _, _ := procRegCreateKeyExW.Call(uintptr(syscall.HKEY_CURRENT_USER), wptr(runKeyPath), 0, 0, 0,
		KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&k)), 0)
	if r != 0 {
		return syscall.Errno(r)
	}
	defer syscall.RegCloseKey(k)

	if !on {
		r, _, _ = procRegDeleteValueW.Call(uintptr(k), wptr(runValueName))
		if r != 0 && syscall.Errno(r) != syscall.ERROR_FILE_NOT_FOUND {
			return syscall.Errno(r)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd, _ := syscall.UTF16FromString(`"` + exe + `"`)
	r, _, _ = procRegSetValueExW.Call(uintptr(k), wptr(runValueName), 0, REG_SZ,
		uintptr(unsafe.Pointer(&cmd[0])), uintptr(len(cmd)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}
