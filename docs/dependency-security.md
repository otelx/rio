# Dependency security

RIO follows the [OpenSSF Best Practices vulnerability analysis and dependency maintenance criteria](https://www.bestpractices.dev/en/criteria/0). Scanner matches are evidence for review, not proof of exploitability. Only the latest published alpha is maintained; development fixes land on `main` before a separately approved release. An old image tag is immutable and does not acquire fixes from `main`.

## Ownership and cadence

@MrAlias owns dependency alerts, failed scheduled security checks, scanner updates, Go patch updates, and payload pin/lockfile refreshes. A release maintainer must verify these checks before tagging. Review alerts and the scheduled run every Monday, and check all upstream pins before every release. If the owner is unavailable, another repository maintainer must explicitly take the tracked work; silence is not acceptance of risk.

Dependabot security updates are enabled in repository settings. Weekly version-update PRs cover Go modules, npm, Python, Bundler, GitHub Actions, and Dockerfiles, including digest-pinned image inputs. Actions use full commit SHAs. Go's patch version in `go.mod` and the digest-pinned controller builder must move together; the builder rejects a mismatch and a regression test checks the pins. Dependabot cannot maintain the custom release-asset checksums in `payload/versions.json`, Java SBOM pins in `security/java-sboms.json`, Ruby's upstream source revision, or scanner archive hashes in `scripts/install-security-tools.sh`. The owner checks these weekly, verifies upstream checksums, regenerates locks, and submits a reviewed PR. Do not silently replace an immutable checksum after an upstream asset changes.

## Checks and release gate

Every PR, push to `main`, and release tag runs `govulncheck ./...`, scans the compiled controller extracted from its final image, and scans final `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation` images on native amd64 and arm64 workers. These checks run before the tested image archives can reach release publication. Every Monday at 07:23 UTC, the same pipeline rebuilds and checks current source and also pulls and scans the latest published release, including prereleases. Manual dispatch performs both checks. A scan failure blocks that run; maintainers must inspect it even when it concerns an older published image.

The final-image Trivy report includes all severities, unfixed findings, OS packages, and package inventories. Inventory checks require Go in the controller and Alpine, npm, Python, and Ruby in the instrumentation image, plus Java and NuGet packages in the supplemental scans. A missing inventory or scanner/database/download error fails the check. No scanner ignore file or `--ignore-unfixed` is used. The controller uses `scratch`, so it has no OS package database to inventory.

The Java agent shades dependencies, and the .NET bundle omits `*.deps.json`; a container scan alone misses dependencies in both. Additional Trivy SBOM scans use checksum-pinned upstream release inventories. For Java, verify the JAR extracted from the final image against its recorded hash before scanning the matching agent SBOM, including shaded Jackson dependencies. For .NET, verify the upstream SPDX checksum and release asset pins, select the final image's architecture, and require an exact match between every shipped DLL path and the inventory for both glibc and musl. The .NET inventory is downloaded and verified separately, including when scanning alpha.3. Keep the alpha.3 Java inventory pin until that release leaves support. Upstream SBOM identities and versions remain upstream assertions; path matching does not independently reverse-engineer assemblies or native libraries. Review upstream advisories for Injector/native libraries and shaded code whose metadata is incomplete. A clean scan does not establish the absence of vulnerabilities.

Any reachable Go source finding or affected symbol in the compiled controller blocks the pipeline. High, critical, and unknown-severity image/SBOM findings block by default, whether or not a fix exists. Review critical findings within one day and high/unknown findings within seven days. Review medium findings within 30 days and low findings within 90 days; open tracked remediation work with an owner and deadline for applicable findings. Fix applicable findings before the first non-prerelease release. Non-applicability requires evidence about the shipped component, call path, inputs, and deployment assumptions. Do not classify a match as harmless solely because it is transitive, unused in a smoke test, or lacks a known exploit.

For an image/SBOM finding that requires an exception, add an entry to `security/exceptions.json` through a maintainer-reviewed PR. Required fields are `role` (`operator`, `instrumentation`, `java`, or `dotnet`), exact scanner `target`, advisory `id`, `package`, installed `version`, `owner`, evidence-backed `reason`, repository `issue` or advisory URL, ISO `reviewed` date, and ISO `expires` date. Scope to the exact finding; wildcards are unsupported. Exceptions expire at the start of their expiry date in UTC and may last at most 30 days. Missing fields, future review dates, and expired or longer exceptions fail the gate. Reassess any renewal with new evidence; do not extend dates automatically. Go findings require remediation rather than a scanner suppression. There are currently no exceptions.

JSON inventories, image IDs/digests, scanner versions, binary scan results, and supplemental SPDX inventories are retained as CI artifacts for 30 days. Link the relevant run, image digest, applicability analysis, owner, and deadline from the remediation issue. Use [private reporting](../SECURITY.md) for sensitive details.

## Reproduce and refresh

Use the Go patch release in `go.mod`. On Linux amd64 or arm64:

```bash
scripts/install-security-tools.sh
export PATH="$PWD/.artifacts/bin:$PATH"
govulncheck ./...
make images
scripts/scan-image.sh operator ghcr.io/otelx/rio/operator:dev .artifacts/security/operator
scripts/scan-image.sh instrumentation ghcr.io/otelx/rio/instrumentation:dev .artifacts/security/instrumentation
python3 -m unittest discover -s scripts -p test_security.py
```

For a published image, pull it with `docker pull --platform linux/amd64 IMAGE:VERSION` (or `linux/arm64`) before scanning it. `scan-image.sh` scans the image in the local Docker store and records its identity; it does not refresh tags implicitly. Keep output directories separate between images and architectures. Source and binary scanners use the current Go vulnerability database; Trivy updates its vulnerability and Java databases normally. Do not freeze those databases to obtain a passing result.

Update `payload/versions.json` and language locks together. Regenerate npm and Python locks with `make -C payload lock`, and update Ruby's frozen Bundler graph when its upstream revision changes. When updating Java, add the upstream SBOM ZIP URL/hash and matching JAR hash to `security/java-sboms.json`. When updating .NET, pin its SPDX file alongside the binary ZIPs, and verify complete DLL coverage for both architectures. Retest all five loaders with glibc and musl, then verify application trace export and both delivery methods. See [development](development.md) for the integration commands and [the scan disposition](security-scan-2026-10-27.md) for the initial remediation evidence.
