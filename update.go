//go:build windows

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Self-updater. New versions are published as GitHub Releases by the
// workflow in .github/workflows/release.yml. The app checks shortly after
// start and then every few hours, downloads the .exe for this PC's
// architecture, verifies it against SHA256SUMS.txt from the same release,
// swaps it in place of the running .exe and restarts itself. Updates are held
// back while a download is being kept awake, so the restart never interrupts
// anything.

const (
	updateFirstCheck = 30 * time.Second
	updateInterval   = 6 * time.Hour
	maxExeSize       = 64 << 20

	WM_UPDATE_RESULT = WM_APP + 2
	updatedFlag      = "--updated"
)

type releaseInfo struct {
	Tag     string
	Version [3]int
	PageURL string
	ExeURL  string
	SumsURL string
}

type updateResult struct {
	manual    bool
	err       error
	release   *releaseInfo // newer release found (nil = up to date)
	staged    string       // path of downloaded + verified new .exe, ready to install
	notWanted bool         // newer release exists but auto-install is off
}

var (
	updateBusy    atomic.Bool
	updateMu      sync.Mutex
	pendingResult *updateResult
	stagedUpdate  *updateResult // verified update waiting to be installed
	httpClient    = &http.Client{Timeout: 5 * time.Minute}
	githubAPI     = "https://api.github.com"
)

// parseVersion turns "v1.2.3" / "1.2.3" into numbers. Dev builds ("0.0.0-dev+abc") fail.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func newer(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func assetName() string {
	if runtime.GOARCH == "arm64" {
		return "DownloadKeepAwake-arm64.exe"
	}
	return "DownloadKeepAwake.exe"
}

// repoAPI turns https://github.com/owner/repo into the releases API URL.
func repoAPI() (string, error) {
	u := strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	u = strings.TrimSuffix(u, ".git")
	const prefix = "https://github.com/"
	if !strings.HasPrefix(u, prefix) {
		return "", fmt.Errorf("update source %q is not a GitHub repository", repoURL)
	}
	parts := strings.Split(strings.TrimPrefix(u, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("update source %q is not a GitHub repository", repoURL)
	}
	return githubAPI + "/repos/" + parts[0] + "/" + parts[1] + "/releases/latest", nil
}

func httpGet(url string, accept string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "DownloadKeepAwake/"+version)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, errors.New("no published release found (the GitHub repository must be public and have at least one release)")
		case http.StatusForbidden, http.StatusTooManyRequests:
			return nil, errors.New("GitHub is rate-limiting update checks; will try again later")
		}
		return nil, fmt.Errorf("server replied %s", resp.Status)
	}
	return resp, nil
}

func fetchLatestRelease() (*releaseInfo, error) {
	api, err := repoAPI()
	if err != nil {
		return nil, err
	}
	resp, err := httpGet(api, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("couldn't read release info: %w", err)
	}
	v, ok := parseVersion(body.TagName)
	if !ok || body.Draft || body.Prerelease {
		return nil, fmt.Errorf("latest release %q isn't a normal version", body.TagName)
	}
	r := &releaseInfo{Tag: body.TagName, Version: v, PageURL: body.HTMLURL}
	for _, a := range body.Assets {
		switch {
		case strings.EqualFold(a.Name, assetName()):
			r.ExeURL = a.URL
		case strings.EqualFold(a.Name, "SHA256SUMS.txt"):
			r.SumsURL = a.URL
		}
	}
	return r, nil
}

func expectedHash(sumsURL, name string) (string, error) {
	resp, err := httpGet(sumsURL, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.EqualFold(strings.TrimPrefix(f[1], "*"), name) {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", errors.New("the release's checksum file doesn't list " + name)
}

func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return exe, nil
}

// download fetches the new .exe next to the running one and verifies it.
func download(r *releaseInfo) (string, error) {
	if r.ExeURL == "" || r.SumsURL == "" {
		return "", fmt.Errorf("release %s is missing %s or SHA256SUMS.txt", r.Tag, assetName())
	}
	want, err := expectedHash(r.SumsURL, assetName())
	if err != nil {
		return "", err
	}
	exe, err := exePath()
	if err != nil {
		return "", err
	}
	staged := exe + ".new"
	f, err := os.Create(staged)
	if err != nil {
		return "", fmt.Errorf("can't write to %s - move the app to a folder you own (e.g. Documents or %%LOCALAPPDATA%%) so it can update itself", filepath.Dir(exe))
	}

	resp, err := httpGet(r.ExeURL, "application/octet-stream")
	if err != nil {
		f.Close()
		os.Remove(staged)
		return "", err
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxExeSize+1))
	cerr := f.Close()
	fail := func(e error) (string, error) { os.Remove(staged); return "", e }
	switch {
	case err != nil:
		return fail(fmt.Errorf("download interrupted: %w", err))
	case cerr != nil:
		return fail(cerr)
	case n > maxExeSize || n < 64*1024:
		return fail(errors.New("downloaded file has an unexpected size"))
	case hex.EncodeToString(h.Sum(nil)) != want:
		return fail(errors.New("downloaded file failed its checksum check, so it was discarded"))
	}
	head := make([]byte, 2)
	if g, err := os.Open(staged); err == nil {
		io.ReadFull(g, head)
		g.Close()
	}
	if string(head) != "MZ" {
		return fail(errors.New("downloaded file isn't a Windows program"))
	}
	return staged, nil
}

// checkForUpdate runs in the background and posts its result to the window.
func checkForUpdate(manual, autoInstall bool) {
	if !updateBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer updateBusy.Store(false)
		res := &updateResult{manual: manual}
		defer func() {
			updateMu.Lock()
			pendingResult = res
			updateMu.Unlock()
			procPostMessageW.Call(a.hwnd, WM_UPDATE_RESULT, 0, 0)
		}()

		cur, ok := parseVersion(version)
		if !ok {
			if !manual {
				res.err = errors.New("development build")
				return
			}
			cur = [3]int{0, 0, 0}
		}
		r, err := fetchLatestRelease()
		if err != nil {
			res.err = err
			return
		}
		if !newer(r.Version, cur) {
			return
		}
		res.release = r
		if !autoInstall && !manual {
			res.notWanted = true
			return
		}
		res.staged, res.err = download(r)
	}()
}

// onUpdateTimer runs on the UI thread: first 30s after start, then every 6 hours.
func onUpdateTimer() {
	procSetTimer.Call(a.hwnd, updateTimerID, uintptr(updateInterval/time.Millisecond), 0)
	if a.cfg.CheckForUpdates && stagedUpdate == nil {
		checkForUpdate(false, a.cfg.AutoInstallUpdates)
	}
}

// handleUpdateResult runs on the UI thread.
func handleUpdateResult() {
	updateMu.Lock()
	res := pendingResult
	pendingResult = nil
	updateMu.Unlock()
	if res == nil {
		return
	}
	switch {
	case res.err != nil && res.manual:
		messageBox("Couldn't check for updates:\n"+res.err.Error(), appName, MB_OK|MB_ICONWARNING)
	case res.err != nil:
		// background check failed (offline, rate-limited...): try again next time, quietly
	case res.release == nil:
		if res.manual {
			messageBox(fmt.Sprintf("You're up to date.\n\n%s %s is the latest version.", appName, version), appName, MB_OK|MB_ICONINFORMATION)
		}
	case res.notWanted:
		a.availableUpdate = res.release
		notify("Update available", fmt.Sprintf("%s %s is out. Right-click the tray icon to install it.", appName, res.release.Tag))
	default:
		a.availableUpdate = res.release
		stagedUpdate = res
		if res.manual || canInstallNow() {
			installStagedUpdate()
		} else {
			notify(fmt.Sprintf("%s %s is ready", appName, res.release.Tag),
				"Click here to restart now. Otherwise it installs by itself when your downloads finish.")
		}
	}
}

// canInstallNow: don't restart while keeping the PC awake for a download.
func canInstallNow() bool {
	return a.st == stIdle || a.st == stPaused
}

func installStagedUpdate() {
	res := stagedUpdate
	if res == nil || res.staged == "" {
		return
	}
	stagedUpdate = nil
	if err := swapAndRestart(res.staged); err != nil {
		os.Remove(res.staged)
		messageBox("The update to "+res.release.Tag+" couldn't be installed:\n"+err.Error(), appName, MB_OK|MB_ICONWARNING)
	}
}

func swapAndRestart(staged string) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	old := exe + ".old"
	os.Remove(old)
	// Windows lets a running .exe be renamed (but not overwritten or deleted).
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		os.Rename(old, exe)
		return err
	}

	// Hand over: free the single-instance lock and the tray icon, start the new
	// version, then quit. The new version deletes the .old file.
	releaseSingleInstance()
	removeTrayIcon()
	args := []string{updatedFlag + "=" + version}
	if hasFlag(portableFlag) {
		args = append(args, portableFlag)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		// roll back so the user still has a working app
		os.Rename(exe, staged)
		os.Rename(old, exe)
		acquireSingleInstance(false)
		addTrayIcon()
		return err
	}
	setAwake(false, false)
	procDestroyWindow.Call(a.hwnd)
	return nil
}

// installOnExit swaps in a downloaded update when the app closes without
// restarting it; the new version runs the next time the app starts.
func installOnExit() {
	res := stagedUpdate
	if res == nil || res.staged == "" {
		return
	}
	stagedUpdate = nil
	exe, err := exePath()
	if err != nil {
		return
	}
	old := exe + ".old"
	os.Remove(old)
	if os.Rename(exe, old) != nil {
		return
	}
	if os.Rename(res.staged, exe) != nil {
		os.Rename(old, exe)
	}
}

// cleanupAfterUpdate removes leftovers from a previous update.
func cleanupAfterUpdate() {
	exe, err := exePath()
	if err != nil {
		return
	}
	os.Remove(exe + ".new")
	go func() {
		for i := 0; i < 20; i++ { // the old process may take a moment to exit
			if err := os.Remove(exe + ".old"); err == nil || errors.Is(err, os.ErrNotExist) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// updatedFrom returns the previous version if we were just started by the updater.
func updatedFrom() (string, bool) {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, updatedFlag) {
			return strings.TrimPrefix(strings.TrimPrefix(arg, updatedFlag), "="), true
		}
	}
	return "", false
}
