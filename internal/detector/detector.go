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

// Package detector implements a hysteresis state machine that decides when
// humidity has crossed into a "high" condition and when it has recovered.
package detector

// State is the current condition tracked by the Detector.
type State int

const (
	// Normal means humidity is below the high threshold.
	Normal State = iota
	// High means humidity is at or above the threshold and has not recovered.
	High
)

// Event is emitted by Update when a transition happens.
type Event int

const (
	// None means no transition on this reading.
	None Event = iota
	// HighAlert means humidity just crossed up to the high condition.
	HighAlert
	// Recovered means humidity just dropped back below the recovery point.
	Recovered
)

// Detector tracks humidity against a threshold with a hysteresis buffer so it
// does not flap when humidity hovers near the threshold.
type Detector struct {
	threshold float64
	buffer    float64
	state     State
}

// New returns a Detector in the Normal state. It fires HighAlert at or above
// threshold and Recovered at or below (threshold - buffer).
func New(threshold, buffer float64) *Detector {
	return &Detector{threshold: threshold, buffer: buffer, state: Normal}
}

// Update feeds one humidity reading and returns the resulting Event.
func (d *Detector) Update(humidity float64) Event {
	switch d.state {
	case Normal:
		if humidity >= d.threshold {
			d.state = High
			return HighAlert
		}
	case High:
		if humidity <= d.threshold-d.buffer {
			d.state = Normal
			return Recovered
		}
	}
	return None
}
