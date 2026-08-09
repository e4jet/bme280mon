# bme280mon

A small Go daemon that monitors room humidity (and records temperature and pressure) using a GY-BME280 sensor on a Raspberry Pi 3/4/5. When humidity crosses a configurable threshold it pushes a phone alert via [ntfy](https://ntfy.sh) — once per event, with no spam thanks to a hysteresis state machine. Readings are also exposed as Prometheus metrics for scraping into VictoriaMetrics (or any Prometheus-compatible collector).

## Hardware

**Sensor:** GY-BME280 breakout, connected over I2C on bus 1 at address `0x76`.

Power the sensor board from the Pi's **3.3V** pin, not 5V (BME280 supports v5). The Pi's GPIO is 3.3V logic; running the breakout at 3.3V keeps its onboard I2C pull-ups from pulling SDA/SCL above 3.3V. This was confirmed working at `0x76` using `i2cdetect -y 1`.

### Wiring (I2C, 4 wires)

| BME280 pin | Pi 4 physical pin  | Signal    |
|------------|--------------------|-----------|
| VIN / VCC  | Pin 1 (3.3V)       | power     |
| GND        | Pin 6 (GND)        | ground    |
| SCL        | Pin 5 (GPIO3/SCL1) | I2C clock |
| SDA        | Pin 3 (GPIO2/SDA1) | I2C data  |

### One-time Pi bring-up

1. Enable I2C: `sudo raspi-config` → **Interface Options** → **I2C** (or set `dtparam=i2c_arm=on` in `/boot/firmware/config.txt`), then reboot.
2. Install I2C tools: `sudo apt install i2c-tools`
3. Confirm the sensor is visible: `i2cdetect -y 1` — you should see a device at `0x76` (or `0x77`, depending on the breakout's address pin).

## Building and installing

Build and package are possible on a development machine (macOS or Linux) and can be cross-compiled for the Pi (`linux/arm64`); the installer is then copied over and run on the Pi itself.

1. Build the self-extracting installer:

   ```bash
   make sfx
   ```

   This produces `bme280mon-v0.1.0-linux-arm64.install` — a single executable shell script with the `bme280mon` binary, the systemd units, and the example configs bundled inside it.

2. Copy it to the Pi and run it as root:

   ```bash
   scp bme280mon-v0.1.0-linux-arm64.install <host>:
   ssh <host>
   sudo bash bme280mon-v0.1.0-linux-arm64.install
   ```

   The installer:
   - creates a dedicated, unprivileged `bme280mon` system user (in the `i2c` group, for access to `/dev/i2c-1`)
   - installs the binary to `/usr/local/bin/bme280mon`
   - installs the example config to `/etc/bme280mon/config.yaml` (only if a config doesn't already exist there — re-running the installer won't clobber your settings)
   - installs and enables the `bme280mon.service` systemd unit
   - installs the `bme280mon@.service` template unit and `/etc/bme280mon/examples/` (for a second sensor; not enabled, and ignorable on a single-sensor host)

3. **Edit the config** before starting the service:

   ```bash
   sudo vi /etc/bme280mon/config.yaml
   ```

   At minimum, set `ntfy_topic` to a unique, hard-to-guess topic name (see below — this is effectively a secret). Adjust `humidity_threshold`, `humidity_buffer`, `poll_interval`, etc. if the defaults don't suit your room/environment.

4. Start the service:

   ```bash
   sudo systemctl start bme280mon
   sudo systemctl status bme280mon
   journalctl -u bme280mon -f
   ```

   The service is enabled at install time, so it also starts automatically on boot and restarts on failure.

## Alerts via ntfy

[ntfy.sh](https://ntfy.sh) is a free push-notification service: you publish to a "topic" (just a URL path) and anyone subscribed to that topic name gets a push notification. There is no account or registration — the topic name itself is the access control, so **treat it like a secret**: pick something long and hard to guess (e.g. a random string), not something like `my-humidity-alerts`.

1. Choose a topic name and set it as `ntfy_topic` in `/etc/bme280mon/config.yaml`.
2. Install the **ntfy** app on your phone (iOS App Store / Google Play), or use the web app at <https://ntfy.sh/app>.
3. In the app, subscribe to the same topic name you put in the config.
4. When humidity crosses the threshold you'll get a push notification (e.g.  "Humidity high: 63%"), and a follow-up low-priority notification once it recovers back below the threshold minus the hysteresis buffer. Sensor failures are reported the same way if reads fail repeatedly.

`bme280mon` also announces its own lifecycle:

| Notification              | Priority | When                                                                                                   |
|---------------------------|----------|--------------------------------------------------------------------------------------------------------|
| `▶️ bme280mon started`    | low      | at startup, right after the config is loaded; the body carries the version and hostname                |
| `⏹️ bme280mon stopped`    | low      | on `SIGINT`/`SIGTERM` (`systemctl stop`/`restart`), or on a fatal error, whose reason is in the body   |

When `location` is set, it leads every title from that instance (`attic: ▶️ bme280mon started`); when it isn't, titles are exactly as shown. Both are low priority, so they won't buzz your phone. Because a fatal exit is reported, a restart loop shows up as repeating start/stop pairs. A failure to load the config is the one exit that can't be announced — ntfy isn't configured yet at that point, so check `journalctl -u bme280mon` if the service never says it started.

## Metrics

`bme280mon` serves Prometheus-format metrics over HTTP, by default on `:9101`:

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

Point a Prometheus or VictoriaMetrics scrape config at `http://<pi-host>:9101/metrics` to collect these over time.

If `location` is set, every series carries it as a constant label (`bme280_humidity_percent{location="attic"}`). Leaving `location` empty emits the series unlabelled, exactly as above — so a single-sensor install needs no dashboard or alert-rule changes.

## Running two sensors on one Pi

A BME280 picks its I2C address from its `SDO` pin: tied to GND it answers at `0x76`, tied to 3.3V at `0x77`. Two sensors can therefore share bus 1, and `bme280mon` runs one process per sensor.

### Wiring the second sensor

Connect it to the same four pins as the first (3.3V, GND, Pin 5/SCL, Pin 3/SDA), but strap `SDO` to **3.3V** rather than GND. Then confirm both are visible:

```bash
i2cdetect -y 1     # expect devices at both 0x76 and 0x77
```

Note that the Pi has fixed 1.8kΩ pull-ups on GPIO2/GPIO3 and each breakout adds its own, so the pull-ups get stronger as you add boards. Two breakouts is comfortably within spec, but keep the runs short.

Two sensors is the ceiling on one bus.

### Configuring the second instance

The installer ships a template systemd unit, `bme280mon@.service`, which reads `/etc/bme280mon/<instance>.yaml`. It is installed but **not enabled** — a single-sensor host uses `bme280mon.service` and can ignore it. The first (existing) sensor keeps running on the plain unit so there is nothing to migrate.

```bash
sudo install -m 640 -o bme280mon -g bme280mon \
    /etc/bme280mon/examples/second-sensor.yaml /etc/bme280mon/attic.yaml
sudo vi /etc/bme280mon/attic.yaml
sudo systemctl enable --now bme280mon@attic
sudo systemctl status bme280mon@attic
```

Use `install -m 640 -o bme280mon -g bme280mon` rather than `cp`: a config copied as root lands world-readable, and the file contains your ntfy topic, which is effectively a password.

Three keys **must** differ between the two configs:

- `i2c_address` — `0x76` and `0x77`; the two sensors cannot share one address.
- `metrics_addr` — the two instances cannot bind the same port. A clash is a startup failure, announced over ntfy, not a silently metric-less daemon.
- `location` — both instances share a hostname, so this is the only thing distinguishing their alerts.

Don't name an instance `config` — `/etc/bme280mon/config.yaml` belongs to the plain `bme280mon.service`, and `bme280mon@config` would run a second process against the same sensor.

`ntfy_topic` is normally the *same* in both configs: one topic, one phone subscription, with `location` telling the alerts apart (`attic: ⚠️ Humidity high: 63%`). Separate topics work too, at the cost of a second subscription.

## Configuration reference

`/etc/bme280mon/config.yaml`:

| Key                  | Default           | Meaning                         |
|----------------------|-------------------|---------------------------------|
| `poll_interval`      | `30s`             | time between reads              |
| `humidity_threshold` | `60.0`            | percent RH counted as "high"    |
| `humidity_buffer`    | `3.0`             | hysteresis buffer, recovery gap |
| `i2c_address`        | `0x76`            | BME280 I2C address (0x76/0x77)  |
| `location`           | *(empty)*         | sensor site name; see below     |
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
