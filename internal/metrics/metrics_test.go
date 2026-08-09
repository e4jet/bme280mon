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

package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestUpdateSetsGauges(t *testing.T) {
	t.Parallel()
	m := New("")
	m.Update(21.5, 63.2, 1013.25)
	if got := testutil.ToFloat64(m.humidity); got != 63.2 {
		t.Errorf("humidity = %v, want 63.2", got)
	}
	if got := testutil.ToFloat64(m.temperature); got != 21.5 {
		t.Errorf("temperature = %v, want 21.5", got)
	}
	if got := testutil.ToFloat64(m.pressure); got != 1013.25 {
		t.Errorf("pressure = %v, want 1013.25", got)
	}
	m.IncReadError()
	if got := testutil.ToFloat64(m.readErrors); got != 1 {
		t.Errorf("readErrors = %v, want 1", got)
	}
	m.IncNotifyError()
	if got := testutil.ToFloat64(m.notifyErrors); got != 1 {
		t.Errorf("notifyErrors = %v, want 1", got)
	}
}

func TestHandlerExposesMetricNames(t *testing.T) {
	t.Parallel()
	m := New("")
	m.Update(20, 50, 1000)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, name := range []string{"bme280_humidity_percent", "bme280_temperature_celsius", "bme280_pressure_hpa"} {
		if !strings.Contains(body, name) {
			t.Errorf("metrics output missing %q", name)
		}
	}
}

func TestLocationLabel(t *testing.T) {
	t.Parallel()

	labelled := New("attic")
	labelled.Update(20, 50, 1000)
	rec := httptest.NewRecorder()
	labelled.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `bme280_humidity_percent{location="attic"}`) {
		t.Errorf("labelled output missing location label:\n%s", rec.Body.String())
	}

	// An unset location must leave the single-sensor series exactly as it was,
	// so existing dashboards and alert rules keep matching.
	plain := New("")
	plain.Update(20, 50, 1000)
	rec = httptest.NewRecorder()
	plain.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "bme280_humidity_percent 50") {
		t.Errorf("unlabelled output should have no label set:\n%s", rec.Body.String())
	}
}

func TestListenReturnsBindError(t *testing.T) {
	t.Parallel()
	// A port out of range fails to bind, so Listen reports it to the caller
	// before any serving goroutine is started.
	if _, err := New("").Listen(t.Context(), "127.0.0.1:99999"); err == nil {
		t.Fatal("Listen returned nil, want a bind error")
	}
}

// Two instances on one host must not both claim the same metrics_addr: the
// second has to fail at bind time rather than run on without an endpoint.
func TestListenRejectsAddressAlreadyInUse(t *testing.T) {
	t.Parallel()

	first, err := New("basement").Listen(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := New("attic").Listen(t.Context(), first.Addr().String()); err == nil {
		t.Error("second Listen on the same address succeeded, want a bind error")
	}
}

func TestServeShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	m := New("")
	m.Update(20, 50, 1000)
	// Listen picks a free port and holds it, so there is no window in which
	// another test could claim the address between bind and serve.
	ln, err := m.Listen(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- m.Serve(ctx, ln) }()

	// Poll until the endpoint is serving.
	var body string
	for range 50 {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			body = string(b)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(body, "bme280_humidity_percent") {
		t.Fatalf("metrics endpoint not serving; body = %q", body)
	}

	// Cancelling ctx should gracefully drain and return nil.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}
