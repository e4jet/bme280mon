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

// Package monitor runs the sensor poll loop and drives alerts and metrics.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/e4jet/bme280mon/internal/alert"
	"github.com/e4jet/bme280mon/internal/detector"
	"github.com/e4jet/bme280mon/internal/metrics"
	"github.com/e4jet/bme280mon/internal/sensor"
)

// readTimeout bounds a single sensor read so a wedged I2C bus cannot stall the
// poll loop or graceful shutdown indefinitely.
const readTimeout = 5 * time.Second

// Dispatcher delivers notifications asynchronously; the Monitor hands off and
// never blocks on delivery. *dispatch.Dispatcher satisfies it.
type Dispatcher interface {
	Enqueue(n alert.Notification)
	Run(ctx context.Context)
}

// Deps are the collaborators the Monitor needs. Declared before Monitor per
// the >3-arg struct convention.
type Deps struct {
	Reader     sensor.Reader
	Dispatcher Dispatcher
	Detector   *detector.Detector
	Metrics    *metrics.Metrics
	Interval   time.Duration
	FailLimit  int
	Logger     *slog.Logger
}

// Monitor owns all mutable poll-loop state and runs in a single goroutine.
type Monitor struct {
	d          Deps
	fails      int
	sensorDown bool
}

// New returns a Monitor.
func New(d Deps) *Monitor { return &Monitor{d: d} }

// Run polls every Interval until ctx is cancelled. It reads once immediately.
// The notification dispatcher runs alongside on its own goroutine, tied to ctx.
func (m *Monitor) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() { m.d.Dispatcher.Run(ctx) })
	defer wg.Wait()

	ticker := time.NewTicker(m.d.Interval)
	defer ticker.Stop()
	m.step(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.step(ctx)
		}
	}
}

// step performs one poll cycle.
func (m *Monitor) step(ctx context.Context) {
	readCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	r, err := m.d.Reader.Read(readCtx)
	if err != nil {
		m.d.Metrics.IncReadError()
		m.fails++
		m.d.Logger.Warn("sensor read failed", "error", err, "consecutive", m.fails)
		if m.fails >= m.d.FailLimit && !m.sensorDown {
			m.sensorDown = true
			m.notify(alert.Notification{
				Title:    "BME280 sensor unavailable",
				Body:     fmt.Sprintf("no successful reading after %d attempts", m.fails),
				Priority: alert.High,
			})
		}
		return
	}
	if m.sensorDown {
		m.sensorDown = false
		m.notify(alert.Notification{
			Title:    "BME280 sensor recovered",
			Body:     "sensor reads have resumed",
			Priority: alert.Low,
		})
	}
	m.fails = 0
	m.d.Metrics.Update(r.Temperature, r.Humidity, r.Pressure)
	m.d.Logger.Info("reading", "humidity", r.Humidity, "temperature", r.Temperature, "pressure", r.Pressure)

	if n, ok := humidityNotification(m.d.Detector.Update(r.Humidity), r); ok {
		m.notify(n)
	}
}

// humidityNotification maps a detector transition to the notification to send.
// The second return is false when the event needs no notification.
func humidityNotification(event detector.Event, r sensor.Reading) (alert.Notification, bool) {
	body := fmt.Sprintf("Humidity %.1f%%, temperature %.1f°C", r.Humidity, r.Temperature)
	switch event {
	case detector.HighAlert:
		return alert.Notification{
			Title:    fmt.Sprintf("⚠️ Humidity high: %.0f%%", r.Humidity),
			Body:     body,
			Priority: alert.High,
		}, true
	case detector.Recovered:
		return alert.Notification{
			Title:    fmt.Sprintf("✅ Humidity back to normal: %.0f%%", r.Humidity),
			Body:     body,
			Priority: alert.Low,
		}, true
	case detector.None:
	}
	return alert.Notification{}, false
}

// notify hands a notification to the dispatcher for asynchronous, retrying
// delivery. It never blocks the poll loop.
func (m *Monitor) notify(n alert.Notification) {
	m.d.Dispatcher.Enqueue(n)
}
