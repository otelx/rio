# Security reporting

RIO is experimental. No production support or security response SLA is offered. Only the latest published alpha is maintained during initial development.

Report a suspected vulnerability privately through [GitHub private vulnerability reporting](https://github.com/otelx/rio/security/advisories/new). Include the affected version, prerequisites, impact, and a minimal reproduction. Do not publish exploit details, credentials, or private application data in a public issue. Ordinary bugs and feature requests belong in GitHub Issues.

Installation requires cluster-wide permissions, and the payload executes inside selected application containers. Read [operational limits](docs/compatibility.md) and restrict policy editing to trusted administrators.

See [dependency security](docs/dependency-security.md) for scanning coverage, blocking policy, exception deadlines, and the maintainer responsible for alerts and payload updates.
