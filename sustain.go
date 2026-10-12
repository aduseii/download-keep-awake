//go:build windows

package main

import "time"

// sustainTracker answers "has this been true for most of the last N seconds?".
// Real downloads run at a steady speed for minutes; scrolling a feed or loading
// a video arrives in short bursts with gaps, so it rarely holds up for 80% of a
// 30-second window.
type sustainTracker struct {
	samples []sustainSample
}

type sustainSample struct {
	t  time.Time
	on bool
}

func (s *sustainTracker) add(t time.Time, on bool, window time.Duration) {
	s.samples = append(s.samples, sustainSample{t, on})
	cut := 0
	for cut < len(s.samples) && t.Sub(s.samples[cut].t) > window {
		cut++
	}
	s.samples = s.samples[cut:]
}

// fraction returns the share of samples in the window that were true, and
// whether the window has filled up enough (most of its length) to be trusted.
func (s *sustainTracker) fraction(t time.Time, window time.Duration) (float64, bool) {
	if len(s.samples) == 0 {
		return 0, false
	}
	on := 0
	for _, x := range s.samples {
		if x.on {
			on++
		}
	}
	span := t.Sub(s.samples[0].t)
	return float64(on) / float64(len(s.samples)), span >= window*3/4
}

func (s *sustainTracker) reset() { s.samples = nil }
