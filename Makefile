SHELL := /bin/sh

BINDIR ?= bin
GO ?= go
GOFLAGS ?= -buildvcs=false
CONFIG ?= examples/config.yaml
TEST_CONFIG ?= examples/test-config.yaml

# Install paths
PREFIX ?= /usr/local/bin
ETC_DIR ?= /etc/cpuguard
STATE_DIR ?= /var/lib/cpuguard
SYSTEMD_DIR ?= /etc/systemd/system
SERVICE_NAME ?= cpuguard.service

BINARIES := cpuguardd cpuguardctl cpuguard-burn

.PHONY: all build test fmt vet check check-config run-daemon burn \
        install install-binaries install-config install-systemd \
        uninstall uninstall-binaries clean help

all: check build

build: clean
	mkdir -p $(BINDIR)
	$(GO) build $(GOFLAGS) -o $(BINDIR)/cpuguardd ./cmd/cpuguardd
	$(GO) build $(GOFLAGS) -o $(BINDIR)/cpuguardctl ./cmd/cpuguardctl
	$(GO) build $(GOFLAGS) -o $(BINDIR)/cpuguard-burn ./cmd/cpuguard-burn

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

# ── Install targets (source-build path, for developers) ──────────────

install: build install-binaries install-config install-systemd
	@echo ""
	@echo "Installation complete."
	@echo ""
	@echo "Next steps:"
	@echo "  1. Edit config:     $(ETC_DIR)/config.yaml"
	@echo "  2. Validate config: $(PREFIX)/cpuguardctl check-config $(ETC_DIR)/config.yaml"
	@echo "  3. Enable at boot:  $(PREFIX)/cpuguardctl enable"
	@echo "  4. Start service:   $(PREFIX)/cpuguardctl start"
	@echo "  5. Check status:    $(PREFIX)/cpuguardctl status"

install-binaries:
	install -d $(DESTDIR)$(PREFIX)
	install -m 0755 $(BINDIR)/cpuguardd $(DESTDIR)$(PREFIX)/cpuguardd
	install -m 0755 $(BINDIR)/cpuguardctl $(DESTDIR)$(PREFIX)/cpuguardctl

install-config:
	install -d $(DESTDIR)$(ETC_DIR) $(DESTDIR)$(STATE_DIR)
	if [ ! -f $(DESTDIR)$(ETC_DIR)/config.yaml ]; then \
		install -m 0644 $(CONFIG) $(DESTDIR)$(ETC_DIR)/config.yaml; \
	else \
		echo "Preserving existing config: $(DESTDIR)$(ETC_DIR)/config.yaml"; \
	fi

install-systemd:
	install -d $(DESTDIR)$(SYSTEMD_DIR)
	install -m 0644 packaging/systemd/$(SERVICE_NAME) $(DESTDIR)$(SYSTEMD_DIR)/$(SERVICE_NAME)
	systemctl daemon-reload

# ── Uninstall targets ────────────────────────────────────────────────

uninstall:
	sudo sh scripts/uninstall.sh

uninstall-binaries:
	rm -f $(DESTDIR)$(PREFIX)/cpuguardd $(DESTDIR)$(PREFIX)/cpuguardctl
	rm -f $(DESTDIR)$(SYSTEMD_DIR)/$(SERVICE_NAME)
	systemctl daemon-reload 2>/dev/null || true
	@echo "Removed binaries and systemd unit."
	@echo "Config ($(ETC_DIR)) and state ($(STATE_DIR)) are preserved."

clean:
	rm -rf $(BINDIR)
	rm -f coverage.out coverage.html

help:
	@echo "Targets:"
	@echo "  make build              Build cpuguardd, cpuguardctl, and cpuguard-burn into ./$(BINDIR)"
	@echo "  make test               Run Go tests"
	@echo "  make fmt                Format Go files"
	@echo "  make vet                Run go vet"
	@echo "  make check              Run fmt, vet, tests, and config validation"
	@echo "  make check-config       Validate example configs"
	@echo "  make run-daemon         Run cpuguardd with CONFIG=$(CONFIG)"
	@echo "  make burn               Run cpuguard-burn recovery scenario"
	@echo "  make install            Build and install binaries, config, and systemd unit"
	@echo "  make install-binaries   Install pre-built binaries only (requires make build first)"
	@echo "  make install-config     Install default config only"
	@echo "  make install-systemd    Install systemd unit only"
	@echo "  make uninstall          Remove binaries and systemd unit"
	@echo "  make clean              Remove local build outputs"
