# Security reporting

Report vulnerabilities through [GitHub private vulnerability reporting](https://github.com/otelx/rio/security/advisories/new). Include the affected version, prerequisites, impact, and a minimal reproduction. Keep exploit details, credentials, and private application data out of public issues.

We aim to acknowledge reports within **7 calendar days** and provide an initial assessment or request for information within **14 calendar days**. These are best-effort targets, without an SLA or fixed remediation deadline. Follow up in the private report if overdue and coordinate disclosure there.

## Maintained versions

- During alpha development, only the latest published alpha is maintained.
- After the first normal release, only the latest patch of the latest normal minor line is maintained. Prereleases remain evaluation builds.
- Older releases and custom builds receive no promised fixes or backports.

Security fixes, including dependency updates, ship in a new immutable alpha or normal patch release. Replaced patches reach end of support immediately. Announce retirement of a normal minor line in the changelog at least 30 days before publishing its replacement. An urgent security transition may shorten that notice with an advisory and mitigation. There is no LTS commitment.

[@MrAlias](https://github.com/MrAlias) handles security triage; there is no appointed backup. See [maintenance responsibilities and recovery](CONTRIBUTING.md#maintenance-responsibilities-and-recovery).

RIO is experimental and offers no production support. Installation grants cluster-wide permissions, and agents execute inside application containers. Restrict policy editing to trusted administrators and review the [operational limits](docs/compatibility.md).

See [dependency security](docs/dependency-security.md) for scanning coverage, blocking policy, exception deadlines, and the maintainer responsible for alerts and payload updates.

Maintainers follow the [repository protections and credential response policy](docs/maintainers.md). Revoke or rotate exposed credentials immediately, report them privately, and investigate their use before closing secret-scanning alerts. Removing a credential from source does not revoke it.
