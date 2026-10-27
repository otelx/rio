# Dependency scan disposition, October 27, 2026

This review addresses [issue #3](https://github.com/otelx/rio/issues/3). The original audit described `be271b8`; this change starts from `bc66684` and uses the current `rio/operator` and `rio/instrumentation` image names. Scanners identify affected versions and symbols; these results do not establish exploitability in a deployment.

## Remediation

| Component | Audited input | Updated input and disposition |
| --- | --- | --- |
| Controller toolchain | Docker Go 1.26.1; CI selected Go 1.25.0 | Go 1.27.3 for both CI and the digest-pinned builder; the builder and regression test reject drift |
| Controller modules | `golang.org/x/net` 0.47.0, `golang.org/x/text` 0.31.0 | 0.61.0 and 0.43.0, with required `x/sys` and `x/term` updates; source and compiled-binary scans clean |
| Instrumentation OS | Alpine 3.24.1 | Digest-pinned Alpine 3.24.3; addresses the reported OpenSSL, musl, and zlib matches |
| npm agents | Auto-instrumentations 0.78.0, API 1.9.1 | Auto-instrumentations 0.82.0, API 1.10.0, regenerated lock; removes eight database-instrumentation information-disclosure matches for CVE-2026-104872 |
| Python agents | Distro 0.66b0, SDK/API 1.45.0 | Distro 0.66b1, SDK/API 1.45.1, regenerated hash lock; removes CVE-2026-114603 in each libc payload |
| Java agent | 2.30.0 | 2.33.1 and matching checksum-pinned upstream SBOM; removes five high and two medium Jackson matches hidden by shading |
| .NET agents | 1.17.0 | Retained; no findings in the matching upstream inventory, with every shipped DLL covered for both libc variants |
| Ruby agents | Pinned upstream commit and frozen lock | Retained; no findings in the final gem inventory |

The Java 2.30.0 SBOM scan found CVE-2026-89407 and CVE-2026-89425 in `jackson-core` 2.22.1, and CVE-2026-68497, CVE-2026-91776, CVE-2026-91777, CVE-2026-19032, and CVE-2026-83557 in `jackson-databind` 2.22.1. Upgrade rather than assume the agent's parsing paths cannot encounter hostile data. The final 2.33.1 SBOM scan has no matches. The SBOM itself contains more packages than Trivy can map to Maven identities; preserve the original inventory and review upstream advisories for unrecognized/shaded components.

The published amd64 alpha.3 controller still reports affected Go symbols, and the instrumentation image has 23 OS matches (two high, seven medium, twelve low, two unknown), eight medium npm matches, two medium Python matches, and the seven supplemental Java matches above. These are known affected published inputs, not waived or classified as non-applicable. The scheduled scan deliberately fails on their blocking findings until a separately approved patched release is published. @MrAlias owns that release follow-up; do not mutate the existing tags. No release is published by this remediation task.

## Validation evidence

Local verification uses `govulncheck` 1.9.0 and Trivy 0.76.1 with refreshed databases. The updated amd64 source, compiled controller, final controller image, final instrumentation image, shaded Java SBOM, and .NET SBOM scans have no findings. The inventory includes 41 Go packages, 16 Alpine packages, 189 installed npm packages, 158 Python distributions and 138 Ruby gems across both libc variants, 52 Maven components from the Java SBOM, and 114 .NET DLLs per libc variant. The 41 additional Java SBOM entries without Maven identities are upstream project modules tied to the agent's source revision. See [dependency security](dependency-security.md) for reproduction commands, coverage limits, gate policy, and retained CI evidence.

`make test vet build`, security gate regression tests, Actionlint, ShellCheck, and Python compilation pass locally. All five updated agents activate on amd64 with glibc and musl. Local lifecycle runs could not create either issue-specific kind cluster: the host failed to emit kind's required `Multi-User System` startup log before any Kubernetes tests ran. CI supplies independent native amd64/arm64 loader, image, image-volume/init-container lifecycle, and multi-platform selection validation. Successful remote validation must be verified before integration.

Remaining findings in the refreshed artifacts: none at this database snapshot. There are no exceptions. Repeated scans can discover new advisories; the weekly process and before-release checks are required even after a clean result. Published alpha.3 remains affected until a new release is approved, built, scanned, and validated.
