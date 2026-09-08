# Vendored third-party license texts

`victoria-metrics-LICENSE` is the Apache-2.0 text from VictoriaMetrics `v1.149.0`,
vendored verbatim because the `linux-arm64` release tarball ships the bare
`victoria-metrics-prod` binary with no LICENSE or NOTICE of its own — and Apache-2.0
section 4(a) conditions redistribution on including it.

**Refresh this file whenever `VM_VERSION` changes in the `Makefile`**, from
`https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/<VM_VERSION>/LICENSE`.
Nothing else here needs vendoring: node_exporter's tarball carries its own LICENSE and
NOTICE, which `stack-fetch` extracts directly.
