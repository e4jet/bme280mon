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

// Package metrics exposes BME280 readings as Prometheus metrics.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTP server timeouts for the metrics endpoint.
const (
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 5 * time.Second
)

// Metrics holds the gauges/counters and their private registry.
type Metrics struct {
	reg          *prometheus.Registry
	humidity     prometheus.Gauge
	temperature  prometheus.Gauge
	pressure     prometheus.Gauge
	readErrors   prometheus.Counter
	notifyErrors prometheus.Counter
}

// New builds a Metrics with its own registry (so tests are isolated). A
// non-empty location is attached to every series as a constant `location`
// label, which is how two instances on one host stay apart in Prometheus;
// an empty location emits the same unlabelled series as a single-sensor host.
func New(location string) *Metrics {
	labels := prometheus.Labels{}
	if location != "" {
		labels["location"] = location
	}
	m := &Metrics{
		reg:          prometheus.NewRegistry(),
		humidity:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "bme280_humidity_percent", Help: "Relative humidity in percent.", ConstLabels: labels}),
		temperature:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "bme280_temperature_celsius", Help: "Temperature in degrees Celsius.", ConstLabels: labels}),
		pressure:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "bme280_pressure_hpa", Help: "Atmospheric pressure in hectopascals.", ConstLabels: labels}),
		readErrors:   prometheus.NewCounter(prometheus.CounterOpts{Name: "bme280_read_errors_total", Help: "Total sensor read failures.", ConstLabels: labels}),
		notifyErrors: prometheus.NewCounter(prometheus.CounterOpts{Name: "bme280_notify_errors_total", Help: "Total notification send failures.", ConstLabels: labels}),
	}
	m.reg.MustRegister(m.humidity, m.temperature, m.pressure, m.readErrors, m.notifyErrors)
	return m
}

// Update records the latest reading.
func (m *Metrics) Update(temperatureC, humidityPct, pressureHPa float64) {
	m.temperature.Set(temperatureC)
	m.humidity.Set(humidityPct)
	m.pressure.Set(pressureHPa)
}

// IncReadError increments the read-error counter.
func (m *Metrics) IncReadError() { m.readErrors.Inc() }

// IncNotifyError increments the notification-send-error counter.
func (m *Metrics) IncNotifyError() { m.notifyErrors.Inc() }

// Handler returns an http.Handler serving the registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Listen binds addr. It is separate from Serve so that a caller running Serve
// on its own goroutine can still fail fast on a bind error: with two instances
// on one host, a duplicated metrics_addr is the likeliest misconfiguration, and
// it must not degrade into a silently metric-less daemon.
func (m *Metrics) Listen(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return ln, nil
}

// Serve runs the metrics HTTP server on ln with explicit timeouts until ctx is
// cancelled, then gracefully drains it. Its lifetime is tied to ctx, and it
// takes ownership of ln.
func (m *Metrics) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:      m.Handler(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}
	errCh := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// ctx is already cancelled here, so the drain needs a fresh deadline.
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutCtx) //nolint:contextcheck // fresh ctx is required to drain after ctx cancellation
	}
}
