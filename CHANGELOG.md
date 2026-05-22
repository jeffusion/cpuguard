# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2025-05-22

### Added

- Rule-based matching for host process trees and Docker containers
- `selector.match_all: true` for global dynamic monitoring with built-in critical process protection
- Automatic CPU throttling when subjects exceed 90% total CPU over a 5-minute window
- Throttle factor: reduce to 50% of observed value, with configurable minimum limit
- Automatic recovery when subjects stay below 70% total CPU over a 10-minute window
- Manual throttle / hold / release via CLI and API
- SQLite state persistence for subjects and events
- Event retention with configurable max events and max age
- Unix Socket HTTP API
- `cpuguardctl` CLI with status, doctor, limits, logs, protected, and TUI commands
- JSON output support for query commands (`--json` flag)
- Interactive TUI with tab navigation, refresh, and direct throttle/release actions
- systemd unit file and install/uninstall scripts
- `cpuguard-burn` test load generator for validating throttle and recovery
- Built-in hard protection for PID 1, kernel threads, systemd services, container runtimes, and cpuguard itself
- Docker container label `cpuguard.ignore=true` for exclusion
- Configuration validation (`check-config` command)
- Diagnostic command (`doctor`) checking cgroup v2, Docker, systemd, CPU thermal, and RAPL
