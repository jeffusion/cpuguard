# cpuguard

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**English** | [中文](#cpuguard-中文)

---

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
cd cpuguard
sudo bash scripts/install.sh
```

The install script will:

- Build `cpuguardd` and `cpuguardctl`
- Install binaries to `/usr/local/bin`
- Copy example config to `/etc/cpuguard/config.yaml`
- Install systemd unit to `/etc/systemd/system/cpuguard.service`
- Reload systemd daemon

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
cd cpuguard
sudo bash scripts/uninstall.sh
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

---

# cpuguard 中文

`cpuguard` 是一个 Linux 宿主机与 Docker 容器 CPU 自治限流工具。

## 能力

- 按规则匹配宿主机进程树和 Docker 容器
- 支持 `selector.match_all: true` 进行全局动态监听；系统关键进程由内置硬保护跳过，`exclude` 只用于用户自定义排除
- 当治理对象在 `5 分钟窗口内 90% 采样点` 超过 `整机总 CPU 90%` 时自动限流
- 触发后将 CPU 配额降为触发瞬间观测值的一半，且不低于最小限制
- 当治理对象在 `10 分钟窗口内 90% 采样点` 低于 `整机总 CPU 70%` 时自动解除限制
- 通过 Unix Socket HTTP API 和 `cpuguardctl` 查看状态、事件、手动解除

## 项目结构

```
cpuguard/
├── cmd/
│   ├── cpuguardd/        # 守护进程入口
│   ├── cpuguardctl/      # 命令行工具入口
│   └── cpuguard-burn/    # 测试负载生成器
├── internal/
│   ├── api/              # Unix Socket HTTP API
│   ├── config/           # YAML 配置加载与校验
│   ├── engine/           # 策略评估与限流逻辑
│   ├── model/            # 公共类型
│   ├── store/            # SQLite 持久化
│   ├── system/           # /proc、cgroup v2、Docker、预检
│   └── tui/              # 交互式终端界面
├── examples/             # 示例配置
├── packaging/systemd/    # systemd 服务单元
├── scripts/              # 安装/卸载脚本
├── Makefile
├── go.mod
└── go.sum
```

## 构建

```bash
make build
make check
```

## 安装

```bash
cd cpuguard
sudo bash scripts/install.sh
```

安装脚本会执行这些操作：

- 编译 `cpuguardd` 和 `cpuguardctl`
- 安装到 `/usr/local/bin`
- 将示例配置复制到 `/etc/cpuguard/config.yaml`
- 安装 systemd 服务到 `/etc/systemd/system/cpuguard.service`
- 重新加载系统服务定义

安装后执行：

```bash
sudoedit /etc/cpuguard/config.yaml
cpuguardctl check-config /etc/cpuguard/config.yaml
sudo cpuguardctl enable
sudo cpuguardctl start
cpuguardctl status
cpuguardctl doctor
cpuguardctl tui
```

服务管理统一使用 `cpuguardctl`：

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

卸载：

```bash
cd cpuguard
sudo bash scripts/uninstall.sh
```

## 运行

```bash
sudo ./cpuguardd -config ./examples/config.yaml
./cpuguardctl -socket /run/cpuguardd.sock status
```

## 验证限流

仓库提供 `cpuguard-burn` 测试负载程序，用于验证自动限流和恢复：

```bash
make build
sudo ./cpuguardd -config ./examples/test-config.yaml
./bin/cpuguard-burn -duration 90s -recover-duration 90s
```

另开终端观察：

```bash
cpuguardctl tui
cpuguardctl limits
cpuguardctl logs --limit 20
```

`examples/test-config.yaml` 使用短窗口规则，只匹配进程名 `cpuguard-burn`：持续高占用约 20 秒后触发限流，进入低占用恢复阶段后约 30 秒解除。默认生产配置仍使用 5 分钟触发和 10 分钟恢复窗口。

首版默认依赖：

- Linux `cgroup v2`
- root 权限
- Docker 支持可选；若本机不存在 `/var/run/docker.sock`，Docker 规则会被忽略

默认示例配置会监控：

- 宿主机上除内置硬保护对象之外的全部进程；硬保护会跳过 PID 1/低 PID、内核线程、systemd/system.slice 关键服务、容器运行时、cpuguard 自身和容器内映射进程
- 全部 Docker 容器；如果某个容器不希望被治理，可加 label：`cpuguard.ignore=true`

## 说明

- 宿主机进程治理通过为命中进程树创建受管 cgroup 并动态设置 `cpu.max` 实现
- Docker 治理通过调用 Docker Engine `container update` 动态调整 `CpuQuota/CpuPeriod`
- 配置、API 和 TUI 统一使用整机总 CPU 百分比：例如 8 核机器上，一个占用约 2 个逻辑核的进程会显示为 `25.0%`，不会触发 `cpu_percent_total_gt: 90`。内部执行 cgroup/Docker 配额时会自动换算，用户不需要按核心数配置。
- 状态和事件持久化在 `SQLite` 中，默认路径为 `/var/lib/cpuguard/state.db`
- 事件记录默认最多保留 `10000` 条和 `30` 天，可通过 `global.event_retention.max_events` 与 `global.event_retention.max_age` 调整；设置为 `0` 表示关闭对应条件
- `cpuguardctl status` 只显示服务、系统指标和治理对象数量；当前限流对象使用 `cpuguardctl limits` 查看，事件使用 `cpuguardctl logs` 查询，内置保护策略使用 `cpuguardctl protected` 审计。
- `cpuguardctl doctor` 会检查配置、daemon socket、API、cgroup v2、systemd、Docker、CPU 温度和 RAPL 功率源；查询类命令支持 `--json`，例如 `cpuguardctl logs --json --type error --since 1h`。
- 治理对象和事件也可通过交互式 `cpuguardctl tui` 集中展示。TUI 支持 `tab` 切换区域、`r` 刷新、`u` 解除限制、`h` 保持限制、`q` 退出。

## 贡献

欢迎贡献！请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 了解开发流程和提交规范。
