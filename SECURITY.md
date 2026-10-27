# Security reporting

RIO is currently experimental and offers no production support or security response SLA. The [support contract](docs/compatibility.md) defines the limited scope intended for the first normal release; no normal release is currently published.

## Maintained versions and security fixes

| Release range | Maintenance |
| --- | --- |
| Before the first normal release | Only the latest published alpha; earlier alphas are end of support |
| After the first normal release | Only the latest normal patch of the latest normal minor line; prereleases are evaluation builds |
| Older patches/minors, retired image packages, custom builds | No promised fixes or backports; migrate to a maintained release |

Security fixes, including updates to bundled upstream dependencies, are delivered in a new immutable release of the maintained line: a new alpha during initial development, then a patch for normal releases. Earlier patches reach end of support when their replacement is published. Before replacing a normal minor line, announce its retirement in the changelog at least 30 calendar days in advance; publish the new minor only after that notice period. Urgent security changes may require an earlier transition with an explicit advisory and mitigation. There is no LTS branch or guaranteed lifetime. Image/source availability alone does not indicate maintenance, and no fixed remediation deadline is promised.

## Private reports and response target

Report a suspected vulnerability privately through [GitHub private vulnerability reporting](https://github.com/otelx/rio/security/advisories/new). Include the affected version, prerequisites, impact, and a minimal reproduction. Do not publish exploit details, credentials, or private application data in a public issue. Ordinary bugs and feature requests belong in GitHub Issues.

The initial target is to acknowledge a private report within **7 calendar days** and provide an initial triage assessment or request for information within **14 calendar days**. These are best-effort targets for a project with one maintainer, not an on-call service or SLA. If acknowledgement is overdue, follow up in the private report. Coordinate disclosure and mitigation there; do not assume a release date or publish sensitive details because a target was missed.

Release decisions, issue/security triage, and dependency maintenance belong to [@MrAlias](https://github.com/MrAlias), the current repository administrator. There is no appointed backup maintainer or independent security response team. See [maintainer responsibilities and recovery](CONTRIBUTING.md#maintenance-responsibilities-and-recovery) for the limits and recovery arrangements. If the maintainer is unavailable, responses and releases can pause; users needing guaranteed response coverage should arrange independent maintenance before adoption.

Installation requires cluster-wide permissions, and the payload executes inside selected application containers. Read [operational limits](docs/compatibility.md) and restrict policy editing to trusted administrators.
