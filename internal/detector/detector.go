/*
(c) Copyright 2026 Eric Paul Forgette

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package detector implements a hysteresis state machine that decides when a
// reading has left its acceptable band and when it has recovered.
package detector

import "math"

// State is the current condition tracked by the Detector.
type State int

const (
	// Normal means the reading is inside its acceptable band.
	Normal State = iota
	// High means the reading is at or above the high threshold and has not recovered.
	High
	// Low means the reading is at or below the low threshold and has not recovered.
	Low
)

// Event is emitted by Update when a transition happens.
type Event int

const (
	// None means no transition on this reading.
	None Event = iota
	// HighAlert means the reading just crossed up to the high condition.
	HighAlert
	// LowAlert means the reading just crossed down to the low condition.
	LowAlert
	// Recovered means the reading just returned to the band, from either side.
	Recovered
)

// Detector tracks a reading against a band with a hysteresis buffer so it does
// not flap when the reading hovers at a threshold.
type Detector struct {
	low    float64
	high   float64
	buffer float64
	state  State
}

// NewHigh returns a Detector with no low bound, for a quantity where only the
// upper end is a problem (humidity). It fires HighAlert at or above high and
// Recovered at or below (high - buffer), and never fires LowAlert.
func NewHigh(high, buffer float64) *Detector {
	// -Inf is the disabled low bound: no reading can be at or below it, so the
	// low branch of Update is unreachable without a second state flag.
	return NewBand(math.Inf(-1), high, buffer)
}

// NewBand returns a Detector bounded on both ends, for a quantity where either
// extreme is a problem (temperature). It fires HighAlert at or above high,
// LowAlert at or below low, and Recovered once the reading is back inside the
// band by at least buffer. The caller is responsible for low < high and for a
// buffer narrow enough that the two recovery points stay ordered; config
// validation enforces both.
func NewBand(low, high, buffer float64) *Detector {
	return &Detector{low: low, high: high, buffer: buffer, state: Normal}
}

// Update feeds one reading and returns the resulting Event.
func (d *Detector) Update(v float64) Event {
	switch d.state {
	case Normal:
		if v >= d.high {
			d.state = High
			return HighAlert
		}
		if v <= d.low {
			d.state = Low
			return LowAlert
		}
	case High:
		if v <= d.high-d.buffer {
			d.state = Normal
			return Recovered
		}
	case Low:
		if v >= d.low+d.buffer {
			d.state = Normal
			return Recovered
		}
	}
	return None
}
