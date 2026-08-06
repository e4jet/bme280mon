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

package dispatch

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/e4jet/bme280mon/internal/alert"
)

type stubNotifier struct {
	mu    sync.Mutex
	sent  []alert.Notification
	fail  func(n alert.Notification) bool
	calls chan alert.Notification
}

func (s *stubNotifier) Send(_ context.Context, n alert.Notification) error {
	s.mu.Lock()
	s.sent = append(s.sent, n)
	s.mu.Unlock()
	if s.calls != nil {
		s.calls <- n
	}
	if s.fail != nil && s.fail(n) {
		return alert.ErrSend
	}
	return nil
}

func (s *stubNotifier) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

type countingCounter struct {
	mu sync.Mutex
	n  int
}

func (c *countingCounter) IncNotifyError() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *countingCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// newTestDispatcher builds a Dispatcher with zero backoff for fast tests.
func newTestDispatcher(sn alert.Notifier, mc ErrorCounter) *Dispatcher {
	d := New(Deps{Notifier: sn, Metrics: mc, Logger: slog.New(slog.DiscardHandler)})
	d.backoff = func(int) time.Duration { return 0 }
	return d
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestDeliversEnqueuedNotification(t *testing.T) {
	t.Parallel()
	sn := &stubNotifier{}
	d := newTestDispatcher(sn, &countingCounter{})
	ctx := t.Context()
	go d.Run(ctx)

	d.Enqueue(alert.Notification{Title: "hi"})
	waitFor(t, func() bool { return sn.count() >= 1 })
}

func TestRetriesUntilDelivered(t *testing.T) {
	t.Parallel()
	// Fail the first three sends, then succeed.
	var attempts int
	var mu sync.Mutex
	sn := &stubNotifier{fail: func(alert.Notification) bool {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		return attempts <= 3
	}}
	mc := &countingCounter{}
	d := newTestDispatcher(sn, mc)
	ctx := t.Context()
	go d.Run(ctx)

	d.Enqueue(alert.Notification{Title: "persist"})
	waitFor(t, func() bool { return sn.count() >= 4 })
	if got := mc.count(); got != 3 {
		t.Errorf("notify errors = %d, want 3", got)
	}
}

func TestNewerNotificationSupersedesRetry(t *testing.T) {
	t.Parallel()
	calls := make(chan alert.Notification, 8)
	// "A" always fails; anything else succeeds.
	sn := &stubNotifier{calls: calls, fail: func(n alert.Notification) bool { return n.Title == "A" }}
	d := New(Deps{Notifier: sn, Metrics: &countingCounter{}, Logger: slog.New(slog.DiscardHandler)})
	// A long backoff parks the retry so only a supersede can wake it.
	d.backoff = func(int) time.Duration { return time.Hour }
	ctx := t.Context()
	go d.Run(ctx)

	d.Enqueue(alert.Notification{Title: "A"})
	if got := (<-calls).Title; got != "A" {
		t.Fatalf("first attempt = %q, want A", got)
	}
	// A failed and is parked in backoff; a newer notification supersedes it.
	d.Enqueue(alert.Notification{Title: "B"})
	if got := (<-calls).Title; got != "B" {
		t.Fatalf("after supersede = %q, want B", got)
	}
	// B succeeded; the dropped A must not be retried.
	select {
	case n := <-calls:
		t.Fatalf("unexpected extra send: %q", n.Title)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestEnqueueKeepsLatestWhilePending(t *testing.T) {
	t.Parallel()
	calls := make(chan alert.Notification, 8)
	sn := &stubNotifier{calls: calls}
	d := newTestDispatcher(sn, &countingCounter{})

	// Both enqueued before Run consumes; the later one replaces the earlier.
	d.Enqueue(alert.Notification{Title: "A"})
	d.Enqueue(alert.Notification{Title: "B"})

	ctx := t.Context()
	go d.Run(ctx)

	if got := (<-calls).Title; got != "B" {
		t.Fatalf("delivered = %q, want B (latest)", got)
	}
	select {
	case n := <-calls:
		t.Fatalf("unexpected extra send: %q", n.Title)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	t.Parallel()
	sn := &stubNotifier{fail: func(alert.Notification) bool { return true }}
	d := New(Deps{Notifier: sn, Metrics: &countingCounter{}, Logger: slog.New(slog.DiscardHandler)})
	d.backoff = func(int) time.Duration { return time.Hour }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()

	d.Enqueue(alert.Notification{Title: "stuck"})
	waitFor(t, func() bool { return sn.count() >= 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
