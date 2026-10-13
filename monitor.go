//go:build windows

package main

import (
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ioMonitor measures how much data the watched processes are moving
// (network + disk, as counted by Windows per process). Watching only specific
// apps means browsing, streaming or anything else never counts as a download.
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

// appActivity is one poll's result: which watched apps are running (by
// normalised name) and how many bytes per second each is moving.
type appActivity struct {
	running map[string]bool
	rate    map[string]float64
}

func (x appActivity) total() float64 {
	t := 0.0
	for _, v := range x.rate {
		t += v
	}
	return t
}

// pollEach measures every watched app separately since the previous poll.
func (m *ioMonitor) pollEach(watch []string) appActivity {
	want := make(map[string]bool, len(watch))
	for _, w := range watch {
		if n := normaliseName(w); n != "" {
			want[n] = true
		}
	}
	res := appActivity{running: map[string]bool{}, rate: map[string]float64{}}

	now := time.Now()
	current := map[uint32]uint64{}
	delta := map[string]uint64{}

	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return res
	}
	defer syscall.CloseHandle(snap)

	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = syscall.Process32First(snap, &pe); err == nil; err = syscall.Process32Next(snap, &pe) {
		name := normaliseName(syscall.UTF16ToString(pe.ExeFile[:]))
		if !want[name] {
			continue
		}
		res.running[name] = true

		total, ok := processIOBytes(pe.ProcessID)
		if !ok {
			continue
		}
		current[pe.ProcessID] = total
		if before, seen := m.prev[pe.ProcessID]; seen && total >= before {
			delta[name] += total - before
		}
	}

	elapsed := now.Sub(m.prevTime).Seconds()
	if !m.prevTime.IsZero() && elapsed > 0 {
		for n, d := range delta {
			res.rate[n] = float64(d) / elapsed
		}
	}
	m.prev = current
	m.prevTime = now
	return res
}

// poll returns the watched apps that are running (as named in watch) and
// their combined I/O rate in bytes per second.
func (m *ioMonitor) poll(watch []string) (running []string, bytesPerSec float64) {
	act := m.pollEach(watch)
	for _, w := range watch {
		if act.running[normaliseName(w)] {
			running = append(running, strings.TrimSuffix(strings.TrimSpace(w), ".exe"))
		}
	}
	sort.Strings(running)
	return running, act.total()
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
