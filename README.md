# bme280mon

A small Go daemon that monitors room humidity (and records temperature and
pressure) using a GY-BME280 sensor on a Raspberry Pi 4. When humidity crosses
a configurable threshold it pushes a phone alert via [ntfy](https://ntfy.sh) —
once per event, with no spam thanks to a hysteresis state machine. Readings
are also exposed as Prometheus metrics for scraping into VictoriaMetrics (or
any Prometheus-compatible collector).

## Hardware

**Sensor:** GY-BME280 breakout, connected over I2C on bus 1 at address `0x76`.

Power the sensor board from the Pi's **3.3V** pin, not 5V. The Pi's GPIO is
3.3V logic; running the breakout at 3.3V keeps its onboard I2C pull-ups from
pulling SDA/SCL above 3.3V. Confirmed working at `0x76` via `i2cdetect -y 1`.

### Wiring (I2C, 4 wires)

| BME280 pin | Pi 4 physical pin  | Signal    |
|------------|--------------------|-----------|
| VIN / VCC  | Pin 1 (3.3V)       | power     |
| GND        | Pin 6 (GND)        | ground    |
| SCL        | Pin 5 (GPIO3/SCL1) | I2C clock |
| SDA        | Pin 3 (GPIO2/SDA1) | I2C data  |

### One-time Pi bring-up

1. Enable I2C: `sudo raspi-config` → **Interface Options** → **I2C** (or set
   `dtparam=i2c_arm=on` in `/boot/firmware/config.txt`), then reboot.
2. Install I2C tools: `sudo apt install i2c-tools`
3. Confirm the sensor is visible: `i2cdetect -y 1` — you should see a device
   at `0x76` (or `0x77`, depending on the breakout's address pin).

## Building and installing

Build and package are done on your development machine (macOS or Linux) and
cross-compiled for the Pi (`linux/arm64`); the installer is then copied over
and run on the Pi itself.

1. Build the self-extracting installer:

   ```bash
   make sfx
   ```

   This produces `bme280mon-v0.1.0-linux-arm64.install` — a single executable
   shell script with the `bme280mon` binary, the systemd unit, and the
   example config bundled inside it.

2. Copy it to the Pi and run it as root:

   ```bash
   scp bme280mon-v0.1.0-linux-arm64.install pi@<host>:~
   ssh pi@<host>
   sudo bash bme280mon-v0.1.0-linux-arm64.install
   ```

   The installer:
   - creates a dedicated, unprivileged `bme280mon` system user (in the `i2c`
     group, for access to `/dev/i2c-1`)
   - installs the binary to `/usr/local/bin/bme280mon`
   - installs the example config to `/etc/bme280mon/config.yaml` (only if a
     config doesn't already exist there — re-running the installer won't
     clobber your settings)
   - installs and enables the `bme280mon.service` systemd unit

3. **Edit the config** before starting the service:

   ```bash
   sudo vi /etc/bme280mon/config.yaml
   ```

   At minimum, set `ntfy_topic` to a unique, hard-to-guess topic name (see
   below — this is effectively a secret). Adjust `humidity_threshold`,
   `humidity_buffer`, `poll_interval`, etc. if the defaults don't suit your
   room.

4. Start the service:

   ```bash
   sudo systemctl start bme280mon
   sudo systemctl status bme280mon
   journalctl -u bme280mon -f
   ```

   The service is enabled at install time, so it also starts automatically on
   boot and restarts on failure.

## Alerts via ntfy

[ntfy.sh](https://ntfy.sh) is a free push-notification service: you publish
to a "topic" (just a URL path) and anyone subscribed to that topic name gets
a push notification. There is no account or registration — the topic name
itself is the access control, so **treat it like a secret**: pick something
long and hard to guess (e.g. a random string), not something like
`my-humidity-alerts`.

1. Choose a topic name and set it as `ntfy_topic` in
   `/etc/bme280mon/config.yaml`.
2. Install the **ntfy** app on your phone (iOS App Store / Google Play), or
   use the web app at <https://ntfy.sh/app>.
3. In the app, subscribe to the same topic name you put in the config.
4. When humidity crosses the threshold you'll get a push notification (e.g.
   "Humidity high: 63%"), and a follow-up low-priority notification once it
   recovers back below the threshold minus the hysteresis buffer. Sensor
   failures are reported the same way if reads fail repeatedly.

## Metrics

`bme280mon` serves Prometheus-format metrics over HTTP, by default on
`:9101`:

```text
curl http://<pi-host>:9101/metrics
```

Exposed metrics:

| Metric                       | Meaning                          |
|------------------------------|----------------------------------|
| `bme280_humidity_percent`    | current relative humidity, %     |
| `bme280_temperature_celsius` | current temperature, degrees C   |
| `bme280_pressure_hpa`        | current barometric pressure, hPa |
| `bme280_read_errors_total`   | count of failed sensor reads     |
| `bme280_notify_errors_total` | count of failed ntfy sends       |

Point a Prometheus or VictoriaMetrics scrape config at
`http://<pi-host>:9101/metrics` to collect these over time.

## Configuration reference

`/etc/bme280mon/config.yaml`:

| Key                  | Default           | Meaning                         |
|----------------------|-------------------|---------------------------------|
| `poll_interval`      | `30s`             | time between reads              |
| `humidity_threshold` | `60.0`            | percent RH counted as "high"    |
| `humidity_buffer`    | `3.0`             | hysteresis buffer, recovery gap |
| `i2c_address`        | `0x76`            | BME280 I2C address (0x76/0x77)  |
| `ntfy_server`        | `https://ntfy.sh` | ntfy base URL                   |
| `ntfy_topic`         | *(required)*      | ntfy topic (treat as secret)    |
| `metrics_addr`       | `:9101`           | Prometheus /metrics listen addr |
| `sensor_fail_limit`  | `5`               | failures before alerting        |
| `http_timeout`       | `10s`             | ntfy request timeout            |

## Development

```bash
make check   # fmt, vet, lint, vuln
make test    # unit tests with -race
make vuln    # govulncheck
make sfx     # build the self-extracting installer
```

See `make help` for the full target list.
