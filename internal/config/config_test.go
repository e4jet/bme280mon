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
		"missing topic":       "humidity_threshold: 60\n",
		"buffer >= threshold": "ntfy_topic: t\nhumidity_threshold: 5\nhumidity_buffer: 5\n",
		"threshold over 100":  "ntfy_topic: t\nhumidity_threshold: 150\n",
		"bad address":         "ntfy_topic: t\ni2c_address: 0x10\n",
		"bad duration":        "ntfy_topic: t\npoll_interval: fortnight\n",
		"plaintext http":      "ntfy_topic: t\nntfy_server: http://ntfy.example.com\n",
		"unsafe topic":        "ntfy_topic: has/slash\n",
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
