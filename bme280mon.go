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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reader, err := sensor.Open(cfg.I2CAddress)
	if err != nil {
		return fmt.Errorf("open sensor: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			logger.Error("close sensor", "error", err)
		}
	}()

	m := metrics.New()
	srvErr := make(chan error, 1)
	go func() { srvErr <- m.Serve(ctx, cfg.MetricsAddr) }()

	disp := dispatch.New(dispatch.Deps{
		Notifier: alert.New(alert.Options{Server: cfg.NtfyServer, Topic: cfg.NtfyTopic, Timeout: cfg.HTTPTimeout}),
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
		Logger:     logger,
	})

	runErr := mon.Run(ctx)

	// ctx is cancelled once Run returns; wait for the metrics server to drain.
	if err := <-srvErr; err != nil {
		logger.Error("metrics server", "error", err)
	}
	return runErr
}
