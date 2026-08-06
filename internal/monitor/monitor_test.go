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

package monitor

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/e4jet/bme280mon/internal/alert"
	"github.com/e4jet/bme280mon/internal/detector"
	"github.com/e4jet/bme280mon/internal/metrics"
	"github.com/e4jet/bme280mon/internal/sensor"
)

type readResult struct {
	r   sensor.Reading
	err error
}

type fakeReader struct {
	seq []readResult
	i   int
}

func (f *fakeReader) Read(_ context.Context) (sensor.Reading, error) {
	if f.i >= len(f.seq) {
		return sensor.Reading{}, sensor.ErrRead
	}
	res := f.seq[f.i]
	f.i++
	return res.r, res.err
}
func (f *fakeReader) Close() error { return nil }

// fakeDispatcher records enqueued notifications synchronously; the tests drive
// step() directly and never start Run, so the recording needs no goroutine.
type fakeDispatcher struct{ sent []alert.Notification }

func (f *fakeDispatcher) Enqueue(n alert.Notification) { f.sent = append(f.sent, n) }
func (f *fakeDispatcher) Run(ctx context.Context)      { <-ctx.Done() }

func hum(h float64) readResult { return readResult{r: sensor.Reading{Humidity: h, Time: time.Now()}} }
func fail() readResult         { return readResult{err: sensor.ErrRead} }

// newTestMonitor wires a Monitor with fixed test settings (threshold 60/buffer 3,
// fail limit 3), varying only the reader and dispatcher under test.
func newTestMonitor(fr sensor.Reader, fd Dispatcher) *Monitor {
	return New(Deps{
		Reader:     fr,
		Dispatcher: fd,
		Detector:   detector.New(60, 3),
		Metrics:    metrics.New(),
		Interval:   time.Hour,
		FailLimit:  3,
		Logger:     slog.New(slog.DiscardHandler),
	})
}

func TestStepEmitsAlertsOnTransitions(t *testing.T) {
	t.Parallel()
	fr := &fakeReader{seq: []readResult{
		hum(50), // normal, no alert
		hum(65), // -> HighAlert
		hum(66), // still high, no alert
		hum(55), // -> Recovered (<= 57)
	}}
	fd := &fakeDispatcher{}
	m := newTestMonitor(fr, fd)
	ctx := context.Background()
	for range fr.seq {
		m.step(ctx)
	}
	if len(fd.sent) != 2 {
		t.Fatalf("sent %d notifications, want 2: %+v", len(fd.sent), fd.sent)
	}
	if fd.sent[0].Priority != alert.High {
		t.Errorf("first alert priority = %v, want High", fd.sent[0].Priority)
	}
	if fd.sent[1].Priority != alert.Low {
		t.Errorf("recovery alert priority = %v, want Low", fd.sent[1].Priority)
	}
}

func TestStepAlertsAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()
	fr := &fakeReader{seq: []readResult{fail(), fail(), fail(), hum(40)}}
	fd := &fakeDispatcher{}
	m := newTestMonitor(fr, fd)
	ctx := context.Background()
	for range fr.seq {
		m.step(ctx)
	}
	// One "unavailable" alert at the 3rd failure, one "recovered" alert on the read.
	if len(fd.sent) != 2 {
		t.Fatalf("sent %d notifications, want 2: %+v", len(fd.sent), fd.sent)
	}
	if fd.sent[0].Priority != alert.High || fd.sent[1].Priority != alert.Low {
		t.Errorf("priorities = %v/%v, want High/Low", fd.sent[0].Priority, fd.sent[1].Priority)
	}
}
