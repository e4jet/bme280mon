# bme280mon — BME280 humidity/temperature monitor for Raspberry Pi 4

**Date:** 2026-08-04
**Status:** Approved design, pending implementation plan
**Repo:** `github.com/e4jet/bme280mon` (`/Users/e4jet/repos/bme280mon`)

## Purpose

Monitor a room's humidity (and record temperature/pressure) using a GY-BME280
sensor on a Raspberry Pi 4, and push a phone alert via ntfy when humidity goes
high — once per event, with no spam. Readings are also exposed as Prometheus
metrics for later scraping into VictoriaMetrics.

## Project guidelines (binding)

This project **MUST** follow `docs/project-guidelines.md` (RFC-2119 rules,
enforced by CI/review). The requirements below are woven into the design; the
most load-bearing for this project are:

- **CMD:** config comes from a **config file**, treated as immutable after init;
  sane defaults with clear docs.
- **Contexts:** `ctx context.Context` is the first parameter to functions doing
  I/O; never stored in structs; used for cancellation.
- **Errors:** wrap with `%w` + context; control flow via `errors.Is/As`; sentinel
  errors defined per package.
- **Logging:** structured `slog` with levels; version string logged at startup.
- **Security:** validate all inputs; explicit I/O timeouts; prefer TLS; never log
  secrets (the ntfy topic is treated as sensitive — see Alerting); least privilege.
- **Concurrency:** goroutine lifetime tied to `context.Context`; sender closes
  channels; shared state protected.
- **Testing:** table-driven, deterministic, hermetic; `-race`; unit tests tagged
  `//go:build unit` (`xxx_test.go`), integration tests `//go:build integration`
  (`xxx_integration_test.go`).
- **Modules:** prefer stdlib; only add deps with clear payoff; no GPL/LGPL/copyleft
  licenses; run `govulncheck ./...`.
- **CI/Branching:** lint, vet, test (`-race`), build on every PR; develop on
  branches, merge to `main` via PR.

## Hardware

- **Sensor:** GY-BME280 breakout (advertised "5V" variant), connected over **I2C**.
- **Confirmed I2C address:** `0x76` on bus 1, powered from the Pi's **3.3V** pin
  (works — no 5V needed).

### Wiring (I2C, 4 wires)

| BME280 pin | Pi 4 physical pin | Signal        |
|------------|-------------------|---------------|
| VIN / VCC  | Pin 1 (3.3V)      | power         |
| GND        | Pin 6 (GND)       | ground        |
| SCL        | Pin 5 (GPIO3/SCL1)| I2C clock     |
| SDA        | Pin 3 (GPIO2/SDA1)| I2C data      |

Rationale for 3.3V: the Pi's GPIO is 3.3V logic; powering the board at 3.3V keeps
the onboard I2C pull-ups from pulling SDA/SCL above 3.3V. Verified working via
`i2cdetect -y 1` showing the device at `0x76`.

### One-time Pi bring-up

1. Enable I2C: `sudo raspi-config` → Interface Options → I2C (or `dtparam=i2c_arm=on`), reboot.
2. `sudo apt install i2c-tools`
3. `i2cdetect -y 1` → confirm device at `0x76`.

## Software

### Technology choice

- **Language:** Go 1.26.
- **Sensor access:** [periph.io](https://periph.io) — pure-Go I2C host + built-in
  `bmxx80` BME280 driver. No CGo, so it cross-compiles cleanly from macOS to the Pi.
- **Metrics:** `prometheus/client_golang`.

### Project conventions

- **Module path:** `github.com/e4jet/bme280mon`, `go 1.26`.
- **Layout:** the `main` package lives in `bme280mon.go` at the repo root, with logic in `internal/` packages.
- **Apache-2.0** license header block on every Go source file.
- **Lint:** `golangci-lint` driven by a `.golangci.yml` copied/adapted from pirewall.
- **Test build tag:** unit tests carry `//go:build unit` and run with
  `go test -race -tags unit -coverprofile=bme280mon.coverprofile`.
- **Makefile** mirrors pirewall's targets and style (see Build & Deploy).

### Layout

**Dependencies** (all permissively licensed — no GPL/LGPL/copyleft, per
guidelines): `periph.io/x/*` (Apache-2.0), `prometheus/client_golang`
(Apache-2.0), a YAML parser such as `sigs.k8s.io/yaml` or `gopkg.in/yaml.v3`
(Apache-2.0 / MIT). Licenses are re-checked before adding, and `govulncheck`
runs via `make vuln`.

Each unit has one job and a clear interface. Per the guidelines, `ctx` is the
first parameter to any method doing I/O, functions taking >3 args use an input
struct (declared before the function, ctx excluded), and each package defines its
own sentinel errors.

- **sensor** — `Reader` interface: `Read(ctx context.Context) (Reading, error)`
  where `Reading{ Temperature, Humidity, Pressure float64; Time time.Time }`.
  Real implementation opens the I2C bus at `0x76` via periph.io; a fake
  implementation drives tests without hardware. Sentinel: `ErrRead`.
- **detector** — pure state machine, no network/clock/ctx. Given `(state, reading)`
  returns `(newState, event)`. See below.
- **alert** — `Notifier` interface:
  `Send(ctx context.Context, n Notification) error` where
  `Notification{ Title, Body string; Priority Priority }`. Real implementation
  POSTs to `<ntfy-server>/<topic>` with an explicit HTTP client timeout. Sentinel: `ErrSend`.
- **metrics** — registers gauges, `Update(Reading)` sets them, serves `/metrics`
  from an `http.Server` with explicit read/write/idle timeouts, its goroutine
  tied to the root context.

### Data flow (per poll tick)

```
ticker → sensor.Read() ──► metrics.Update(reading)
                       └─► detector.Update(reading) ──► event? ──► alert.Send(ntfy)
```

### Detector: hysteresis state machine

Two states, `Normal` and `High`:

- **Normal:** if `humidity >= threshold` → emit `HighAlert`, transition to `High`.
- **High:** stay quiet until `humidity <= threshold - buffer` → emit `Recovered`
  (low priority), transition to `Normal`.

The `buffer` (hysteresis gap) prevents flapping when humidity hovers near the
threshold. Defaults: **threshold 60%, buffer 3%** (recover at 57%). Both configurable.

This is the only unit with real logic and is pure — trivially unit-tested for
rising, falling, and flapping-near-threshold cases.

### Alerting (ntfy)

- On `HighAlert`: POST to `<ntfy-server>/<topic>` with title e.g.
  `⚠️ Humidity high: 63%` and body including current temperature.
- On `Recovered`: low-priority note (e.g. `✅ Humidity back to normal: 56%`).
- Server URL and topic come from config so a self-hosted ntfy can be swapped in later.
- Temperature is **recorded only** — it never triggers alerts (per requirements).
- Transport uses **TLS** (default `https://ntfy.sh`); the HTTP client sets an
  **explicit timeout**. On a public ntfy server the topic name is effectively an
  unguessable access token, so it is **treated as a secret**: never logged, and
  redacted in any error/diagnostic output (guidelines: "MUST NOT log secrets").
  Send failures wrap `ErrSend` with `%w` and are logged + counted, not fatal.

### Metrics

`/metrics` on a configurable port (default `:9101`), exposing:

- `bme280_humidity_percent`
- `bme280_temperature_celsius`
- `bme280_pressure_hpa`
- `bme280_read_errors_total`

Ready for VictoriaMetrics to scrape.

### Error handling

- Each package defines sentinel errors (`sensor.ErrRead`, `alert.ErrSend`);
  callers wrap with `%w` + context (`fmt.Errorf("read sensor: %w", err)`) and
  branch with `errors.Is/As` — never string matching.
- A failed sensor read is logged (`slog`, warn) and skipped; the loop continues.
- Because a silently-dead sensor would stop all humidity alerts, after **N
  consecutive failures** (default 5) the tool sends **one** ntfy alert
  ("sensor unavailable"), and a "sensor recovered" note when reads resume — the
  same one-shot discipline as humidity alerts.
- `bme280_read_errors_total` tracks failures for observability.

### Concurrency & lifecycle

- The **poll loop** owns all mutable state (detector state, consecutive-failure
  counter) and runs in a single goroutine, so that state needs no locking.
- The **metrics HTTP server** runs in its own goroutine; Prometheus gauges are
  internally goroutine-safe.
- A **root `context.Context`** (cancelled on `SIGINT`/`SIGTERM`) ties both
  goroutines' lifetimes together and is passed first into `sensor.Read` and
  `alert.Send`. On shutdown the ticker stops and the metrics server is
  gracefully drained via `Shutdown(ctx)`.

### Logging

- Structured `slog` (text or JSON handler) with consistent fields
  (`humidity`, `temperature`, `state`, `event`).
- The **version string is logged at startup** (`-ldflags -X main.version`).
- Secrets (ntfy topic) are never logged.

### Configuration

Per the guidelines, configuration comes from a **config file** (not flags) and is
**immutable after init**. The only command-line flags are operational:
`-config <path>` (path to the file, default `/etc/bme280mon/config.yaml`) and
`-version` (print version and exit). Format is YAML.

At startup the file is loaded once into an immutable `Config` value that is
**validated** before the loop starts (guidelines: "MUST validate inputs") — see
Validation below. Values with sane defaults:

| Key                  | Default            | Meaning                             |
|----------------------|--------------------|-------------------------------------|
| `poll_interval`      | `30s`              | time between reads                  |
| `humidity_threshold` | `60.0`             | percent that counts as "high"       |
| `humidity_buffer`    | `3.0`              | hysteresis gap (recover below 57%)  |
| `i2c_address`        | `0x76`             | sensor address                      |
| `ntfy_server`        | `https://ntfy.sh`  | ntfy base URL                       |
| `ntfy_topic`         | (required)         | ntfy topic to publish to            |
| `metrics_addr`       | `:9101`            | Prometheus listen address           |
| `sensor_fail_limit`  | `5`                | consecutive failures before alert   |
| `http_timeout`       | `10s`              | ntfy client request timeout         |

**Validation** (fail fast at startup with a wrapped error): `ntfy_topic`
non-empty; `ntfy_server` a valid `https`/`http` URL (warn if not `https`);
`0 < humidity_threshold <= 100`; `0 <= humidity_buffer < humidity_threshold`;
`poll_interval > 0`; `sensor_fail_limit >= 1`; `i2c_address` in the BME280 range
(`0x76`/`0x77`); `metrics_addr` a parseable listen address.

## Build & Deploy

### Deploy to the Pi

Run `make sfx` on macOS, copy the `.install` to the Pi, execute it. The installer
places the binary, installs `bme280mon.service`, drops an example config to
`/etc/bme280mon/config.yaml` to edit (fill in `ntfy_topic`), then enables + starts
the service. Service auto-starts on boot and restarts on failure.

**Least privilege (systemd hardening).** The service runs as a dedicated
non-root user in the `i2c` group (for `/dev/i2c-1` access) with hardening
directives: `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`,
`PrivateTmp=yes`, and `DeviceAllow=/dev/i2c-1 rw` (guidelines: least privilege,
limit filesystem/network access). The config file is read-only to the service user
and holds the ntfy topic, so it is mode `0640` owned by that user.

## Testing

Table-driven, deterministic, hermetic; `t.Parallel()` where safe; `t.Cleanup`
for teardown. Unit tests are tagged `//go:build unit` in `xxx_test.go`.

- **detector** — pure unit tests: rising crossing, falling recovery, flapping
  around the buffer, staying high without re-alerting.
- **alert** — against an `httptest` server asserting the POST URL/payload, and
  asserting the topic is redacted from error output.
- **sensor** — behind the `Reader` interface with a fake, so the poll loop is
  testable without hardware; the fake also drives the consecutive-failure /
  recovery path.
- **metrics** — via the Prometheus registry, asserting gauge values after `Update`.
- **config** — validation table: each invalid field yields a wrapped error.

An **integration test** tagged `//go:build integration`
(`sensor_integration_test.go`) exercises the real periph.io driver against
hardware at `0x76`; it is excluded from the default `make test` run and only
executed on a Pi with the sensor attached.

`make test` runs `-race -tags unit`; `make vuln` runs `govulncheck`.

## Out of scope (YAGNI)

- Temperature/pressure alerting.
- Local history storage beyond Prometheus metrics (no CSV/DB).
- SPI mode.
- A UI/dashboard (Grafana lives downstream of the metrics endpoint).
