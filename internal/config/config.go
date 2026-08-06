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

// Package config loads and validates the immutable bme280mon configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrInvalidConfig is returned (wrapped) when configuration fails validation.
var ErrInvalidConfig = errors.New("invalid config")

// Config is the immutable runtime configuration.
type Config struct {
	PollInterval      time.Duration
	HumidityThreshold float64
	HumidityBuffer    float64
	I2CAddress        uint16
	NtfyServer        string
	NtfyTopic         string
	MetricsAddr       string
	SensorFailLimit   int
	HTTPTimeout       time.Duration
}

// rawConfig mirrors the YAML file; durations are strings so we can report a
// clear validation error on a bad value.
type rawConfig struct {
	PollInterval      string  `yaml:"poll_interval"`
	HumidityThreshold float64 `yaml:"humidity_threshold"`
	HumidityBuffer    float64 `yaml:"humidity_buffer"`
	I2CAddress        uint16  `yaml:"i2c_address"`
	NtfyServer        string  `yaml:"ntfy_server"`
	NtfyTopic         string  `yaml:"ntfy_topic"`
	MetricsAddr       string  `yaml:"metrics_addr"`
	SensorFailLimit   int     `yaml:"sensor_fail_limit"`
	HTTPTimeout       string  `yaml:"http_timeout"`
}

//nolint:mnd
func defaults() rawConfig {
	return rawConfig{
		PollInterval:      "30s",
		HumidityThreshold: 60,
		HumidityBuffer:    3,
		I2CAddress:        0x76,
		NtfyServer:        "https://ntfy.sh",
		MetricsAddr:       ":9101",
		SensorFailLimit:   5,
		HTTPTimeout:       "10s",
	}
}

// isLoopbackHost reports whether host is localhost or a loopback IP, for which
// plaintext http is acceptable because the topic never leaves the machine.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// parseDur parses a duration-valued config field, wrapping a bad value with
// ErrInvalidConfig and the field name for a clear validation error.
func parseDur(field, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", field, value, errors.Join(ErrInvalidConfig, err))
	}
	return d, nil
}

// Load reads, defaults, parses, and validates the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	raw := defaults()
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, errors.Join(ErrInvalidConfig, err))
	}
	poll, err := parseDur("poll_interval", raw.PollInterval)
	if err != nil {
		return Config{}, err
	}
	timeout, err := parseDur("http_timeout", raw.HTTPTimeout)
	if err != nil {
		return Config{}, err
	}
	c := Config{
		PollInterval:      poll,
		HumidityThreshold: raw.HumidityThreshold,
		HumidityBuffer:    raw.HumidityBuffer,
		I2CAddress:        raw.I2CAddress,
		NtfyServer:        raw.NtfyServer,
		NtfyTopic:         raw.NtfyTopic,
		MetricsAddr:       raw.MetricsAddr,
		SensorFailLimit:   raw.SensorFailLimit,
		HTTPTimeout:       timeout,
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Validate enforces the invariants documented in the design spec.
//
//nolint:cyclop
func (c Config) Validate() error {
	if c.NtfyTopic == "" {
		return fmt.Errorf("ntfy_topic is required: %w", ErrInvalidConfig)
	}
	// The topic travels in the URL path, so it must be usable there verbatim
	// (no characters that would need percent-encoding, e.g. '/' or spaces).
	if url.PathEscape(c.NtfyTopic) != c.NtfyTopic {
		return fmt.Errorf("ntfy_topic contains characters that are not URL-path safe: %w", ErrInvalidConfig)
	}
	u, err := url.Parse(c.NtfyServer)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("ntfy_server %q must be a valid http(s) URL: %w", c.NtfyServer, ErrInvalidConfig)
	}
	// The topic acts as an access token, so it must not cross the network in
	// cleartext. Plaintext http is allowed only to a loopback host, where it
	// never leaves the machine.
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("ntfy_server %q uses plaintext http to a non-loopback host, which would leak the secret topic; use https: %w", c.NtfyServer, ErrInvalidConfig)
	}
	if c.HumidityThreshold <= 0 || c.HumidityThreshold > 100 {
		return fmt.Errorf("humidity_threshold %.1f out of range (0,100]: %w", c.HumidityThreshold, ErrInvalidConfig)
	}
	if c.HumidityBuffer < 0 || c.HumidityBuffer >= c.HumidityThreshold {
		return fmt.Errorf("humidity_buffer %.1f must be in [0, threshold): %w", c.HumidityBuffer, ErrInvalidConfig)
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be > 0: %w", ErrInvalidConfig)
	}
	if c.HTTPTimeout <= 0 {
		return fmt.Errorf("http_timeout must be > 0: %w", ErrInvalidConfig)
	}
	if c.SensorFailLimit < 1 {
		return fmt.Errorf("sensor_fail_limit must be >= 1: %w", ErrInvalidConfig)
	}
	if c.I2CAddress != 0x76 && c.I2CAddress != 0x77 {
		return fmt.Errorf("i2c_address %#x must be 0x76 or 0x77: %w", c.I2CAddress, ErrInvalidConfig)
	}
	if _, _, err := net.SplitHostPort(c.MetricsAddr); err != nil {
		return fmt.Errorf("metrics_addr %q invalid: %w", c.MetricsAddr, ErrInvalidConfig)
	}
	return nil
}
