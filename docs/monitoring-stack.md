# Monitoring stack: VictoriaMetrics + Grafana

`bme280mon` exposes readings as Prometheus metrics but does not store them. This stack adds a time-series database that keeps 30 days of history and a Grafana instance to look at it. Everything runs on the same Raspberry Pi.

Design rationale lives in the implementation [spec](docs/specs/2026-08-08-monitoring-stack-design.md).

## What gets installed

| Service | Listens | Source |
|---|---|---|
| `victoria-metrics` | `127.0.0.1:8428` | bundled (Apache-2.0) |
| `node_exporter` | `127.0.0.1:9100` | bundled (Apache-2.0) |
| `grafana-server` | `0.0.0.0:3000` (HTTPS) | Grafana's apt repo (AGPLv3) |

VictoriaMetrics scrapes three targets every 30s: `bme280mon`, `node_exporter`, and its own `/metrics`. It stores them under `/var/lib/victoria-metrics`. Grafana is the only service reachable from the LAN.

This is deliberately a **single-sensor** stack: `scrape.yaml` only scrapes `127.0.0.1:9101`. If you run a second `bme280mon` instance (see "Running two sensors on one Pi" in the main README), it is not collected until you add it as a scrape target yourself. See "Operating notes" below.

Grafana is installed from its upstream apt repository rather than bundled into the installer, because Grafana is AGPLv3 and this project does not ship copyleft code in its artifacts. The practical consequence: **the Pi needs working network access when you run the stack installer.**

### Bundled licenses

The two bundled binaries are Apache-2.0, which conditions redistribution on shipping the License and any NOTICE with them. The installer places both under `/usr/local/share/doc/`:

| Project | Installed to | Source of the text |
|---|---|---|
| `node_exporter` | `/usr/local/share/doc/node_exporter/{LICENSE,NOTICE}` | extracted from its release tarball |
| `victoria-metrics` | `/usr/local/share/doc/victoria-metrics/LICENSE` | vendored in `install/stack/licenses/` |

The VictoriaMetrics `linux-arm64` release tarball contains only the `victoria-metrics-prod` binary, with no LICENSE and no NOTICE. Its license text is therefore vendored into this repo at the pinned tag instead. It must be refreshed whenever `VM_VERSION` changes in the `Makefile`. See `install/stack/licenses/README.md`.

## Installing

On your build machine:

```bash
make stack-sfx
scp bme280mon-stack-v0.1.0-linux-arm64.install <host>:
```

On the Pi:

```bash
sudo bash bme280mon-stack-v0.1.0-linux-arm64.install
```

The installer prompts once for a Grafana admin password. It is never echoed and is stored hashed in Grafana's database. There is no plaintext copy on disk.

Then verify. The installer puts the verification script on the Pi as `/usr/local/bin/stack-doctor`. Wait about 60 seconds after the install finishes so the first scrape has landed, then run:

```bash
sudo stack-doctor
```

It needs root, because it reads `/etc/grafana/grafana.ini` and dry-runs the scrape config, both of which are root-only. It refuses to run without root rather than reporting permission errors as failed checks. The data checks retry for up to 60 seconds on their own, so running it a little early costs time rather than a false failure.

Every check should pass. If the `bme280mon (:9101)` check fails, see "Upgrading an existing install" below.

`stack-doctor` also exercises Grafana's TLS setup: it confirms TLS 1.2 works, attempts a TLS 1.1 handshake to confirm the server refuses it, and checks that `/etc/grafana/grafana.ini` sets `min_tls_version = TLS1.2`. The TLS 1.1 check is reported as a failure labelled `(inconclusive)` when the Pi's own `openssl` build cannot offer TLS 1.1 to test with in the first place. That outcome does not mean TLS 1.1 was accepted, only that this particular check couldn't be run, and it is counted as a failure rather than a silent pass.

Re-running the installer is safe. Existing configs, the TLS certificate, and the admin password are all left alone.

## Upgrading an existing install

If you installed `bme280mon` before this stack existed, its config still has `metrics_addr: ":9101"`, which exposes the sensor endpoint to your whole network. The installer deliberately never overwrites your config, so fix it by hand:

```bash
sudo sed -i 's/^metrics_addr:.*/metrics_addr: "127.0.0.1:9101"/' /etc/bme280mon/config.yaml
sudo systemctl restart bme280mon
```

## Trusting the certificate

Grafana serves HTTPS with a self-signed certificate generated on the Pi at install time. Every browser will warn once. To silence it, copy the certificate to your machine and add it to your trust store:

```bash
ssh <host> sudo cat /etc/grafana/tls/grafana.crt > ~/Downloads/grafana-pi.crt
```

- **macOS:** open Keychain Access -> System, drag the `.crt` in, open it, then set Trust -> "When using this certificate: Always Trust".
- **Linux:** `sudo cp grafana-pi.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates`

The certificate is valid for 825 days and carries subject-alternative names for the Pi's hostname, `<hostname>.local`, and its LAN IP address. Use one of those in the URL. An address not in the SAN list will still warn.

Grafana enforces a TLS 1.2 floor but offers no setting for choosing cipher suites, so the connection uses Go's default suite list. That list prefers forward secrecy, but is not restricted to the FIPS-approved set that `docs/project-guidelines.md` asks for. That is a known and accepted deviation, recorded in the design document.

## Your first dashboard

Open `https://<hostname>.local:3000` and log in as `admin`.

The `VictoriaMetrics` datasource is already configured and is the default. It is marked read-only in the UI on purpose, because it comes from a provisioning file, so a rebuilt Pi always gets the same datasource. Dashboards are yours to build.

Create a dashboard (**Dashboards -> New -> New dashboard -> Add visualization**, then pick `VictoriaMetrics`). For each panel, paste the query into the code-mode query box.

**Panel 1: Humidity, with the alert threshold visible**

```promql
bme280_humidity_percent
```

Set *Visualization* to **Time series**. Under *Standard options* set **Unit** to `Misc -> Percent (0-100)`, **Min** `0`, **Max** `100`. Then add a threshold at `60` (*Thresholds -> Add threshold*) so the panel shows the same line `bme280mon` alerts on, and set *Thresholds -> Show thresholds* to `As a filled region`. Title it "Humidity".

**Panel 2: Temperature and pressure together**

Two queries on one panel. Pressure needs its own axis because the values are three orders of magnitude apart.

```promql
bme280_temperature_celsius
```

```promql
bme280_pressure_hpa
```

Set the panel unit to `Temperature -> Celsius`, then add an override for the pressure series: *Overrides -> Add field override -> Fields with name -> `bme280_pressure_hpa`*, and add the properties **Unit** = `Pressure -> Hectopascal` and **Axis -> Placement** = `Right`.

**Panel 3: Pi SoC temperature**

```promql
node_thermal_zone_temp
```

Unit `Temperature -> Celsius`. A Pi 4 under light load sits around 45-55 degrees C. Sustained values above 80 degrees C mean it is throttling.

**Panel 4: Pi CPU usage**

```promql
100 - (avg without(cpu) (rate(node_cpu_seconds_total{mode="idle"}[$__rate_interval])) * 100)
```

Unit `Misc -> Percent (0-100)`, Min `0`, Max `100`. `$__rate_interval` is a Grafana variable that picks a window suited to the current zoom level. It works correctly here because the datasource declares `timeInterval: 30s`.

**Panel 5: Disk space, as a gauge**

```promql
100 * (1 - node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"})
```

Set *Visualization* to **Gauge**, unit `Misc -> Percent (0-100)`, Min `0`, Max `100`. VictoriaMetrics stops ingesting when free space drops below 512MB, so this is the panel that warns you first.

**Panel 6: Is anything failing?**

```promql
increase(bme280_read_errors_total[1h])
```

```promql
increase(bme280_notify_errors_total[1h])
```

Visualization **Stat**. Both should read `0`.

Save the dashboard (**Save**, then name it, for example "Room environment").

### Exploring without Grafana

VictoriaMetrics has its own query UI at `/vmui`, bound to loopback. Reach it through an SSH tunnel:

```bash
ssh -N -L 8428:127.0.0.1:8428 <host>
```

Then open <http://127.0.0.1:8428/vmui>. Use it to try a PromQL expression before committing it to a panel.

## Backing up your dashboards

Dashboards built in the UI live only in `/var/lib/grafana/grafana.db`. When one becomes worth keeping, export it (**Dashboard settings -> JSON Model**, or **Share -> Export**) and commit the JSON to this repo. This is deliberately a manual practice. Automating it would mean provisioning dashboards from files, which makes them read-only in the UI and would defeat the point of learning to build them.

## Operating notes

- **Adding a scrape target:** edit `/etc/victoria-metrics/scrape.yaml`, then run `sudo systemctl reload victoria-metrics`. Reload sends `SIGHUP`, so the database does not restart and no data is lost. This is also how a second sensor joins the stack. Enabling `bme280mon@<name>` on its own port (see the README's "Running two sensors on one Pi") does not, by itself, get scraped. Add its `127.0.0.1:<port>` to `scrape.yaml` and reload. Once both sensors are scraped, the `location` label (for example `location="attic"`) is what tells their series apart in queries and dashboards.
- **Retention** is set by `-retentionPeriod=30d` in `/etc/victoria-metrics/victoria-metrics.env`. Raising it takes a restart. 30 days of the default target set is roughly 40-60MB.
- **Logs:** `journalctl -u victoria-metrics -f` (likewise `node_exporter`, `grafana-server`).
- **Known blind spot:** `node_exporter` runs with `ProtectHome=yes`, so its `filesystem` collector does not report `/home` mounts. On a single-purpose Pi this is intentional.
- **If VictoriaMetrics is down**, samples are not buffered anywhere, because the scraper lives inside VM itself. An outage leaves a gap in history rather than backfilling. This was a deliberate trade for one fewer daemon.
