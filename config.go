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

const currentConfigVersion = 7

type Config struct {
	Help          string `json:"_help"`
	ConfigVersion int    `json:"configVersion"`

	// 1. Browsers: Microsoft Edge and Google Chrome (unfinished .crdownload files)
	DetectEdge    bool     `json:"detectEdge"`
	EdgeFolders   []string `json:"edgeFolders"`
	DetectChrome  bool     `json:"detectChrome"`
	ChromeFolders []string `json:"chromeFolders"`

	// 2. Download managers (FDM, IDM, Neat, Xtreme, JDownloader, Motrix...)
	DetectDownloadManagers bool     `json:"detectDownloadManagers"`
	DownloadManagers       []string `json:"downloadManagers"`

	// Settings from 1.5 (Free Download Manager only), read once and moved into
	// the download-manager settings above.
	LegacyDetectFDM    *bool    `json:"detectFreeDownloadManager,omitempty"`
	LegacyFDMProcesses []string `json:"freeDownloadManagerProcesses,omitempty"`

	// 3. Game launchers (Steam, Epic, Xbox, EA, Battle.net, Ubisoft, GOG, Riot, Amazon...)
	DetectGameLaunchers bool     `json:"detectGameLaunchers"`
	GameLaunchers       []string `json:"gameLaunchers"`

	MinSpeedKBps  float64 `json:"minSpeedKBps"`
	GraceSeconds  int     `json:"graceSeconds"`
	CheckSeconds  int     `json:"checkSeconds"`
	KeepScreenOn  bool    `json:"keepScreenOn"`
	Notifications bool    `json:"notifications"`

	CheckForUpdates    bool `json:"checkForUpdates"`
	AutoInstallUpdates bool `json:"autoInstallUpdates"`
}

const configHelp = "The PC stays awake only while one of these is downloading, nothing else counts: " +
	"detectEdge / detectChrome: Microsoft Edge / Google Chrome (their unfinished .crdownload files in your Downloads folder, the browser's own download folder, and any edgeFolders / chromeFolders you add; %DOWNLOADS% = your Downloads folder). " +
	"detectDownloadManagers: the download managers in downloadManagers (program names from Task Manager > Details). " +
	"detectGameLaunchers: the game launchers and stores in gameLaunchers (program names from Task Manager > Details). " +
	"minSpeedKBps: how much steady activity from a download manager or launcher counts as downloading. " +
	"graceSeconds: how long to stay awake after downloading stops or stalls. keepScreenOn: also keep the display on. " +
	"checkForUpdates: look for new versions on GitHub at start and every 6 hours and download them in the background. " +
	"autoInstallUpdates: install them without asking (otherwise click Update now in the tray menu). " +
	"Save this file and changes apply within a few seconds."

// Game launcher programs that download games themselves. Their built-in web
// views (steamwebhelper, EpicWebHelper, UplayWebCore...) are left out on
// purpose so store pages and trailers don't count.
func defaultGameLaunchers() []string {
	return []string{
		"steam", "steamservice", // Steam
		"EpicGamesLauncher",           // Epic Games
		"XboxPcApp", "GamingServices", // Xbox app / Microsoft Store games
		"EADesktop", "EABackgroundService", // EA app
		"Battle.net",            // Battle.net (its downloader "Agent" is watched while Battle.net runs)
		"UbisoftConnect", "upc", // Ubisoft Connect
		"GalaxyClient",       // GOG Galaxy
		"RiotClientServices", // Riot Client
		"Amazon Games",       // Amazon Games
		"RockstarService",    // Rockstar Games Launcher (its "Launcher" is watched while it runs)
		"itch",               // itch.io
	}
}

// Download managers that download files themselves.
func defaultDownloadManagers() []string {
	return []string{
		"fdm",              // Free Download Manager
		"IDMan",            // Internet Download Manager
		"NeatDM",           // Neat Download Manager
		"xdm-app", "xdman", // Xtreme Download Manager
		"JDownloader2", // JDownloader 2
		"Motrix",       // Motrix
		"EagleGet",     // EagleGet
		"FlashGet",     // FlashGet
		"aria2c",       // aria2 (also what Motrix uses underneath)
	}
}

var downloadManagerNames = map[string]string{
	"fdm": "Free Download Manager", "idman": "IDM", "neatdm": "Neat Download Manager",
	"xdm-app": "Xtreme Download Manager", "xdman": "Xtreme Download Manager",
	"jdownloader2": "JDownloader", "motrix": "Motrix", "eagleget": "EagleGet",
	"flashget": "FlashGet", "aria2c": "aria2",
}

func friendlyDownloadManager(norm string) string {
	if n, ok := downloadManagerNames[norm]; ok {
		return n
	}
	return norm
}

// Downloaders with generic program names, watched only while their launcher runs.
var launcherCompanions = map[string][]string{
	"battle.net":      {"Agent"},
	"rockstarservice": {"Launcher"},
}

var launcherNames = map[string]string{
	"steam": "Steam", "steamservice": "Steam",
	"epicgameslauncher": "Epic Games",
	"xboxpcapp":         "Xbox app", "gamingservices": "Xbox app",
	"eadesktop": "EA app", "eabackgroundservice": "EA app",
	"battle.net": "Battle.net", "agent": "Battle.net",
	"ubisoftconnect": "Ubisoft Connect", "upc": "Ubisoft Connect",
	"galaxyclient":       "GOG Galaxy",
	"riotclientservices": "Riot Client",
	"amazon games":       "Amazon Games",
	"rockstarservice":    "Rockstar Launcher", "launcher": "Rockstar Launcher",
	"itch": "itch.io",
}

func friendlyLauncher(norm string) string {
	if n, ok := launcherNames[norm]; ok {
		return n
	}
	return norm
}

func defaultConfig() Config {
	return Config{
		Help:                   configHelp,
		ConfigVersion:          currentConfigVersion,
		DetectEdge:             true,
		EdgeFolders:            []string{"%DOWNLOADS%"},
		DetectChrome:           true,
		ChromeFolders:          []string{"%DOWNLOADS%"},
		DetectDownloadManagers: true,
		DownloadManagers:       defaultDownloadManagers(),
		DetectGameLaunchers:    true,
		GameLaunchers:          defaultGameLaunchers(),
		MinSpeedKBps:           64,
		GraceSeconds:           90,
		CheckSeconds:           3,
		KeepScreenOn:           false,
		Notifications:          true,

		CheckForUpdates:    true,
		AutoInstallUpdates: false, // updates download quietly; you click "Update now"
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

// migrate upgrades a config saved by an older version. Returns true if it changed.
// Settings from before 1.5 that no longer exist (network detection, other
// browsers...) are simply dropped; the new ones start at their defaults.
func (c *Config) migrate() bool {
	if c.ConfigVersion >= currentConfigVersion {
		return false
	}
	if c.ConfigVersion < 4 {
		// From 1.4 updates wait for you to click "Update now" instead of installing by themselves.
		c.AutoInstallUpdates = false
	}
	if c.ConfigVersion < 7 {
		// 1.6 adds Chrome and widens "Free Download Manager" into a list of
		// download managers. Keep FDM switched off if it was, and keep any
		// program names that were added by hand.
		if c.LegacyDetectFDM != nil {
			c.DetectDownloadManagers = *c.LegacyDetectFDM
		}
		have := map[string]bool{}
		for _, n := range c.DownloadManagers {
			have[normaliseName(n)] = true
		}
		for _, n := range c.LegacyFDMProcesses {
			if k := normaliseName(n); k != "" && !have[k] {
				have[k] = true
				c.DownloadManagers = append(c.DownloadManagers, n)
			}
		}
	}
	c.LegacyDetectFDM, c.LegacyFDMProcesses = nil, nil
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
