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

// Package report decides when the once-a-day reading notification is due and
// builds it. It holds no goroutines and does no I/O: the caller checks Due on
// each poll tick against the wall clock, which stays correct when NTP steps the
// clock at boot on a Pi without an RTC.
package report

import (
	"fmt"
	"time"

	"github.com/e4jet/bme280mon/internal/alert"
	"github.com/e4jet/bme280mon/internal/sensor"
)

// Options configures a Schedule. Declared before New per the input-struct
// convention.
type Options struct {
	Hour, Minute int
	Location     *time.Location // nil means time.Local
	Stale        time.Duration  // a reading older than this is reported with its age
	Start        time.Time      // process start, used to suppress a report after a restart
}

// Schedule tracks the daily report time and the last calendar day reported.
// It is not safe for concurrent use. The Monitor goroutine owns it.
type Schedule struct {
	hour, minute int
	loc          *time.Location
	stale        time.Duration
	lastDay      int // dateKey of the last reported day, 0 if none
}

// New returns a Schedule. If Start is at or after that day's report time, the
// day counts as reported, so a restart in the afternoon sends no second report.
func New(o Options) *Schedule {
	loc := o.Location
	if loc == nil {
		loc = time.Local //nolint:gosmopolitan // the documented default
	}
	s := &Schedule{hour: o.Hour, minute: o.Minute, loc: loc, stale: o.Stale}
	start := o.Start.In(s.loc)
	if s.reached(start) {
		s.lastDay = dateKey(start)
	}
	return s
}

// Due reports whether the report should be sent at now, and if so records
// now's calendar day as reported. Only a day later than the last reported one
// can fire, so a backward clock step cannot cause a repeat.
func (s *Schedule) Due(now time.Time) bool {
	now = now.In(s.loc)
	today := dateKey(now)
	if !s.reached(now) || today <= s.lastDay {
		return false
	}
	s.lastDay = today
	return true
}

// Notification builds the report from the last good reading r. ok is false
// when there has been no successful read since start.
func (s *Schedule) Notification(r sensor.Reading, ok bool, now time.Time) alert.Notification {
	if !ok {
		return alert.Notification{
			Title:    "🤮 Daily: no reading available",
			Body:     "no successful sensor read since start",
			Priority: alert.Low,
		}
	}
	body := r.Summary()
	if age := now.Sub(r.Time); age > s.stale {
		body += fmt.Sprintf(" (last reading %s ago)", age.Round(time.Second))
	}
	return alert.Notification{
		Title:    fmt.Sprintf("📊 Daily: %.0f°C, %.0f%%", r.Temperature, r.Humidity),
		Body:     body,
		Priority: alert.Low,
	}
}

// reached reports whether now's wall clock is at or past the report time.
// Comparing wall clocks, rather than building the report instant with
// time.Date, is correct for a report time inside a DST gap of any length: the
// clock jumps past it, and the first tick after the gap fires. A repeated hour
// at fall-back cannot fire twice, because Due fires once per day.
func (s *Schedule) reached(now time.Time) bool {
	h, m, _ := now.Clock()
	return h > s.hour || (h == s.hour && m >= s.minute)
}

// dateKey encodes t's calendar day as YYYYMMDD, so days compare as integers.
func dateKey(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}
