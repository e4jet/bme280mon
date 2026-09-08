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
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/devices/v3/bmxx80"
	"periph.io/x/host/v3"
)

// BME280 is a Reader backed by a periph.io I2C BME280 device.
type BME280 struct {
	dev *bmxx80.Dev
	bus i2c.BusCloser
}

// Open initializes the periph host, opens the default I2C bus, and configures
// the BME280 at addr (0x76 or 0x77).
func Open(addr uint16) (*BME280, error) {
	if err := hostInit(); err != nil {
		return nil, fmt.Errorf("periph host init: %w", err)
	}
	bus, err := i2creg.Open("")
	if err != nil {
		return nil, fmt.Errorf("open i2c bus: %w", err)
	}
	dev, err := bmxx80.NewI2C(bus, addr, &bmxx80.DefaultOpts)
	if err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("init bme280 at %#x: %w", addr, err)
	}
	return &BME280{dev: dev, bus: bus}, nil
}

// hostInit runs periph's host.Init while suppressing its standard-logger
// output. host.Init eagerly initializes every registered driver and logs a
// failure line for each device it cannot open, including the GPIO chips this
// daemon neither uses nor is granted under its least-privilege sandbox. Real
// I2C failures still surface through the errors returned by i2creg.Open and
// bmxx80.NewI2C below, which run with logging restored.
func hostInit() error {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(prev)
	_, err := host.Init()
	return err
}

// Read takes one sample, honoring ctx for cancellation and deadlines. periph's
// Sense is not interruptible, so on ctx cancellation the blocked read is
// abandoned in its goroutine (its result discarded) and the bus is released
// only when that call finally returns; the buffered channel avoids leaking the
// goroutine's send.
func (b *BME280) Read(ctx context.Context) (Reading, error) {
	var env physic.Env
	done := make(chan error, 1)
	go func() { done <- b.dev.Sense(&env) }()
	select {
	case <-ctx.Done():
		return Reading{}, fmt.Errorf("bme280 sense: %w", errors.Join(ErrRead, ctx.Err()))
	case err := <-done:
		if err != nil {
			return Reading{}, fmt.Errorf("bme280 sense: %w", errors.Join(ErrRead, err))
		}
		return envToReading(env, time.Now()), nil
	}
}

// Close releases the I2C bus.
func (b *BME280) Close() error { return b.bus.Close() }
