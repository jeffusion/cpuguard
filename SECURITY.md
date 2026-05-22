# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in cpuguard, please report it responsibly.

**Preferred method:** Open a [GitHub Security Advisory](https://github.com/jeffusion/cpuguard/security/advisories/new) using the "Report a vulnerability" option.

**Alternative:** Open a private [GitHub Issue](https://github.com/jeffusion/cpuguard/issues) and label it `security`.

Please include:

- A description of the vulnerability
- Steps to reproduce or a proof of concept
- The affected version(s)
- Any potential impact

We will acknowledge your report within 48 hours and provide an estimated timeline for a fix.

## Scope

cpuguard operates with root privileges and manages cgroup and Docker resources. Security issues of particular concern include:

- Privilege escalation via the Unix Socket API
- Unintended throttling of critical system processes
- Container escape or Docker API misuse
- Denial of service through configuration manipulation

## Out of Scope

- Issues requiring root access to exploit (cpuguard already runs as root by design)
- Social engineering attacks
- Vulnerabilities in third-party dependencies (report to the upstream project)

## Disclosure Policy

We practice coordinated disclosure. Once a fix is released, we will publish a security advisory on GitHub with credit to the reporter (unless anonymity is requested).
