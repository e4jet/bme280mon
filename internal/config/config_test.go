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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Parallel()
	// Only the required topic is set; everything else should default.
	c, err := Load(writeTemp(t, "ntfy_topic: my-secret-topic\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", c.PollInterval)
	}
	if c.HumidityThreshold != 60 || c.HumidityBuffer != 3 {
		t.Errorf("threshold/buffer = %v/%v, want 60/3", c.HumidityThreshold, c.HumidityBuffer)
	}
	if c.I2CAddress != 0x76 {
		t.Errorf("I2CAddress = %#x, want 0x76", c.I2CAddress)
	}
	if c.HTTPTimeout != 10*time.Second {
		t.Errorf("HTTPTimeout = %v, want 10s", c.HTTPTimeout)
	}
	if c.TemperatureLowThreshold != 15 || c.TemperatureHighThreshold != 30 || c.TemperatureBuffer != 1 {
		t.Errorf("temperature low/high/buffer = %v/%v/%v, want 15/30/1",
			c.TemperatureLowThreshold, c.TemperatureHighThreshold, c.TemperatureBuffer)
	}
}

func TestLoadLocation(t *testing.T) {
	t.Parallel()

	c, err := Load(writeTemp(t, "ntfy_topic: t\nlocation: attic\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Location != "attic" {
		t.Errorf("Location = %q, want %q", c.Location, "attic")
	}

	// Omitting it is the single-sensor case and must stay valid.
	d, err := Load(writeTemp(t, "ntfy_topic: t\n"))
	if err != nil {
		t.Fatalf("Load without location: %v", err)
	}
	if d.Location != "" {
		t.Errorf("Location = %q, want empty by default", d.Location)
	}
}

func TestLoadAllowsLoopbackHTTP(t *testing.T) {
	t.Parallel()
	// Plaintext http is acceptable to a loopback host (topic never leaves the box).
	if _, err := Load(writeTemp(t, "ntfy_topic: t\nntfy_server: http://localhost:8080\n")); err != nil {
		t.Fatalf("Load with loopback http: %v", err)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing topic":                  "humidity_threshold: 60\n",
		"buffer >= threshold":            "ntfy_topic: t\nhumidity_threshold: 5\nhumidity_buffer: 5\n",
		"threshold over 100":             "ntfy_topic: t\nhumidity_threshold: 150\n",
		"bad address":                    "ntfy_topic: t\ni2c_address: 0x10\n",
		"bad duration":                   "ntfy_topic: t\npoll_interval: fortnight\n",
		"plaintext http":                 "ntfy_topic: t\nntfy_server: http://ntfy.example.com\n",
		"unsafe topic":                   "ntfy_topic: has/slash\n",
		"bare metrics port":              "ntfy_topic: t\nmetrics_addr: \"9101\"\n",
		"location with newline":          "ntfy_topic: t\nlocation: \"attic\\nX-Injected: 1\"\n",
		"location too long":              "ntfy_topic: t\nlocation: " + strings.Repeat("x", 65) + "\n",
		"temperature band inverted":      "ntfy_topic: t\ntemperature_low_threshold: 30\ntemperature_high_threshold: 15\n",
		"temperature band collapsed":     "ntfy_topic: t\ntemperature_low_threshold: 20\ntemperature_high_threshold: 20\n",
		"temperature low below range":    "ntfy_topic: t\ntemperature_low_threshold: -50\n",
		"temperature high over range":    "ntfy_topic: t\ntemperature_high_threshold: 90\n",
		"negative temperature buffer":    "ntfy_topic: t\ntemperature_buffer: -1\n",
		"temperature buffer at boundary": "ntfy_topic: t\ntemperature_buffer: 7.5\n",
		"temperature low NaN":            "ntfy_topic: t\ntemperature_low_threshold: .nan\n",
		"temperature high NaN":           "ntfy_topic: t\ntemperature_high_threshold: .nan\n",
		"temperature buffer NaN":         "ntfy_topic: t\ntemperature_buffer: .nan\n",
		"humidity threshold NaN":         "ntfy_topic: t\nhumidity_threshold: .nan\n",
		"humidity buffer NaN":            "ntfy_topic: t\nhumidity_buffer: .nan\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(writeTemp(t, body)); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Load(%q) error = %v, want ErrInvalidConfig", name, err)
			}
		})
	}
}

func TestLoadAcceptsMetricsAddrForms(t *testing.T) {
	t.Parallel()
	// The stack binds the sensor endpoint to loopback; the compiled default
	// stays ":9101". Both must survive validation.
	tests := map[string]string{
		"loopback host and port": "ntfy_topic: t\nmetrics_addr: \"127.0.0.1:9101\"\n",
		"port only":              "ntfy_topic: t\nmetrics_addr: \":9101\"\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(writeTemp(t, body)); err != nil {
				t.Fatalf("Load(%q) error = %v, want nil", name, err)
			}
		})
	}
}

func TestLoadAcceptsTemperatureBands(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		body              string
		low, high, buffer float64
	}{
		"custom band": {"ntfy_topic: t\ntemperature_low_threshold: 5\ntemperature_high_threshold: 35\ntemperature_buffer: 2\n", 5, 35, 2},
		// Default band is 15-to-30, width 15. 2*7.4 = 14.8 stays just under it,
		// where 7.5 (in the reject table above) does not.
		"buffer just inside the band": {"ntfy_topic: t\ntemperature_buffer: 7.4\n", 15, 30, 7.4},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := Load(writeTemp(t, tc.body))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.TemperatureLowThreshold != tc.low || c.TemperatureHighThreshold != tc.high || c.TemperatureBuffer != tc.buffer {
				t.Errorf("temperature low/high/buffer = %v/%v/%v, want %v/%v/%v",
					c.TemperatureLowThreshold, c.TemperatureHighThreshold, c.TemperatureBuffer, tc.low, tc.high, tc.buffer)
			}
		})
	}
}
