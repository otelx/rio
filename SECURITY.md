# Security reporting

RIO is experimental. No production support or security response SLA is offered. Only the latest published alpha is maintained during initial development.

Report a suspected vulnerability privately through [GitHub private vulnerability reporting](https://github.com/otelx/rio/security/advisories/new). Include the affected version, prerequisites, impact, and a minimal reproduction. Do not publish exploit details, credentials, or private application data in a public issue. Ordinary bugs and feature requests belong in GitHub Issues.

Installation requires cluster-wide permissions, and the payload executes inside selected application containers. Read [operational limits](docs/compatibility.md) and restrict policy editing to trusted administrators.

Maintainers follow the [repository protections and credential response policy](docs/maintainers.md). Revoke or rotate exposed credentials immediately, report them privately, and investigate their use before closing secret-scanning alerts. Removing a credential from source does not revoke it.
