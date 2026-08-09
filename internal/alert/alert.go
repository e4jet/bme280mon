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

// Package alert delivers notifications to an ntfy topic over HTTP(S).
package alert

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrSend is returned (wrapped) when a notification cannot be delivered.
var ErrSend = errors.New("ntfy send failed")

// redactedCause returns a cause safe to include in error messages: for a
// *url.Error (from http.NewRequest/client.Do) it returns the inner error,
// which omits the request URL (and therefore the secret topic).
func redactedCause(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// Priority maps to ntfy's Priority header.
type Priority int

const (
	// Default priority.
	Default Priority = iota
	// Low priority (used for recovery notices).
	Low
	// High priority (used for high-humidity and sensor-down alerts).
	High
)

func (p Priority) header() string {
	switch p {
	case Default:
		return "default"
	case Low:
		return "low"
	case High:
		return "high"
	}
	return "default"
}

// Notification is a single message to deliver.
type Notification struct {
	Title    string
	Body     string
	Priority Priority
}

// Label prefixes a notification title with location, so several sensors sharing
// a host or an ntfy topic stay distinguishable on a phone's lock screen. An
// empty location returns title unchanged, keeping single-sensor deployments
// byte-identical to an unlabelled one.
func Label(location, title string) string {
	if location == "" {
		return title
	}
	return location + ": " + title
}

// Notifier delivers notifications.
type Notifier interface {
	Send(ctx context.Context, n Notification) error
}

// WithLocation wraps next so every notification sent through it has its title
// prefixed with location, letting callers work with a plain Notifier without
// threading location through themselves. An empty location returns next
// unchanged, so a single-sensor deployment pays no cost for the wrapper.
func WithLocation(location string, next Notifier) Notifier {
	if location == "" {
		return next
	}
	return locatedNotifier{location: location, next: next}
}

// locatedNotifier prefixes every notification's title with location before
// delegating to next.
type locatedNotifier struct {
	location string
	next     Notifier
}

func (l locatedNotifier) Send(ctx context.Context, n Notification) error {
	n.Title = Label(l.location, n.Title)
	return l.next.Send(ctx, n)
}

// Options configures a Ntfy client.
type Options struct {
	Server  string
	Topic   string
	Timeout time.Duration
}

// Ntfy is a Notifier backed by an ntfy server.
type Ntfy struct {
	server string
	topic  string
	client *http.Client
}

// New returns a Ntfy client. The topic is treated as a secret and never logged.
func New(o Options) *Ntfy {
	return &Ntfy{
		server: strings.TrimRight(o.Server, "/"),
		topic:  o.Topic,
		client: &http.Client{Timeout: o.Timeout},
	}
}

// Send posts the notification to the configured topic. Error messages include
// the server but never the topic (which acts as an access token).
func (c *Ntfy) Send(ctx context.Context, n Notification) error {
	endpoint := c.server + "/" + c.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(n.Body))
	if err != nil {
		return fmt.Errorf("build ntfy request to %s: %w", c.server, errors.Join(ErrSend, redactedCause(err)))
	}
	req.Header.Set("Title", n.Title)
	req.Header.Set("Priority", n.Priority.header())
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post to ntfy %s: %w", c.server, errors.Join(ErrSend, redactedCause(err)))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ntfy %s returned status %d: %w", c.server, resp.StatusCode, ErrSend)
	}
	return nil
}
