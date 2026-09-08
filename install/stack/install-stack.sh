#!/bin/bash
#
# (c) Copyright 2026 Eric Paul Forgette
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# bme280mon monitoring stack installer. Run as root on the target Raspberry Pi:
#   sudo bash bme280mon-stack-<version>-linux-arm64.install
#
# Installs VictoriaMetrics and node_exporter (bundled, Apache-2.0) and Grafana
# (from Grafana's upstream apt repo -- deliberately NOT bundled, because Grafana
# is AGPLv3 and must not enter this artifact).

set -euo pipefail

INSTALL_BIN=/usr/local/bin
CONFIG_DIR=/etc/victoria-metrics
DATA_DIR=/var/lib/victoria-metrics
DOC_DIR=/usr/local/share/doc
SYSTEMD_DIR=/etc/systemd/system
VM_USER=victoriametrics
NE_USER=node_exporter

echo "bme280mon monitoring stack installer"

if [[ $EUID -ne 0 ]]; then
    echo "Error: this installer must be run as root." >&2
    exit 1
fi

# Every tool below is used unconditionally further down. Discovering a missing
# one there would leave users and binaries already installed, so check now --
# before anything mutates state. (sudo is needed to drop privileges for the
# grafana cli password reset.)
echo "Checking for required tools..."
MISSING=()
for cmd in curl openssl ip useradd sudo; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        MISSING+=("${cmd}")
    fi
done
if [[ ${#MISSING[@]} -gt 0 ]]; then
    echo "Error: required command(s) not found: ${MISSING[*]}" >&2
    echo "       Install them and re-run. Nothing has been installed." >&2
    exit 1
fi

# Grafana comes from apt, so the Pi needs working network before we change any
# state. Fail here rather than half-installing.
echo "Checking network reachability (needed for the Grafana apt repo)..."
if ! curl -fsS --max-time 15 -o /dev/null https://apt.grafana.com/gpg.key; then
    echo "Error: cannot reach https://apt.grafana.com -- check network/DNS." >&2
    echo "       Nothing has been installed." >&2
    exit 1
fi

PAYLOAD_LINE=$(awk '/^#__PAYLOAD__$/{print NR+1; exit}' "$0")
if [[ -z "${PAYLOAD_LINE}" ]]; then
    echo "Error: payload marker not found. Archive may be corrupt." >&2
    exit 1
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "${TMPDIR}"' EXIT

echo "Extracting payload..."
tail -n +"${PAYLOAD_LINE}" "$0" | tar xz -C "${TMPDIR}"

echo "Creating service users..."
for u in "${VM_USER}" "${NE_USER}"; do
    if ! id -u "${u}" >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "${u}"
        echo "  created ${u}"
    else
        echo "  ${u} already exists"
    fi
done

echo "Installing binaries -> ${INSTALL_BIN}"
install -m 755 "${TMPDIR}/victoria-metrics" "${INSTALL_BIN}/victoria-metrics"
install -m 755 "${TMPDIR}/node_exporter" "${INSTALL_BIN}/node_exporter"

echo "Creating directories..."
install -d -m 750 -o root -g "${VM_USER}" "${CONFIG_DIR}"
install -d -m 750 -o "${VM_USER}" -g "${VM_USER}" "${DATA_DIR}"

# Mirrors install.sh's rule: an existing config is never overwritten, so
# re-running the installer cannot discard local edits.
install_config_once() {
    local src=$1 dest=$2 owner=$3
    if [[ -f "${dest}" ]]; then
        echo "  keeping existing ${dest}"
        return 0
    fi
    install -m 640 -o root -g "${owner}" "${src}" "${dest}"
    echo "  wrote ${dest}"
}

echo "Installing configuration -> ${CONFIG_DIR}"
install_config_once "${TMPDIR}/examples/victoria-metrics.env" "${CONFIG_DIR}/victoria-metrics.env" "${VM_USER}"
install_config_once "${TMPDIR}/examples/node_exporter.env"    "${CONFIG_DIR}/node_exporter.env"    "${NE_USER}"
install_config_once "${TMPDIR}/examples/scrape.yaml"          "${CONFIG_DIR}/scrape.yaml"          "${VM_USER}"

echo "Installing systemd units -> ${SYSTEMD_DIR}"
install -m 644 "${TMPDIR}/victoria-metrics.service" "${SYSTEMD_DIR}/victoria-metrics.service"
install -m 644 "${TMPDIR}/node_exporter.service"    "${SYSTEMD_DIR}/node_exporter.service"
systemctl daemon-reload

# The repo is not on the Pi, so the verification script has to travel in the
# payload and land on PATH.
echo "Installing verification script -> ${INSTALL_BIN}/stack-doctor"
install -m 755 "${TMPDIR}/stack-doctor" "${INSTALL_BIN}/stack-doctor"

# Apache-2.0 sections 4(a) and 4(d): redistributing these binaries obliges us to
# ship the License and any NOTICE alongside them. node_exporter's release tarball
# carries both; VictoriaMetrics' carries neither, so its LICENSE is vendored in
# the repo at the pinned tag and packed by stack-sfx. Same obligation, two
# sources -- kept together so they read as one concern.
echo "Installing third-party license texts -> ${DOC_DIR}"
install -d -m 755 "${DOC_DIR}/node_exporter"
install -m 644 "${TMPDIR}/node_exporter.LICENSE" "${DOC_DIR}/node_exporter/LICENSE"
install -m 644 "${TMPDIR}/node_exporter.NOTICE"  "${DOC_DIR}/node_exporter/NOTICE"
install -d -m 755 "${DOC_DIR}/victoria-metrics"
install -m 644 "${TMPDIR}/victoria-metrics.LICENSE" "${DOC_DIR}/victoria-metrics/LICENSE"

# Validate the scrape config before starting anything. A typo here would
# otherwise show up as a service that will not start.
echo "Validating scrape config..."
if ! "${INSTALL_BIN}/victoria-metrics" \
        -promscrape.config="${CONFIG_DIR}/scrape.yaml" \
        -promscrape.config.dryRun; then
    echo "Error: ${CONFIG_DIR}/scrape.yaml failed validation." >&2
    exit 1
fi

echo "Starting VictoriaMetrics and node_exporter..."
systemctl enable --now victoria-metrics.service node_exporter.service
# `enable --now` is a no-op on an already-active unit, so on a re-run the
# services would keep executing the previous binary image and ignore unit
# changes. try-restart picks up the new ones without starting anything that was
# deliberately stopped.
systemctl try-restart victoria-metrics.service node_exporter.service

# --- Grafana -------------------------------------------------------------
# Installed from Grafana's apt repo, NOT bundled: Grafana is AGPLv3 and must
# not enter this artifact (see docs/project-guidelines.md, Modules).
GRAFANA_DIR=/etc/grafana
TLS_DIR="${GRAFANA_DIR}/tls"
ADMIN_MARKER="${GRAFANA_DIR}/.admin-password-set"

echo "Installing Grafana from its upstream apt repository..."
install -d -m 755 /etc/apt/keyrings
# apt reads ASCII-armored keys directly, so the key is stored as fetched. That
# drops the gnupg dependency and, more importantly, makes this a download to a
# temp file followed by an atomic rename: a mid-transfer network drop can no
# longer leave a truncated keyring that the existence guard then skips forever.
if [[ ! -f /etc/apt/keyrings/grafana.asc ]]; then
    curl -fsSL --max-time 30 -o /etc/apt/keyrings/grafana.asc.tmp https://apt.grafana.com/gpg.key
    chmod 644 /etc/apt/keyrings/grafana.asc.tmp
    mv -f /etc/apt/keyrings/grafana.asc.tmp /etc/apt/keyrings/grafana.asc
fi
# Temp-then-rename here too: a half-written sources list is an apt failure.
echo "deb [signed-by=/etc/apt/keyrings/grafana.asc] https://apt.grafana.com stable main" \
    > /etc/apt/sources.list.d/grafana.list.tmp
chmod 644 /etc/apt/sources.list.d/grafana.list.tmp
mv -f /etc/apt/sources.list.d/grafana.list.tmp /etc/apt/sources.list.d/grafana.list

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq grafana

echo "Configuring TLS..."
install -d -m 750 -o root -g grafana "${TLS_DIR}"
if [[ -f "${TLS_DIR}/grafana.crt" && -f "${TLS_DIR}/grafana.key" ]]; then
    echo "  keeping existing certificate at ${TLS_DIR}/grafana.crt"
else
    HOST=$(hostname -s)
    # The address the Pi uses to reach the outside world is the one clients
    # will connect to. Falls back to the first non-loopback address.
    LAN_IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}')
    if [[ -z "${LAN_IP}" ]]; then
        LAN_IP=$(hostname -I | awk '{print $1}')
    fi
    echo "  generating self-signed certificate for ${HOST}, ${HOST}.local, ${LAN_IP}"
    # SANs are mandatory: browsers ignore CN entirely, so a CN-only cert fails
    # on every modern client.
    openssl req -x509 -newkey rsa:4096 -sha256 -days 825 -nodes \
        -keyout "${TLS_DIR}/grafana.key" \
        -out "${TLS_DIR}/grafana.crt" \
        -subj "/CN=${HOST}" \
        -addext "subjectAltName=DNS:${HOST},DNS:${HOST}.local,IP:${LAN_IP}" \
        -addext "keyUsage=digitalSignature,keyEncipherment" \
        -addext "extendedKeyUsage=serverAuth" 2>/dev/null
    chown root:grafana "${TLS_DIR}/grafana.key" "${TLS_DIR}/grafana.crt"
    chmod 640 "${TLS_DIR}/grafana.key"
    chmod 644 "${TLS_DIR}/grafana.crt"
fi

echo "Configuring Grafana..."
# Start bound to loopback. Grafana's built-in admin/admin default must never be
# reachable from the LAN, so the listener is widened only after the password is
# set, at the end of this script.
#
# Written to a temp file in the same directory and renamed into place. A plain
# redirect truncates first, and Grafana's conf/defaults.ini falls back to
# "protocol = http" with an empty http_addr -- so an interrupted write would
# leave the next restart serving plaintext HTTP on every interface.
#
# There is no cipher_suites setting here: [server] has no such key and Grafana
# silently ignores unknown ini keys, so listing one would only look like
# enforcement. See the TLS notes in docs/monitoring-stack.md.
cat > "${GRAFANA_DIR}/grafana.ini.tmp" <<EOF
# Managed by the bme280mon stack installer.
[server]
protocol = https
http_addr = 127.0.0.1
http_port = 3000
cert_file = ${TLS_DIR}/grafana.crt
cert_key = ${TLS_DIR}/grafana.key
min_tls_version = TLS1.2

[users]
allow_sign_up = false

[auth.anonymous]
enabled = false

[analytics]
reporting_enabled = false
check_for_updates = false
EOF
chown root:grafana "${GRAFANA_DIR}/grafana.ini.tmp"
chmod 640 "${GRAFANA_DIR}/grafana.ini.tmp"
mv -f "${GRAFANA_DIR}/grafana.ini.tmp" "${GRAFANA_DIR}/grafana.ini"

echo "Provisioning the VictoriaMetrics datasource..."
install -d -m 755 "${GRAFANA_DIR}/provisioning/datasources"
install -m 644 \
    "${TMPDIR}/grafana/provisioning/datasources/victoriametrics.yaml" \
    "${GRAFANA_DIR}/provisioning/datasources/victoriametrics.yaml"

systemctl daemon-reload
systemctl enable --now grafana-server.service

echo "Waiting for Grafana to become healthy..."
for _ in $(seq 1 30); do
    if curl -fsSk --max-time 3 -o /dev/null https://127.0.0.1:3000/api/health; then
        break
    fi
    sleep 2
done
if ! curl -fsSk --max-time 3 -o /dev/null https://127.0.0.1:3000/api/health; then
    echo "Error: Grafana did not come up. Check: journalctl -u grafana-server" >&2
    exit 1
fi

# Only prompt on a genuinely fresh install -- re-running must not reset a
# password the operator already chose.
if [[ ! -f "${ADMIN_MARKER}" ]]; then
    echo ""
    echo "Set the Grafana admin password."
    while :; do
        # -s: never echoed, never in argv, never in shell history.
        read -r -s -p "  Password (min 8 chars): " GF_PW; echo ""
        read -r -s -p "  Confirm: " GF_PW2; echo ""
        if [[ "${GF_PW}" != "${GF_PW2}" ]]; then
            echo "  Passwords do not match; try again." >&2
            continue
        fi
        if [[ ${#GF_PW} -lt 8 ]]; then
            echo "  Too short; try again." >&2
            continue
        fi
        break
    done
    # grafana cli resolves its sqlite path from --homepath and paths.data, and
    # gets none of the overrides the deb's unit puts on grafana-server's command
    # line. Left bare it would happily reset the password in a fresh, empty
    # database that nobody serves. Pin all three, and run as the grafana user so
    # the -wal/-journal files it leaves behind stay writable by the server.
    printf '%s' "${GF_PW}" | sudo -u grafana grafana cli \
        --homepath /usr/share/grafana \
        --config "${GRAFANA_DIR}/grafana.ini" \
        --configOverrides cfg:default.paths.data=/var/lib/grafana \
        admin reset-admin-password --password-from-stdin >/dev/null
    # Prove it took effect against the running server. The CLI's exit status only
    # says it wrote to *some* database.
    #
    # The credential reaches curl through a config file on stdin, never via -u:
    # argv is world-readable through /proc/<pid>/cmdline, so any local account --
    # including a compromised service account -- could scrape the password out of
    # a `ps` loop while the installer runs.
    #
    # curl's config parser ends a quoted value at the first unescaped " and
    # honours backslash escapes inside quotes, so both characters are escaped
    # first (backslash before quote, or the escaping eats itself). Without this a
    # password containing either would fail the probe and block the install even
    # though the reset had actually succeeded.
    GF_PW_ESC=${GF_PW//\\/\\\\}
    GF_PW_ESC=${GF_PW_ESC//\"/\\\"}
    if ! printf 'user = "admin:%s"\n' "${GF_PW_ESC}" \
            | curl -K - -fsSk --max-time 5 -o /dev/null https://127.0.0.1:3000/api/org; then
        echo "Error: the admin password was not applied to the running Grafana." >&2
        exit 1
    fi
    unset GF_PW GF_PW2 GF_PW_ESC
    install -m 600 -o root -g root /dev/null "${ADMIN_MARKER}"
    echo "  admin password set (stored hashed in Grafana's database)"
else
    echo "  admin password already set by a previous run; leaving it alone"
fi

# Refuse to widen if the password was never actually set -- a prior run that
# died after Grafana created its database would otherwise put the factory
# admin/admin credential on the LAN.
if [[ ! -f "${ADMIN_MARKER}" ]]; then
    echo "Error: the Grafana admin password was never set by this installer." >&2
    echo "       Refusing to expose Grafana to the LAN with a default credential." >&2
    echo "       Re-run this installer to set it." >&2
    exit 1
fi

# The marker lives in /etc/grafana but the credential lives in /var/lib/grafana,
# so the two can drift apart: `apt remove` (not purge) keeps the marker, and an
# SD-card restore or a manual rm of /var/lib/grafana hands Grafana a virgin
# database with admin/admin. Test the property itself rather than the proxy for
# it, immediately before widening.
if curl -fsSk --max-time 5 -o /dev/null -u admin:admin https://127.0.0.1:3000/api/org; then
    echo "Error: Grafana still accepts admin/admin." >&2
    rm -f "${ADMIN_MARKER}"   # force the next run to prompt
    # "Refusing to expose it" would be a lie if a previous run had already
    # widened the listener: `enable --now` is a no-op on an active unit, so that
    # server is serving this virgin database on 0.0.0.0 right now. The on-disk
    # config says 127.0.0.1, so restarting makes the claim true rather than
    # merely asserted. || true: under set -e a failed restart must not preempt
    # the explicit exit below, and a Grafana that will not start is not reachable
    # either.
    systemctl restart grafana-server.service || true
    echo "       Grafana has been returned to loopback. Re-run to set the password." >&2
    exit 1
fi

# The admin password is set; it is now safe to accept LAN connections.
echo "Opening Grafana to the LAN..."
sed -i 's/^http_addr = 127\.0\.0\.1$/http_addr = 0.0.0.0/' "${GRAFANA_DIR}/grafana.ini"
systemctl restart grafana-server.service

# Everything above interrogated the process running *before* this restart. If the
# server that just came up opened a different database, it can be virgin -- and
# it is already LAN-facing. Re-evaluate the property after the restart it gates,
# and be blunt about it: at this point the exposure is real, not hypothetical.
echo "Re-checking the admin credential after the restart..."
for _ in $(seq 1 30); do
    if curl -fsSk --max-time 3 -o /dev/null https://127.0.0.1:3000/api/health; then
        break
    fi
    sleep 2
done
if curl -fsSk --max-time 10 -o /dev/null -u admin:admin https://127.0.0.1:3000/api/org; then
    echo "Error: Grafana accepts admin/admin after the restart, and is now LAN-facing." >&2
    echo "       Stop it immediately: sudo systemctl stop grafana-server" >&2
    exit 1
fi

HOST_SHORT=$(hostname -s)
echo ""
echo "Installation complete."
echo "  Grafana:          https://${HOST_SHORT}.local:3000  (log in as 'admin')"
echo "  VictoriaMetrics:  http://127.0.0.1:8428  (loopback only; use an SSH tunnel for vmui)"
echo ""
echo "Your browser will warn about the self-signed certificate -- see"
echo "docs/monitoring-stack.md for how to trust it."
echo ""
echo "Wait about 60 seconds for the first scrape to land, then verify with:"
echo "  sudo stack-doctor"

exit 0

#__PAYLOAD__
