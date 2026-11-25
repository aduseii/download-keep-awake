//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Microsoft Edge and Google Chrome save an unfinished download as
// "<name>.crdownload" in the folder they're downloading to, and rename it when
// it's done. Each browser's chosen download folder (Settings > Downloads >
// Location) is stored per profile in "<User Data>\<profile>\Preferences".

var browserPartialExtensions = []string{".crdownload"}

type browser struct {
	name     string // shown in the tray, e.g. "Edge"
	process  string // normalised program name
	userData []string

	checked time.Time
	folders []string
}

var (
	edgeBrowser = &browser{
		name: "Edge", process: "msedge",
		userData: []string{`Microsoft\Edge\User Data`},
	}
	chromeBrowser = &browser{
		name: "Chrome", process: "chrome",
		userData: []string{`Google\Chrome\User Data`},
	}
)

// downloadFolders returns the download folders set in each of the browser's
// profiles. The Preferences files can be large, so they're re-read at most
// once a minute.
func (b *browser) downloadFolders() []string {
	if !b.checked.IsZero() && time.Since(b.checked) < time.Minute {
		return b.folders
	}
	b.checked = time.Now()
	b.folders = nil
	for _, ud := range b.userData {
		b.folders = append(b.folders, readBrowserDownloadFolders(filepath.Join(os.Getenv("LOCALAPPDATA"), ud))...)
	}
	return b.folders
}

func readBrowserDownloadFolders(userData string) []string {
	entries, err := os.ReadDir(userData)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !(e.Name() == "Default" || strings.HasPrefix(e.Name(), "Profile ")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(userData, e.Name(), "Preferences"))
		if err != nil {
			continue
		}
		var prefs struct {
			Download struct {
				DefaultDirectory string `json:"default_directory"`
			} `json:"download"`
			SaveFile struct {
				DefaultDirectory string `json:"default_directory"`
			} `json:"savefile"`
		}
		if json.Unmarshal(data, &prefs) != nil {
			continue
		}
		for _, dir := range []string{prefs.Download.DefaultDirectory, prefs.SaveFile.DefaultDirectory} {
			if dir == "" {
				continue
			}
			key := strings.ToLower(filepath.Clean(dir))
			if !seen[key] {
				seen[key] = true
				out = append(out, dir)
			}
		}
	}
	return out
}
