//go:build windows

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	ole32                    = syscallLazy("ole32.dll")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
)

func syscallLazy(name string) *syscall.LazyDLL { return syscall.NewLazyDLL(name) }

var folderIDDownloads = GUID{0x374DE290, 0x123F, 0x4565, [8]byte{0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B}}

// downloadsFolder returns the user's real Downloads folder, even if it was moved.
func downloadsFolder() string {
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&folderIDDownloads)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 && p != nil {
		defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
		return windowsString(p)
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Downloads")
}

func windowsString(p *uint16) string {
	var u []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u)
}

var envVar = regexp.MustCompile(`%([^%]+)%`)

// expandPath understands %DOWNLOADS% plus normal Windows variables like %USERPROFILE%.
func expandPath(p string) string {
	return envVar.ReplaceAllStringFunc(p, func(m string) string {
		name := strings.Trim(m, "%")
		if strings.EqualFold(name, "DOWNLOADS") {
			return downloadsFolder()
		}
		if v := os.Getenv(name); v != "" {
			return v
		}
		return m
	})
}

type fileState struct {
	size int64
	mod  time.Time
}

// partialMonitor looks for in-progress download files (Chrome/Edge .crdownload,
// Firefox .part, torrent and download-manager temp files) that are still
// growing. This catches browser downloads even when they're too slow to stand
// out from normal network traffic.
type partialMonitor struct {
	prev   map[string]fileState
	primed bool
}

func newPartialMonitor() *partialMonitor {
	return &partialMonitor{prev: map[string]fileState{}}
}

const (
	maxScanDepth   = 2
	maxScanEntries = 20000
)

// reset forgets everything seen so far.
func (m *partialMonitor) reset() {
	m.prev = map[string]fileState{}
	m.primed = false
}

// poll returns the names of partial download files that changed since the last scan.
func (m *partialMonitor) poll(folders, extensions []string) []string {
	exts := make([]string, 0, len(extensions))
	for _, e := range extensions {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		exts = append(exts, e)
	}
	isPartial := func(name string) bool {
		n := strings.ToLower(name)
		for _, e := range exts {
			if strings.HasSuffix(n, e) {
				return true
			}
		}
		return false
	}

	current := map[string]fileState{}
	var active []string
	entries := 0
	now := time.Now()
	seenDir := map[string]bool{}

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if entries >= maxScanEntries {
			return
		}
		list, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range list {
			entries++
			if entries >= maxScanEntries {
				return
			}
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if depth < maxScanDepth && e.Type()&os.ModeSymlink == 0 {
					walk(full, depth+1)
				}
				continue
			}
			if !isPartial(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			st := fileState{info.Size(), info.ModTime()}
			current[full] = st
			before, seen := m.prev[full]
			changed := (seen && (st.size != before.size || !st.mod.Equal(before.mod))) ||
				(!seen && m.primed) || // a brand-new partial file appeared
				(!seen && now.Sub(st.mod) < 15*time.Second) // first scan: written moments ago
			if changed {
				active = append(active, e.Name())
			}
		}
	}

	for _, f := range folders {
		dir := filepath.Clean(expandPath(f))
		key := strings.ToLower(dir)
		if dir == "" || seenDir[key] {
			continue
		}
		seenDir[key] = true
		walk(dir, 0)
	}
	m.prev = current
	m.primed = true
	return active
}
