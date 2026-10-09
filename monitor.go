//go:build windows

package main

import (
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ioMonitor measures how much data the watched download-manager processes are
// moving (network reads + disk writes, as counted by Windows per process).
// Measuring only those processes means that streaming video, games or Windows
// Update never count as "a download is happening" - only your download manager.
type ioMonitor struct {
	prev     map[uint32]uint64
	prevTime time.Time
}

func newIOMonitor() *ioMonitor {
	return &ioMonitor{prev: map[uint32]uint64{}}
}

func normaliseName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimSuffix(s, ".exe")
}

// poll returns the names of watched apps that are running and their combined
// I/O rate in bytes per second since the previous poll.
func (m *ioMonitor) poll(watch []string) (running []string, bytesPerSec float64) {
	want := make(map[string]bool, len(watch))
	for _, w := range watch {
		if n := normaliseName(w); n != "" {
			want[n] = true
		}
	}

	now := time.Now()
	current := map[uint32]uint64{}
	names := map[string]bool{}
	var delta uint64

	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, 0
	}
	defer syscall.CloseHandle(snap)

	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = syscall.Process32First(snap, &pe); err == nil; err = syscall.Process32Next(snap, &pe) {
		exe := syscall.UTF16ToString(pe.ExeFile[:])
		if !want[normaliseName(exe)] {
			continue
		}
		names[strings.TrimSuffix(exe, ".exe")] = true

		total, ok := processIOBytes(pe.ProcessID)
		if !ok {
			continue
		}
		current[pe.ProcessID] = total
		if before, seen := m.prev[pe.ProcessID]; seen && total >= before {
			delta += total - before
		}
	}

	elapsed := now.Sub(m.prevTime).Seconds()
	if !m.prevTime.IsZero() && elapsed > 0 {
		bytesPerSec = float64(delta) / elapsed
	}
	m.prev = current
	m.prevTime = now

	for n := range names {
		running = append(running, n)
	}
	sort.Strings(running)
	return running, bytesPerSec
}

func processIOBytes(pid uint32) (uint64, bool) {
	h, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer syscall.CloseHandle(h)
	var c IO_COUNTERS
	r, _, _ := procGetProcessIoCounters.Call(uintptr(h), uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		return 0, false
	}
	return c.ReadTransferCount + c.WriteTransferCount + c.OtherTransferCount, true
}
