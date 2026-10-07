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

package report

import (
	"testing"
	"time"
	_ "time/tzdata" // the DST case needs America/New_York regardless of the host's zoneinfo

	"github.com/e4jet/bme280mon/internal/alert"
	"github.com/e4jet/bme280mon/internal/sensor"
)

// zone is a fixed non-UTC offset, so a test that confuses UTC and local dates fails.
var zone = time.FixedZone("test", -5*60*60)

// at returns 2026-10-<day> hh:mm in zone.
func at(day, hh, mm int) time.Time {
	return time.Date(2026, time.October, day, hh, mm, 0, 0, zone)
}

func TestDue(t *testing.T) {
	t.Parallel()
	type call struct {
		now  time.Time
		want bool
	}
	tests := map[string]struct {
		hour, minute int
		start        time.Time
		calls        []call
	}{
		"before target":            {12, 0, at(6, 9, 0), []call{{at(6, 11, 59), false}}},
		"at target":                {12, 0, at(6, 9, 0), []call{{at(6, 12, 0), true}}},
		"once per day":             {12, 0, at(6, 9, 0), []call{{at(6, 13, 0), true}, {at(6, 18, 0), false}}},
		"next day":                 {12, 0, at(6, 9, 0), []call{{at(6, 12, 0), true}, {at(7, 11, 0), false}, {at(7, 12, 0), true}}},
		"start after target":       {12, 0, at(6, 13, 0), []call{{at(6, 14, 0), false}, {at(7, 12, 0), true}}},
		"start at target":          {12, 0, at(6, 12, 0), []call{{at(6, 12, 0), false}}},
		"backward jump":            {12, 0, at(6, 9, 0), []call{{at(6, 12, 0), true}, {at(5, 13, 0), false}}},
		"forward jump over target": {12, 0, at(5, 9, 0), []call{{at(6, 14, 0), true}, {at(6, 15, 0), false}}},
		"midnight report":          {0, 0, at(6, 9, 0), []call{{at(6, 23, 59), false}, {at(7, 0, 0), true}}},
		"non-local now":            {12, 0, at(6, 9, 0), []call{{at(6, 12, 0).UTC(), true}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := New(Options{Hour: tc.hour, Minute: tc.minute, Location: zone, Stale: time.Minute, Start: tc.start})
			for i, c := range tc.calls {
				if got := s.Due(c.now); got != c.want {
					t.Fatalf("call %d Due(%v) = %v, want %v", i, c.now, got, c.want)
				}
			}
		})
	}
}

// A report time inside a spring-forward gap does not exist that day. The report
// must fire once, at the first tick after the gap. time.Date resolves a gap
// time before the gap in New York but after it in London and Lord Howe, and
// Lord Howe's gap is 30 minutes, so the cases cover each behavior.
func TestDueDSTGap(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		zone         string
		month        time.Month
		day          int
		hour, minute int // report time, inside the gap
		lastBefore   [2]int
		firstAfter   [2]int
	}{
		"New York 1h gap":   {"America/New_York", time.March, 8, 2, 30, [2]int{1, 59}, [2]int{3, 0}},
		"London 1h gap":     {"Europe/London", time.March, 29, 1, 30, [2]int{0, 59}, [2]int{2, 0}},
		"Lord Howe 30m gap": {"Australia/Lord_Howe", time.October, 4, 2, 15, [2]int{1, 59}, [2]int{2, 30}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			at := func(hm [2]int) time.Time { return time.Date(2026, tc.month, tc.day, hm[0], hm[1], 0, 0, loc) }
			s := New(Options{Hour: tc.hour, Minute: tc.minute, Location: loc, Stale: time.Minute, Start: at([2]int{0, 0})})
			if s.Due(at(tc.lastBefore)) {
				t.Errorf("Due(%v) = true before the gap, want false", at(tc.lastBefore))
			}
			if !s.Due(at(tc.firstAfter)) {
				t.Errorf("Due(%v) = false after the gap, want true", at(tc.firstAfter))
			}
			if s.Due(at([2]int{23, 0})) {
				t.Error("Due at 23:00 = true, want false (already reported)")
			}
		})
	}
}

// A nil Location means time.Local and must not panic.
func TestNewNilLocation(t *testing.T) {
	t.Parallel()
	s := New(Options{Hour: 12, Start: time.Now()})
	s.Due(time.Now())
}

func TestNotification(t *testing.T) {
	t.Parallel()
	const title = "📊 Daily: 21°C, 47%"
	now := at(6, 12, 0)
	reading := func(age time.Duration) sensor.Reading {
		return sensor.Reading{Humidity: 47.2, Temperature: 21.3, Time: now.Add(-age)}
	}
	tests := map[string]struct {
		r           sensor.Reading
		ok          bool
		title, body string
	}{
		"fresh": {reading(30 * time.Second), true,
			title, "Humidity 47.2%, temperature 21.3°C"},
		"age at stale boundary": {reading(time.Minute), true,
			title, "Humidity 47.2%, temperature 21.3°C"},
		"stale": {reading(2*time.Hour + 5*time.Minute + 3*time.Second), true,
			title, "Humidity 47.2%, temperature 21.3°C (last reading 2h5m3s ago)"},
		"no reading": {sensor.Reading{}, false,
			"📊 Daily: no reading available", "no successful sensor read since start"},
	}
	s := New(Options{Hour: 12, Location: zone, Stale: time.Minute, Start: at(6, 9, 0)})
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n := s.Notification(tc.r, tc.ok, now)
			if n.Title != tc.title {
				t.Errorf("title = %q, want %q", n.Title, tc.title)
			}
			if n.Body != tc.body {
				t.Errorf("body = %q, want %q", n.Body, tc.body)
			}
			if n.Priority != alert.Low {
				t.Errorf("priority = %v, want Low", n.Priority)
			}
		})
	}
}
