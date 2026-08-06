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

func TestDetectorTransitions(t *testing.T) {
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := New(60, 3)
			for i, h := range tc.seq {
				if got := d.Update(h); got != tc.want[i] {
					t.Fatalf("step %d humidity %.1f: got %v want %v", i, h, got, tc.want[i])
				}
			}
		})
	}
}
