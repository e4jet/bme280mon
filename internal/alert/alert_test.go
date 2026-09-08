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

package alert

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testTopic = "secret-topic"

// assertRedactedSendErr checks err wraps ErrSend and never leaks the topic.
func assertRedactedSendErr(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrSend) {
		t.Fatalf("error = %v, want ErrSend", err)
	}
	if strings.Contains(err.Error(), testTopic) {
		t.Errorf("error message leaked the topic: %q", err.Error())
	}
}

func TestSendPostsToTopic(t *testing.T) {
	t.Parallel()
	var gotPath, gotTitle, gotBody, gotPriority string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Options{Server: srv.URL, Topic: testTopic, Timeout: 2 * time.Second})
	err := n.Send(context.Background(), Notification{Title: "hi", Body: "world", Priority: High})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/secret-topic" {
		t.Errorf("path = %q, want /secret-topic", gotPath)
	}
	if gotTitle != "hi" || gotBody != "world" || gotPriority != "high" {
		t.Errorf("title/body/priority = %q/%q/%q", gotTitle, gotBody, gotPriority)
	}
}

func TestSendErrorIsWrappedAndRedactsTopic(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := New(Options{Server: srv.URL, Topic: testTopic, Timeout: 2 * time.Second})
	err := n.Send(context.Background(), Notification{Title: "t", Body: "b", Priority: Default})
	assertRedactedSendErr(t, err)
}

func TestSendTransportErrorRedactsTopic(t *testing.T) {
	t.Parallel()
	// Use an unreachable address to trigger a transport error.
	n := New(Options{Server: "http://127.0.0.1:1", Topic: testTopic, Timeout: 100 * time.Millisecond})
	err := n.Send(context.Background(), Notification{Title: "t", Body: "b", Priority: Default})
	assertRedactedSendErr(t, err)
}

func TestSendBuildRequestErrorRedactsTopic(t *testing.T) {
	t.Parallel()
	// Use a topic with a newline to trigger URL parsing error in NewRequest.
	n := New(Options{Server: "http://example.com", Topic: testTopic + "\n", Timeout: 2 * time.Second})
	err := n.Send(context.Background(), Notification{Title: "t", Body: "b", Priority: Default})
	assertRedactedSendErr(t, err)
}

type stubNotifier struct {
	sent []Notification
}

func (s *stubNotifier) Send(_ context.Context, n Notification) error {
	s.sent = append(s.sent, n)
	return nil
}

func TestWithLocationPrefixesTitle(t *testing.T) {
	t.Parallel()

	stub := &stubNotifier{}
	notifier := WithLocation("attic", stub)
	if err := notifier.Send(context.Background(), Notification{Title: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := stub.sent[0].Title; got != "attic: hi" {
		t.Errorf("title = %q, want %q", got, "attic: hi")
	}
}

func TestWithLocationEmptyReturnsSameNotifier(t *testing.T) {
	t.Parallel()

	stub := &stubNotifier{}
	if got := WithLocation("", stub); got != Notifier(stub) {
		t.Errorf("WithLocation(\"\", next) = %v, want next unchanged", got)
	}
}

func TestLabel(t *testing.T) {
	t.Parallel()
	const title = "⚠️ Humidity high: 63%"
	tests := []struct {
		name     string
		location string
		want     string
	}{
		{name: "empty location is unchanged", location: "", want: title},
		{name: "location leads the title", location: "attic", want: "attic: " + title},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Label(tt.location, title); got != tt.want {
				t.Errorf("Label(%q, %q) = %q, want %q", tt.location, title, got, tt.want)
			}
		})
	}
}
