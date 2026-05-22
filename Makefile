SHELL := /bin/sh

BINDIR ?= bin
GO ?= go
GOFLAGS ?= -buildvcs=false
CONFIG ?= examples/config.yaml
TEST_CONFIG ?= examples/test-config.yaml

BINARIES := cpuguardd cpuguardctl cpuguard-burn

.PHONY: all build test fmt vet check check-config run-daemon burn install uninstall clean help

all: check build

build: $(addprefix $(BINDIR)/,$(BINARIES))

$(BINDIR):
	mkdir -p $(BINDIR)

$(BINDIR)/cpuguardd: $(BINDIR)
	$(GO) build $(GOFLAGS) -o $@ ./cmd/cpuguardd

$(BINDIR)/cpuguardctl: $(BINDIR)
	$(GO) build $(GOFLAGS) -o $@ ./cmd/cpuguardctl

$(BINDIR)/cpuguard-burn: $(BINDIR)
	$(GO) build $(GOFLAGS) -o $@ ./cmd/cpuguard-burn

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

check: fmt vet test check-config

check-config:
	$(GO) run $(GOFLAGS) ./cmd/cpuguardctl check-config $(CONFIG)
	$(GO) run $(GOFLAGS) ./cmd/cpuguardctl check-config $(TEST_CONFIG)

run-daemon: build
	sudo ./$(BINDIR)/cpuguardd -config $(CONFIG)

burn: build
	./$(BINDIR)/cpuguard-burn -duration 90s -recover-duration 90s

install:
	sudo sh scripts/install.sh

uninstall:
	sudo sh scripts/uninstall.sh

clean:
	rm -rf $(BINDIR)
	rm -f coverage.out coverage.html

help:
	@echo "Targets:"
	@echo "  make build        Build cpuguardd, cpuguardctl, and cpuguard-burn into ./$(BINDIR)"
	@echo "  make test         Run Go tests"
	@echo "  make fmt          Format Go files"
	@echo "  make vet          Run go vet"
	@echo "  make check        Run fmt, vet, tests, and config validation"
	@echo "  make check-config Validate example configs"
	@echo "  make run-daemon   Run cpuguardd with CONFIG=$(CONFIG)"
	@echo "  make burn         Run cpuguard-burn recovery scenario"
	@echo "  make install      Install binaries, config, and systemd unit"
	@echo "  make uninstall    Remove installed cpuguard files"
	@echo "  make clean        Remove local build outputs"
