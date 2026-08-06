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
# bme280mon self-extracting installer. Run as root on the target Raspberry Pi 4:
#   sudo bash bme280mon-<version>-linux-arm64.install

set -euo pipefail

INSTALL_BIN=/usr/local/bin
CONFIG_DIR=/etc/bme280mon
SERVICE=/etc/systemd/system/bme280mon.service
RUN_USER=bme280mon

echo "bme280mon installer"

if [[ $EUID -ne 0 ]]; then
    echo "Error: this installer must be run as root." >&2
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

echo "Creating service user ${RUN_USER} (in group i2c)..."
if ! id -u "${RUN_USER}" >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin --groups i2c "${RUN_USER}"
else
    usermod -aG i2c "${RUN_USER}"
fi

echo "Installing binary -> ${INSTALL_BIN}/bme280mon"
install -m 755 "${TMPDIR}/bme280mon" "${INSTALL_BIN}/bme280mon"

echo "Installing config dir -> ${CONFIG_DIR}"
install -d -m 750 -o "${RUN_USER}" -g "${RUN_USER}" "${CONFIG_DIR}"
if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
    install -m 640 -o "${RUN_USER}" -g "${RUN_USER}" "${TMPDIR}/examples/config.yaml" "${CONFIG_DIR}/config.yaml"
    echo "  wrote ${CONFIG_DIR}/config.yaml (edit it and set ntfy_topic)"
else
    echo "  keeping existing ${CONFIG_DIR}/config.yaml"
fi

echo "Installing service -> ${SERVICE}"
install -m 644 "${TMPDIR}/bme280mon.service" "${SERVICE}"

systemctl daemon-reload
systemctl enable bme280mon.service
echo ""
echo "Installation complete."
echo "Edit ${CONFIG_DIR}/config.yaml (set ntfy_topic), then: sudo systemctl start bme280mon"

exit 0
#__PAYLOAD__
