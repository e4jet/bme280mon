//go:build integration

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

package sensor

import (
	"context"
	"testing"
)

// TestReadRealSensor runs only on a Pi with a BME280 at 0x76.
// Run with: go test -tags integration ./internal/sensor/.
func TestReadRealSensor(t *testing.T) {
	t.Parallel()
	s, err := Open(0x76)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	r, err := s.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if r.Humidity < 0 || r.Humidity > 100 {
		t.Errorf("humidity out of range: %v", r.Humidity)
	}
	t.Logf("reading: %+v", r)
}
