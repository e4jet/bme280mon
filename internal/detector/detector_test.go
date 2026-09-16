//go:build unit

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

package detector

import "testing"

// run feeds seq to d and asserts the event at each step.
func run(t *testing.T, d *Detector, seq []float64, want []Event) {
	t.Helper()
	for i, v := range seq {
		if got := d.Update(v); got != want[i] {
			t.Fatalf("step %d value %.1f: got %v want %v", i, v, got, want[i])
		}
	}
}

func TestHighOnlyTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seq  []float64
		want []Event
	}{
		{"below threshold stays quiet", []float64{40, 55, 59.9}, []Event{None, None, None}},
		{"crossing up fires once", []float64{50, 60, 65, 61}, []Event{None, HighAlert, None, None}},
		{"recovers below buffer", []float64{65, 58, 57}, []Event{HighAlert, None, Recovered}},
		{"flapping in buffer band does not re-alert", []float64{62, 58, 59, 62}, []Event{HighAlert, None, None, None}},
		{"full cycle", []float64{50, 61, 56, 62}, []Event{None, HighAlert, Recovered, HighAlert}},
		{"no low bound: a very low reading is silent", []float64{0, -100}, []Event{None, None}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run(t, NewHigh(60, 3), tc.seq, tc.want)
		})
	}
}

func TestBandTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seq  []float64
		want []Event
	}{
		{"inside the band stays quiet", []float64{20, 15.1, 29.9}, []Event{None, None, None}},
		{"crossing down fires once", []float64{20, 15, 12, 14}, []Event{None, LowAlert, None, None}},
		{"recovers above low buffer", []float64{12, 15.5, 16}, []Event{LowAlert, None, Recovered}},
		{"flapping in the low buffer band does not re-alert", []float64{14, 15.5, 15.9, 14}, []Event{LowAlert, None, None, None}},
		{"crossing up fires once", []float64{20, 30, 33, 29.5}, []Event{None, HighAlert, None, None}},
		{"recovers below high buffer", []float64{33, 29.5, 29}, []Event{HighAlert, None, Recovered}},
		{"full low to high cycle", []float64{20, 14, 17, 31, 28, 20}, []Event{None, LowAlert, Recovered, HighAlert, Recovered, None}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run(t, NewBand(15, 30, 1), tc.seq, tc.want)
		})
	}
}

// A plunge from High past the low threshold must recover first and only then
// alert low, so the two conditions can never be reported by one reading.
func TestBandHighToLowPassesThroughNormal(t *testing.T) {
	t.Parallel()
	run(t, NewBand(15, 30, 1), []float64{31, 10, 10}, []Event{HighAlert, Recovered, LowAlert})
}
