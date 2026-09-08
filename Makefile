GO_VERSION = 1.26
PACKAGE_ROOT = github.com/e4jet/bme280mon
TAG = v0.1.0
GOOS = linux
GOARCH = arm64

# Monitoring stack (see docs/monitoring-stack.md). VM_VERSION keeps its leading
# "v"; NODE_EXPORTER_VERSION does not, matching each project's asset naming.
VM_VERSION = v1.149.0
NODE_EXPORTER_VERSION = 1.12.1
STACK_TAG = v0.1.0
STACK_DIST = _stack_dist

A1 = $(shell printf "»")
A2 = $(shell printf "»»")
S0 = 😁

.PHONY: help
help:
	@echo "Main:"
	@echo "    all         - clean, debug, check, and build"
	@echo "    check       - fmt, vet, lint"
	@echo "    test        - run unit tests with -race"
	@echo "    vuln        - govulncheck"
	@echo "    sfx         - build self-extracting installer for Raspberry Pi 4"
	@echo "    stack-fetch - download + checksum-verify VictoriaMetrics and node_exporter"
	@echo "    stack-check - shellcheck the stack installer and verify script"
	@echo "    stack-sfx   - build the monitoring stack self-extracting installer"
	@echo "    stack-clean - remove stack build artifacts"
	@echo "    clean       - remove build artifacts"
	@echo "$(S0)"

.PHONY: debug
debug:
	@echo "  Go:           `go version`"
	@echo "  GOOS:         $(GOOS)"
	@echo "  GOARCH:       $(GOARCH)"
	@echo "  PACKAGE_ROOT: $(PACKAGE_ROOT)"

.PHONY: all
all: clean debug check bme280mon ; $(info $(A1) $@)

.PHONY: check
check: fmt vet lint vuln ; $(info $(A1) $@)

.PHONY: fmt
fmt: ; $(info $(A1) $@)
	go fmt ./...

.PHONY: vet
vet: ; $(info $(A1) $@)
	go vet ./...

.PHONY: lint
lint: ; $(info $(A1) $@)
	golangci-lint run ./...
	golangci-lint run --build-tags unit,integration ./...

.PHONY: vuln
vuln: ; $(info $(A1) $@)
	govulncheck ./...

.PHONY: test
test: ; $(info $(A1) $@)
	go test -race -tags unit -coverprofile=bme280mon.coverprofile $$(go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' -tags unit ./...)

bme280mon: ; $(info $(A1) $@)
	env GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "-X main.version=$(TAG)" -o bme280mon bme280mon.go

.PHONY: sfx
sfx: bme280mon ; $(info $(A1) $@)
	@mkdir -p _sfx/examples
	@cp bme280mon _sfx/
	@cp install/bme280mon.service _sfx/
	@cp install/bme280mon@.service _sfx/
	@cp install/examples/config.yaml _sfx/examples/
	@cp install/examples/second-sensor.yaml _sfx/examples/
	@tar --no-xattrs -czf _payload.tgz -C _sfx .
	@cat install.sh _payload.tgz > bme280mon-$(TAG)-linux-arm64.install
	@chmod +x bme280mon-$(TAG)-linux-arm64.install
	@rm -rf _sfx _payload.tgz
	@shasum -a 512 bme280mon-$(TAG)-linux-arm64.install
	@echo "$(S0)"

# Apache-2.0 4(a)/(d): the License and any NOTICE must travel with the binary we
# redistribute, so both are lifted out of the tarball here and packed by
# stack-sfx. The node_exporter tarball carries LICENSE and NOTICE. The
# VictoriaMetrics tarball is a bare binary with no license text of any kind, so
# its LICENSE is vendored at the pinned tag in install/stack/licenses/ instead
# -- refresh that file when VM_VERSION changes.
.PHONY: stack-fetch
stack-fetch: ; $(info $(A1) $@)
	@mkdir -p $(STACK_DIST)
	@curl -fsSL -o $(STACK_DIST)/victoria-metrics.tar.gz \
	  https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/$(VM_VERSION)/victoria-metrics-linux-arm64-$(VM_VERSION).tar.gz
	@curl -fsSL -o $(STACK_DIST)/node_exporter.tar.gz \
	  https://github.com/prometheus/node_exporter/releases/download/v$(NODE_EXPORTER_VERSION)/node_exporter-$(NODE_EXPORTER_VERSION).linux-arm64.tar.gz
	@cd $(STACK_DIST) && shasum -a 256 -c ../install/stack/checksums.txt
	@tar xzf $(STACK_DIST)/victoria-metrics.tar.gz -C $(STACK_DIST)
	@tar xzf $(STACK_DIST)/node_exporter.tar.gz -C $(STACK_DIST)
	@mv $(STACK_DIST)/victoria-metrics-prod $(STACK_DIST)/victoria-metrics
	@mv $(STACK_DIST)/node_exporter-$(NODE_EXPORTER_VERSION).linux-arm64/node_exporter $(STACK_DIST)/node_exporter
	@mv $(STACK_DIST)/node_exporter-$(NODE_EXPORTER_VERSION).linux-arm64/LICENSE $(STACK_DIST)/node_exporter.LICENSE
	@mv $(STACK_DIST)/node_exporter-$(NODE_EXPORTER_VERSION).linux-arm64/NOTICE $(STACK_DIST)/node_exporter.NOTICE
	@echo "  fetched and verified: $(VM_VERSION), node_exporter $(NODE_EXPORTER_VERSION)"

.PHONY: stack-check
stack-check: ; $(info $(A1) $@)
	shellcheck install/stack/install-stack.sh scripts/stack-doctor

.PHONY: stack-sfx
stack-sfx: stack-fetch ; $(info $(A1) $@)
	@mkdir -p _stack_sfx/examples _stack_sfx/grafana/provisioning/datasources
	@cp $(STACK_DIST)/victoria-metrics $(STACK_DIST)/node_exporter _stack_sfx/
	@cp $(STACK_DIST)/node_exporter.LICENSE $(STACK_DIST)/node_exporter.NOTICE _stack_sfx/
	@cp install/stack/licenses/victoria-metrics-LICENSE _stack_sfx/victoria-metrics.LICENSE
	@cp scripts/stack-doctor _stack_sfx/
	@cp install/stack/victoria-metrics.service install/stack/node_exporter.service _stack_sfx/
	@cp install/stack/examples/scrape.yaml install/stack/examples/victoria-metrics.env install/stack/examples/node_exporter.env _stack_sfx/examples/
	@cp install/stack/grafana/provisioning/datasources/victoriametrics.yaml _stack_sfx/grafana/provisioning/datasources/
	@tar --no-xattrs -czf _stack_payload.tgz -C _stack_sfx .
	@cat install/stack/install-stack.sh _stack_payload.tgz > bme280mon-stack-$(STACK_TAG)-linux-arm64.install
	@chmod +x bme280mon-stack-$(STACK_TAG)-linux-arm64.install
	@rm -rf _stack_sfx _stack_payload.tgz
	@shasum -a 512 bme280mon-stack-$(STACK_TAG)-linux-arm64.install
	@echo "$(S0)"

.PHONY: stack-clean
stack-clean: ; $(info $(A1) $@)
	rm -rf $(STACK_DIST) _stack_sfx _stack_payload.tgz bme280mon-stack-*.install

.PHONY: clean
clean: ; $(info $(A1) $@)
	rm -f bme280mon bme280mon-*.install bme280mon.coverprofile
	rm -rf $(STACK_DIST) _stack_sfx _stack_payload.tgz bme280mon-stack-*.install
