# Contributing to cpuguard

Thank you for your interest in contributing to cpuguard! This document provides guidelines and instructions for contributing.

## Prerequisites

- Go 1.26 or later
- Linux with cgroup v2 enabled
- Root access (required for cgroup and Docker operations at runtime)
- Docker (optional, for container governance features)

## Development Setup

```bash
git clone https://github.com/jeffusion/cpuguard.git
cd cpuguard
make build
make check
```

## Making Changes

### Code Style

- Run `gofmt` before committing — all code must be formatted.
- Run `go vet` and fix any warnings.
- Keep functions small and focused.
- Follow existing naming conventions: `Rule`, `Subject`, `Event` for public types; unexported helpers for internal logic.
- YAML configuration keys use `snake_case` and are user-facing — keep them stable.

### Commit Messages

Use short, imperative-style commit messages:

```
Add event retention policy
Fix cgroup cleanup on subject removal
Protect critical host processes
```

### Testing

- All new features and bug fixes must include tests.
- Tests live alongside the package under test as `*_test.go`.
- Run the full test suite:

```bash
make test
```

- Unit tests must not require root, Docker, or writable `/sys/fs/cgroup`. Those belong in integration or manual testing.

### Safety

- Never add tests or examples that throttle PID 1, systemd services, Docker runtime processes, or cpuguard itself.
- Critical process protection must remain enforced in code, not only in user config.
- Default configs must be conservative.

## Submitting Changes

1. **Fork** the repository on GitHub.
2. **Create a branch** for your change:
   ```bash
   git checkout -b my-feature
   ```
3. **Commit** your changes with clear messages.
4. **Push** to your fork:
   ```bash
   git push origin my-feature
   ```
5. **Open a Pull Request** against the `main` branch.

### Pull Request Guidelines

- Describe the behavior change clearly.
- Include validation commands you ran.
- Call out any impact on root, cgroup, Docker, or systemd.
- Ensure `make check` passes (fmt, vet, test, config validation).

## Reporting Issues

- Use [GitHub Issues](https://github.com/jeffusion/cpuguard/issues).
- Include your OS, kernel version, and cpuguard version.
- For bugs, provide steps to reproduce and relevant `cpuguardctl doctor` output.

## Questions?

Feel free to open an issue with the `question` label, or start a discussion on GitHub.
