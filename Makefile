GO_VERSION = 1.26
PACKAGE_ROOT = github.com/e4jet/bme280mon
TAG = v0.1.0
GOOS = linux
GOARCH = arm64

A1 = $(shell printf "»")
A2 = $(shell printf "»»")
S0 = 😁

.PHONY: help
help:
	@echo "Main:"
	@echo "    all      - clean, debug, check, and build"
	@echo "    check    - fmt, vet, lint"
	@echo "    test     - run unit tests with -race"
	@echo "    vuln     - govulncheck"
	@echo "    sfx      - build self-extracting installer for Raspberry Pi 4"
	@echo "    clean    - remove build artifacts"
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
	@cp install/examples/config.yaml _sfx/examples/
	@tar --no-xattrs -czf _payload.tgz -C _sfx .
	@cat install.sh _payload.tgz > bme280mon-$(TAG)-linux-arm64.install
	@chmod +x bme280mon-$(TAG)-linux-arm64.install
	@rm -rf _sfx _payload.tgz
	@shasum -a 512 bme280mon-$(TAG)-linux-arm64.install
	@echo "$(S0)"

.PHONY: clean
clean: ; $(info $(A1) $@)
	rm -f bme280mon bme280mon-*.install bme280mon.coverprofile
