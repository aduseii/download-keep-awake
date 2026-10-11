//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const currentConfigVersion = 3

type Config struct {
	Help          string `json:"_help"`
	ConfigVersion int    `json:"configVersion"`

	// 1. Any download over the network (browsers, Steam, Windows Update, torrents...)
	DetectNetwork        bool    `json:"detectNetwork"`
	NetworkMinSpeedKBps  float64 `json:"networkMinSpeedKBps"`
	NetworkWindowSeconds int     `json:"networkWindowSeconds"`

	// 2. In-progress download files in your Downloads folder (and others you add)
	DetectPartialFiles bool     `json:"detectPartialFiles"`
	WatchFolders       []string `json:"watchFolders"`
	PartialExtensions  []string `json:"partialExtensions"`

	// 3. Download manager programs
	DetectApps       bool     `json:"detectApps"`
	DownloadManagers []string `json:"downloadManagers"`
	MinSpeedKBps     float64  `json:"minSpeedKBps"`

	GraceSeconds  int  `json:"graceSeconds"`
	CheckSeconds  int  `json:"checkSeconds"`
	KeepScreenOn  bool `json:"keepScreenOn"`
	Notifications bool `json:"notifications"`

	CheckForUpdates    bool `json:"checkForUpdates"`
	AutoInstallUpdates bool `json:"autoInstallUpdates"`
}

const configHelp = "The PC stays awake while ANY of the enabled detectors sees a download. " +
	"detectNetwork: any app downloading faster than networkMinSpeedKBps (averaged over networkWindowSeconds). " +
	"detectPartialFiles: in-progress download files (e.g. .crdownload, .part) still growing in watchFolders (%DOWNLOADS% = your Downloads folder). " +
	"detectApps: the download manager programs in downloadManagers moving more than minSpeedKBps (see Task Manager > Details for names). " +
	"graceSeconds: how long to stay awake after downloading stops or stalls. keepScreenOn: also keep the display on. " +
	"checkForUpdates / autoInstallUpdates: look for new versions on GitHub every 6 hours and install them automatically (never during a download). " +
	"Save this file and changes apply within a few seconds."

func defaultDownloadManagers() []string {
	return []string{
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
		"persepolis",   // Persepolis
	}
}

func defaultConfig() Config {
	return Config{
		Help:                 configHelp,
		ConfigVersion:        currentConfigVersion,
		DetectNetwork:        true,
		NetworkMinSpeedKBps:  150,
		NetworkWindowSeconds: 15,
		DetectPartialFiles:   true,
		WatchFolders:         []string{"%DOWNLOADS%"},
		PartialExtensions: []string{
			".crdownload",  // Chrome, Edge, Brave, Vivaldi
			".part",        // Firefox, many others
			".partial",     // older Edge / Internet Explorer
			".download",    // Safari-style
			".opdownload",  // Opera
			".!qB",         // qBittorrent (when "append .!qB" is on)
			".!ut",         // uTorrent
			".!bt",         // BitTorrent
			".bc!",         // BitComet
			".aria2",       // aria2 / Motrix control files
			".fdmdownload", // Free Download Manager
			".downloading",
		},
		DetectApps:       true,
		DownloadManagers: defaultDownloadManagers(),
		MinSpeedKBps:     64,
		GraceSeconds:     90,
		CheckSeconds:     3,
		KeepScreenOn:     false,
		Notifications:    true,

		CheckForUpdates:    true,
		AutoInstallUpdates: true,
	}
}

func (c *Config) sanitise() {
	d := defaultConfig()
	c.Help = configHelp
	if c.NetworkMinSpeedKBps <= 0 {
		c.NetworkMinSpeedKBps = d.NetworkMinSpeedKBps
	}
	if c.NetworkWindowSeconds < 3 {
		c.NetworkWindowSeconds = 3
	}
	if c.NetworkWindowSeconds > 120 {
		c.NetworkWindowSeconds = 120
	}
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

// migrate upgrades a config saved by an older version. Returns true if it changed.
func (c *Config) migrate() bool {
	if c.ConfigVersion >= currentConfigVersion {
		return false
	}
	if c.ConfigVersion >= 2 {
		c.ConfigVersion = currentConfigVersion // v2 -> v3 only added update settings (defaults on)
		return true
	}
	// v1 only had download managers: keep the user's list and add any new defaults.
	have := map[string]bool{}
	for _, n := range c.DownloadManagers {
		have[strings.ToLower(n)] = true
	}
	for _, n := range defaultDownloadManagers() {
		if !have[strings.ToLower(n)] {
			c.DownloadManagers = append(c.DownloadManagers, n)
		}
	}
	c.ConfigVersion = currentConfigVersion
	return true
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
	c.ConfigVersion = 0 // a file without the key is from v1
	if err := json.Unmarshal(data, &c); err != nil {
		return defaultConfig(), false, err
	}
	c.sanitise()
	if c.migrate() {
		_ = saveConfig(path, c)
	}
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
