//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Microsoft Edge saves an unfinished download as "<name>.crdownload" in the
// folder it's downloading to, and renames it when it's done. Edge's chosen
// download folder (Settings > Downloads > Location) is stored per profile in
// "%LOCALAPPDATA%\Microsoft\Edge\User Data\<profile>\Preferences".

const edgeProcess = "msedge"

var edgePartialExtensions = []string{".crdownload"}

type edgeFolderCache struct {
	checked time.Time
	folders []string
}

var edgeFolders edgeFolderCache

// edgeDownloadFolders returns the download folders set in each Edge profile.
// The Preferences files can be large, so this is re-read at most once a minute.
func edgeDownloadFolders() []string {
	if time.Since(edgeFolders.checked) < time.Minute && !edgeFolders.checked.IsZero() {
		return edgeFolders.folders
	}
	edgeFolders.checked = time.Now()
	edgeFolders.folders = readEdgeDownloadFolders(filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "User Data"))
	return edgeFolders.folders
}

func readEdgeDownloadFolders(userData string) []string {
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
