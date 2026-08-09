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

// Command bme280mon reads a BME280 sensor over I2C, alerts on high humidity
// via ntfy, and exposes Prometheus metrics.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/e4jet/bme280mon/internal/alert"
	"github.com/e4jet/bme280mon/internal/config"
	"github.com/e4jet/bme280mon/internal/detector"
	"github.com/e4jet/bme280mon/internal/dispatch"
	"github.com/e4jet/bme280mon/internal/metrics"
	"github.com/e4jet/bme280mon/internal/monitor"
	"github.com/e4jet/bme280mon/internal/sensor"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfgPath := flag.String("config", "/etc/bme280mon/config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("starting bme280mon", "version", version)

	if err := run(*cfgPath, logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
	logger.Info("bme280mon stopped")
}

func run(cfgPath string, logger *slog.Logger) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// NotifyContext is the signal trap: SIGINT/SIGTERM cancels ctx, which unwinds
	// serve and hands control back here while the process is still alive to
	// announce its own shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	notifier := alert.New(alert.Options{Server: cfg.NtfyServer, Topic: cfg.NtfyTopic, Timeout: cfg.HTTPTimeout})
	notifyLifecycle(ctx, notifier, logger, startedNotification(cfg.Location))

	err = serve(ctx, cfg, notifier, logger)

	// ctx is already cancelled by the signal that ended serve, so the shutdown
	// notice needs a context detached from it, bounded by the same timeout so an
	// unreachable ntfy server cannot hold the process past TimeoutStopSec.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.HTTPTimeout)
	defer cancel()
	notifyLifecycle(shutdownCtx, notifier, logger, stoppedNotification(cfg.Location, err))
	return err
}

// serve opens the sensor and runs the poll loop and metrics server until ctx is
// cancelled.
func serve(ctx context.Context, cfg config.Config, notifier alert.Notifier, logger *slog.Logger) error {
	reader, err := sensor.Open(cfg.I2CAddress)
	if err != nil {
		return fmt.Errorf("open sensor: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			logger.Error("close sensor", "error", err)
		}
	}()

	m := metrics.New(cfg.Location)
	// Bind before serving so a port already taken by another instance is a
	// startup failure (announced over ntfy) rather than a daemon that runs on
	// happily with no metrics endpoint.
	ln, err := m.Listen(ctx, cfg.MetricsAddr)
	if err != nil {
		return fmt.Errorf("metrics listen: %w", err)
	}
	srvErr := make(chan error, 1)
	go func() { srvErr <- m.Serve(ctx, ln) }()

	disp := dispatch.New(dispatch.Deps{
		Notifier: notifier,
		Metrics:  m,
		Logger:   logger,
	})

	mon := monitor.New(monitor.Deps{
		Reader:     reader,
		Dispatcher: disp,
		Detector:   detector.New(cfg.HumidityThreshold, cfg.HumidityBuffer),
		Metrics:    m,
		Interval:   cfg.PollInterval,
		FailLimit:  cfg.SensorFailLimit,
		Location:   cfg.Location,
		Logger:     logger,
	})

	runErr := mon.Run(ctx)

	// ctx is cancelled once Run returns; wait for the metrics server to drain.
	if err := <-srvErr; err != nil {
		logger.Error("metrics server", "error", err)
	}
	return runErr
}

// notifyLifecycle delivers a startup or shutdown notice synchronously. The
// dispatcher's retrying delivery is tied to the run context, which is already
// cancelled by the time we are shutting down, so these notices get one bounded
// attempt each; a failure is logged and never blocks starting or stopping.
func notifyLifecycle(ctx context.Context, notifier alert.Notifier, logger *slog.Logger, n alert.Notification) {
	if err := notifier.Send(ctx, n); err != nil {
		logger.Error("lifecycle notification failed", "title", n.Title, "error", err)
	}
}

// hostname reports the host for lifecycle notices, falling back to a
// placeholder rather than losing the notification.
func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return h
}

// startedNotification announces that this instance is up. The host identifies
// the Pi and location identifies the sensor on it, so neither several Pis on
// one topic nor several instances on one Pi are ambiguous.
func startedNotification(location string) alert.Notification {
	return alert.Notification{
		Title:    alert.Label(location, "▶️ bme280mon started"),
		Body:     fmt.Sprintf("version %s on %s", version, hostname()),
		Priority: alert.Low,
	}
}

// stoppedNotification announces that this instance is going away. A non-nil err
// is reported in the body so a restart loop is visible without reading the
// journal; config-load failures happen before ntfy is configured and so cannot
// be reported this way.
func stoppedNotification(location string, err error) alert.Notification {
	body := fmt.Sprintf("clean shutdown on %s", hostname())
	if err != nil {
		body = fmt.Sprintf("exited on %s with error: %v", hostname(), err)
	}
	return alert.Notification{
		Title:    alert.Label(location, "⏹️ bme280mon stopped"),
		Body:     body,
		Priority: alert.Low,
	}
}
