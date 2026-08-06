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

// Package dispatch delivers notifications asynchronously, retrying failed
// sends with exponential backoff and jitter. Only the most recent notification
// is kept: a new one supersedes any earlier one still being retried.
package dispatch

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/e4jet/bme280mon/internal/alert"
)

const (
	backoffBase = 1 * time.Second
	backoffCap  = 60 * time.Second
)

// ErrorCounter counts notification-delivery failures for observability.
type ErrorCounter interface {
	IncNotifyError()
}

// Deps are the collaborators the Dispatcher needs. Declared before Dispatcher
// per the >3-arg struct convention.
type Deps struct {
	Notifier alert.Notifier
	Metrics  ErrorCounter
	Logger   *slog.Logger
}

// Dispatcher delivers notifications on its own goroutine so callers never block
// on a slow or failing ntfy server.
type Dispatcher struct {
	notifier alert.Notifier
	metrics  ErrorCounter
	logger   *slog.Logger
	backoff  func(attempt int) time.Duration
	mailbox  chan alert.Notification
}

// New returns a Dispatcher. Run must be called to start delivery.
func New(d Deps) *Dispatcher {
	return &Dispatcher{
		notifier: d.Notifier,
		metrics:  d.Metrics,
		logger:   d.Logger,
		backoff:  jitteredBackoff,
		mailbox:  make(chan alert.Notification, 1),
	}
}

// Enqueue hands a notification to the Dispatcher without blocking. If one is
// still waiting to be delivered, it is replaced by n (latest wins).
func (d *Dispatcher) Enqueue(n alert.Notification) {
	for {
		select {
		case d.mailbox <- n:
			return
		case <-d.mailbox: // drop the stale notification, then insert n
		}
	}
}

// Run delivers enqueued notifications until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-d.mailbox:
			d.deliver(ctx, n)
		}
	}
}

// deliver sends n, retrying with backoff until it succeeds, ctx is cancelled,
// or a newer notification arrives and supersedes it.
func (d *Dispatcher) deliver(ctx context.Context, n alert.Notification) {
	attempt := 0
	for {
		err := d.notifier.Send(ctx, n)
		if err == nil {
			return
		}
		attempt++
		d.metrics.IncNotifyError()
		d.logger.Error("notification failed", "title", n.Title, "attempt", attempt, "error", err)
		timer := time.NewTimer(d.backoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case n = <-d.mailbox: // a newer notification supersedes this retry
			timer.Stop()
			attempt = 0
		case <-timer.C:
		}
	}
}

// jitteredBackoff grows the delay exponentially per attempt (capped) and
// applies full jitter, returning a random delay in [0, delay).
func jitteredBackoff(attempt int) time.Duration {
	delay := backoffBase
	for i := 1; i < attempt && delay < backoffCap; i++ {
		delay <<= 1
	}
	if delay > backoffCap {
		delay = backoffCap
	}
	return rand.N(delay)
}
