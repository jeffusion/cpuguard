# cpuguard

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**English** | [中文](README.zh-CN.md)

cpuguard is a Linux CPU governance tool that automatically throttles host processes and Docker containers when they exceed configurable CPU thresholds, and recovers them when usage drops.

## Features

- Rule-based matching for host process trees and Docker containers
- `selector.match_all: true` for global dynamic monitoring; built-in hard protection skips critical system processes, `exclude` is for user-defined exclusions only
- Auto-throttle when a subject exceeds `90% total machine CPU` in `90% of samples over a 5-minute window`
- On trigger, CPU quota is reduced to half of the observed value at trigger time, no lower than the minimum limit
- Auto-release when a subject stays below `70% total machine CPU` in `90% of samples over a 10-minute window`
- View status, events, and manually release via Unix Socket HTTP API and `cpuguardctl`

## Project Structure

```
cpuguard/
├── cmd/
│   ├── cpuguardd/        # Daemon entry point
│   ├── cpuguardctl/      # CLI entry point
│   └── cpuguard-burn/    # Test load generator
├── internal/
│   ├── api/              # Unix Socket HTTP API
│   ├── config/           # YAML config loading & validation
│   ├── engine/           # Policy evaluation & throttle logic
│   ├── model/            # Shared types
│   ├── store/            # SQLite persistence
│   ├── system/           # /proc, cgroup v2, Docker, preflight
│   └── tui/              # Interactive terminal UI
├── examples/             # Example configs
├── packaging/systemd/    # systemd unit file
├── scripts/              # Install/uninstall scripts
├── Makefile
├── go.mod
└── go.sum
```

## Build

```bash
make build
make check
```

## Install

```bash
curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/install.sh | sudo sh
```

To install a specific version:

```bash
curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/install.sh | sudo sh -s -- v0.1.0
```

<details>
<summary>Build from source (for developers)</summary>

```bash
git clone https://github.com/jeffusion/cpuguard.git
cd cpuguard
make install
```

</details>

After installation:

```bash
sudoedit /etc/cpuguard/config.yaml
cpuguardctl check-config /etc/cpuguard/config.yaml
sudo cpuguardctl enable
sudo cpuguardctl start
cpuguardctl status
cpuguardctl doctor
cpuguardctl tui
```

Service management via `cpuguardctl`:

```bash
sudo cpuguardctl start
sudo cpuguardctl stop
sudo cpuguardctl restart
sudo cpuguardctl enable
sudo cpuguardctl disable
cpuguardctl status
cpuguardctl doctor
cpuguardctl limits
cpuguardctl logs --limit 50
cpuguardctl protected
cpuguardctl tui
```

Uninstall:

```bash
curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/uninstall.sh | sudo sh
```

To remove config and state as well:

```bash
curl -sfL https://raw.githubusercontent.com/jeffusion/cpuguard/main/scripts/uninstall.sh | sudo REMOVE_CONFIG=1 REMOVE_STATE=1 sh
```

## Run

```bash
sudo ./cpuguardd -config ./examples/config.yaml
./cpuguardctl -socket /run/cpuguardd.sock status
```

## Verify Throttling

The repository includes `cpuguard-burn`, a test load generator for verifying auto-throttle and recovery:

```bash
make build
sudo ./cpuguardd -config ./examples/test-config.yaml
./bin/cpuguard-burn -duration 90s -recover-duration 90s
```

In another terminal:

```bash
cpuguardctl tui
cpuguardctl limits
cpuguardctl logs --limit 20
```

`examples/test-config.yaml` uses short window rules matching only process name `cpuguard-burn`: throttle triggers after ~20 seconds of sustained high usage, then releases ~30 seconds after entering the low-usage recovery phase. Default production config uses 5-minute trigger and 10-minute recovery windows.

First release requirements:

- Linux `cgroup v2`
- Root privileges
- Docker support is optional; if `/var/run/docker.sock` is absent, Docker rules are ignored

Default example config monitors:

- All host processes except built-in hard-protected ones; hard protection skips PID 1/low PIDs, kernel threads, systemd/system.slice critical services, container runtimes, cpuguard itself, and container-mapped processes
- All Docker containers; to exclude a container from governance, add label: `cpuguard.ignore=true`

## Notes

- Host process governance works by creating a managed cgroup for matched process trees and dynamically setting `cpu.max`
- Docker governance works by calling Docker Engine `container update` to dynamically adjust `CpuQuota/CpuPeriod`
- Config, API, and TUI all use total machine CPU percentage: for example, on an 8-core machine, a process using ~2 logical cores shows as `25.0%` and will not trigger `cpu_percent_total_gt: 90`. Internal cgroup/Docker quota conversion is automatic — users do not need to configure per-core values.
- State and events are persisted in `SQLite`, default path `/var/lib/cpuguard/state.db`
- Event retention defaults to `10000` entries and `30` days, adjustable via `global.event_retention.max_events` and `global.event_retention.max_age`; set to `0` to disable either condition
- `cpuguardctl status` shows service status, system metrics, and subject count only; use `cpuguardctl limits` for currently throttled subjects, `cpuguardctl logs` for events, and `cpuguardctl protected` to audit built-in protection rules.
- `cpuguardctl doctor` checks config, daemon socket, API, cgroup v2, systemd, Docker, CPU thermal, and RAPL power sources; query commands support `--json`, e.g. `cpuguardctl logs --json --type error --since 1h`.
- Subjects and events are also available via the interactive `cpuguardctl tui`. TUI supports `tab` to switch regions, `r` to refresh, `u` to release limits, `h` to hold limits, `q` to quit.

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and submission guidelines.
