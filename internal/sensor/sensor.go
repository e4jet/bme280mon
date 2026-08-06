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

// Package sensor reads a BME280 environmental sensor over I2C.
package sensor

import (
	"context"
	"errors"
	"time"

	"periph.io/x/conn/v3/physic"
)

// ErrRead is returned (wrapped) when a sensor read fails.
var ErrRead = errors.New("sensor read failed")

// Reading is a single environmental sample.
type Reading struct {
	Temperature float64 // degrees Celsius
	Humidity    float64 // percent relative humidity
	Pressure    float64 // hectopascals
	Time        time.Time
}

// Reader reads environmental samples. Close releases the underlying bus.
type Reader interface {
	Read(ctx context.Context) (Reading, error)
	Close() error
}

// envToReading converts a periph physic.Env into a Reading.
func envToReading(env physic.Env, now time.Time) Reading {
	return Reading{
		Temperature: env.Temperature.Celsius(),
		Humidity:    float64(env.Humidity) / float64(physic.PercentRH),
		Pressure:    float64(env.Pressure) / float64(100*physic.Pascal), //nolint:mnd // 1 hPa = 100 Pa
		Time:        now,
	}
}
