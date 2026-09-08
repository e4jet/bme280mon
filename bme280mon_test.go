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

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/e4jet/bme280mon/internal/alert"
)

type stubNotifier struct {
	sent []alert.Notification
	err  error
}

func (s *stubNotifier) Send(_ context.Context, n alert.Notification) error {
	s.sent = append(s.sent, n)
	return s.err
}

// errFatal stands in for an error that ends the run, e.g. a failed sensor open.
var errFatal = errors.New("open sensor: no such device")

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestStartedNotification(t *testing.T) {
	t.Parallel()

	n := startedNotification()
	if !strings.Contains(n.Body, version) {
		t.Errorf("body %q does not report version %q", n.Body, version)
	}
	if !strings.Contains(n.Body, hostname()) {
		t.Errorf("body %q does not report host %q", n.Body, hostname())
	}
	if n.Priority != alert.Low {
		t.Errorf("priority = %v, want %v", n.Priority, alert.Low)
	}
	if n.Title == "" {
		t.Error("title is empty")
	}
}

func TestStoppedNotification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "clean shutdown", err: nil, want: "clean shutdown"},
		{name: "fatal error", err: errFatal, want: "no such device"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n := stoppedNotification(tt.err)
			if !strings.Contains(n.Body, tt.want) {
				t.Errorf("body = %q, want it to contain %q", n.Body, tt.want)
			}
			if !strings.Contains(n.Body, hostname()) {
				t.Errorf("body %q does not report host %q", n.Body, hostname())
			}
			if n.Priority != alert.Low {
				t.Errorf("priority = %v, want %v", n.Priority, alert.Low)
			}
		})
	}
}

// With two instances on one Pi the hostname is identical, so the location
// (applied by alert.WithLocation, wrapping the notifier once in run()) is the
// only thing separating their lifecycle notices.
func TestLifecycleNotificationsCarryLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    alert.Notification
	}{
		{name: "started", n: startedNotification()},
		{name: "stopped", n: stoppedNotification(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubNotifier{}
			notifyLifecycle(t.Context(), alert.WithLocation("attic", stub), discardLogger(), tt.n)

			if len(stub.sent) != 1 {
				t.Fatalf("sent %d notifications, want 1", len(stub.sent))
			}
			if !strings.HasPrefix(stub.sent[0].Title, "attic: ") {
				t.Errorf("title = %q, want it to lead with the location", stub.sent[0].Title)
			}
		})
	}
}

func TestNotifyLifecycleSends(t *testing.T) {
	t.Parallel()

	stub := &stubNotifier{}
	want := startedNotification()
	notifyLifecycle(t.Context(), stub, discardLogger(), want)

	if len(stub.sent) != 1 {
		t.Fatalf("sent %d notifications, want 1", len(stub.sent))
	}
	if stub.sent[0] != want {
		t.Errorf("sent %+v, want %+v", stub.sent[0], want)
	}
}

// A failed lifecycle notice must be logged and swallowed: neither startup nor
// shutdown may be blocked by an unreachable ntfy server.
func TestNotifyLifecycleLogsFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	stub := &stubNotifier{err: alert.ErrSend}

	notifyLifecycle(t.Context(), stub, logger, stoppedNotification(nil))

	if !strings.Contains(buf.String(), "lifecycle notification failed") {
		t.Errorf("log = %q, want it to report the failure", buf.String())
	}
}

// The shutdown notice is sent after the signal context is already cancelled, so
// it must ride a context detached from it -- otherwise every shutdown notice
// dies before it leaves the process.
func TestShutdownNoticeSurvivesCancelledRunContext(t *testing.T) {
	t.Parallel()

	type received struct {
		title    string
		priority string
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- received{title: r.Header.Get("Title"), priority: r.Header.Get("Priority")}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	notifier := alert.New(alert.Options{Server: srv.URL, Topic: "test-topic", Timeout: 5 * time.Second})

	runCtx, cancel := context.WithCancel(t.Context())
	cancel() // the signal has arrived; the run context is gone

	want := stoppedNotification(nil)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
	t.Cleanup(cancelShutdown)
	notifyLifecycle(shutdownCtx, notifier, discardLogger(), want)

	select {
	case r := <-got:
		if r.title != want.Title {
			t.Errorf("title = %q, want %q", r.title, want.Title)
		}
		if r.priority != "low" {
			t.Errorf("priority = %q, want %q", r.priority, "low")
		}
	default:
		t.Fatal("shutdown notice never reached the server")
	}
}
