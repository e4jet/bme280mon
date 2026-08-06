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

package sensor

import (
	"math"
	"testing"
	"time"

	"periph.io/x/conn/v3/physic"
)

func TestEnvToReading(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	env := physic.Env{
		Temperature: physic.ZeroCelsius + 25*physic.Celsius,
		Pressure:    101325 * physic.Pascal,
		Humidity:    50 * physic.PercentRH,
	}
	r := envToReading(env, now)
	if math.Abs(r.Temperature-25) > 0.01 {
		t.Errorf("Temperature = %v, want ~25", r.Temperature)
	}
	if math.Abs(r.Humidity-50) > 0.01 {
		t.Errorf("Humidity = %v, want ~50", r.Humidity)
	}
	if math.Abs(r.Pressure-1013.25) > 0.01 {
		t.Errorf("Pressure = %v, want ~1013.25", r.Pressure)
	}
	if !r.Time.Equal(now) {
		t.Errorf("Time = %v, want %v", r.Time, now)
	}
}
