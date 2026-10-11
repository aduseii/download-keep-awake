//go:build windows

package main

import (
	"encoding/binary"
	"time"
	"unsafe"
)

var (
	iphlpapi         = syscallLazy("iphlpapi.dll")
	procGetIfTable2  = iphlpapi.NewProc("GetIfTable2")
	procFreeMibTable = iphlpapi.NewProc("FreeMibTable")
)

// Layout of MIB_IF_ROW2 (netioapi.h) on 64-bit Windows. Parsed by offset so
// the 1352-byte struct doesn't need to be mirrored field by field.
const (
	mibIfRow2Size      = 1352
	mibIfTable2RowsOff = 8 // ULONG NumEntries, then rows aligned to 8 bytes
	offLuid            = 0
	offType            = 1128
	offFlags           = 1152 // InterfaceAndOperStatusFlags bitfield
	offOperStatus      = 1156
	offInOctets        = 1208

	ifTypeLoopback = 24
	ifTypeTunnel   = 131
	ifOperStatusUp = 1
	flagHardware   = 0x01
	flagFilter     = 0x02
)

type netSample struct {
	t     time.Time
	total uint64 // cumulative bytes received across counted adapters
}

// netMonitor measures how fast the PC is receiving data over its real network
// adapters (Wi-Fi, Ethernet, mobile broadband). Virtual adapters, VPN tunnels,
// loopback and filter drivers are skipped so traffic isn't counted twice.
// Only incoming data counts: uploading/seeding does not keep the PC awake.
type netMonitor struct {
	prev    map[uint64]uint64
	total   uint64
	samples []netSample
}

func newNetMonitor() *netMonitor {
	return &netMonitor{prev: map[uint64]uint64{}}
}

type ifCounter struct {
	luid     uint64
	in       uint64
	hardware bool
}

func readInterfaces() ([]ifCounter, bool) {
	var table unsafe.Pointer
	if r, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&table))); r != 0 || table == nil {
		return nil, false
	}
	defer procFreeMibTable.Call(uintptr(table))

	n := *(*uint32)(table)
	out := make([]ifCounter, 0, n)
	for i := uint32(0); i < n; i++ {
		row := unsafe.Slice((*byte)(unsafe.Add(table, mibIfTable2RowsOff+uintptr(i)*mibIfRow2Size)), mibIfRow2Size)
		typ := binary.LittleEndian.Uint32(row[offType:])
		flags := row[offFlags]
		status := binary.LittleEndian.Uint32(row[offOperStatus:])
		if status != ifOperStatusUp || typ == ifTypeLoopback || typ == ifTypeTunnel || flags&flagFilter != 0 {
			continue
		}
		out = append(out, ifCounter{
			luid:     binary.LittleEndian.Uint64(row[offLuid:]),
			in:       binary.LittleEndian.Uint64(row[offInOctets:]),
			hardware: flags&flagHardware != 0,
		})
	}
	return out, true
}

// poll records a sample and returns the average download speed (bytes/s) over
// the last window, plus the speed since the previous poll for display.
func (m *netMonitor) poll(window time.Duration) (avg, now float64) {
	ifs, ok := readInterfaces()
	if !ok {
		return 0, 0
	}
	// Prefer physical adapters; fall back to every adapter if none are marked
	// as hardware (happens in some virtual machines).
	hasHW := false
	for _, f := range ifs {
		if f.hardware {
			hasHW = true
			break
		}
	}
	current := map[uint64]uint64{}
	var delta uint64
	for _, f := range ifs {
		if hasHW && !f.hardware {
			continue
		}
		current[f.luid] = f.in
		if before, seen := m.prev[f.luid]; seen && f.in >= before {
			delta += f.in - before
		}
	}
	m.prev = current
	m.total += delta

	t := time.Now()
	m.samples = append(m.samples, netSample{t, m.total})
	// keep just enough history to cover the window
	cut := 0
	for cut < len(m.samples)-1 && t.Sub(m.samples[cut+1].t) >= window {
		cut++
	}
	m.samples = m.samples[cut:]

	if len(m.samples) >= 2 {
		last, prev, first := m.samples[len(m.samples)-1], m.samples[len(m.samples)-2], m.samples[0]
		if dt := last.t.Sub(prev.t).Seconds(); dt > 0 {
			now = float64(last.total-prev.total) / dt
		}
		if dt := last.t.Sub(first.t).Seconds(); dt > 0 {
			avg = float64(last.total-first.total) / dt
		}
	}
	return avg, now
}
