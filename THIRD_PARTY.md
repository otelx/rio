# Redistributed components

RIO's Apache-2.0 license covers RIO's own code. The instrumentation image contains
other packages with their own terms. An image label is not a license inventory.

| Distribution | Version/source inventory | Preserved material |
| --- | --- | --- |
| Operator Go dependencies | `go.mod`, `go.sum`, and compiled-image SBOM | `go-licenses/v2@v2.0.1 save` follows the compiled dependency graph and copies redistribution material into `/licenses/go`; the Go runtime license is included there too. |
| Injector shared library | `payload/versions.json`, Injector v0.11.0 asset SHA-256 per architecture | Tagged upstream Apache-2.0 license in `payload/licenses/injector/LICENSE`, copied to `/licenses/upstream/injector/LICENSE`. Embedded native dependencies require further review. |
| Java agent | `payload/versions.json`, Java instrumentation v2.30.0 asset SHA-256 | Embedded `META-INF/licenses/` and other license/notice entries remain in the agent JAR. The notice inventory extracts these entries as well. |
| .NET agents, glibc and musl | `payload/versions.json`, .NET instrumentation v1.17.0 asset SHA-256 | Upstream distribution `LICENSE` files remain under `/dist/agents/dotnet/{glibc,musl}`. Embedded native/managed dependencies require further review. |
| Node.js agent and dependencies | `payload/node/package-lock.json` | npm package license and notice files remain under `/dist/agents/node/node_modules`. |
| Python agents and dependencies, glibc and musl | `payload/python/requirements.lock` | Wheel license/notice and dist-info material remains under `/dist/agents/python/{glibc,musl}`. |
| Ruby agents and dependencies, glibc and musl | `payload/ruby/Gemfile.lock`, source revision in `payload/versions.json` | Gem license and notice files remain under `/dist/agents/ruby/{glibc,musl}`. |
| Alpine runtime packages | Pinned final base in `payload/Dockerfile`; `/lib/apk/db/installed` in the final image; image SBOM | Package database is preserved in generated `base-packages.txt`. Complete license texts and corresponding-source availability still require review. |

Build-only Go, Node, Python, Ruby, and download-stage operating systems are not
copied wholesale into final images. Their copied agent/package outputs are
redistributed and must be covered by the inventory. The Java JAR and .NET native
binaries can contain dependencies not exhaustively detected by an SBOM scanner.

CI extracts notice files from each **final tested image**, hashes them in
`inventory.json`, and includes the snapshots under
`third_party/{amd64,arm64}/{operator,instrumentation}/` in the release source
archive. The Java JAR's embedded notices are extracted there. The instrumentation
source attribution is in [payload/NOTICE](payload/NOTICE); RIO attribution is in
[NOTICE](NOTICE). These snapshots preserve existing files; they do not establish
that every required upstream notice or source obligation has been satisfied.

## Audit findings, 2026-10-27

Local amd64 scans of the updated images produced SPDX 2.3 documents with 42
operator packages and 771 instrumentation packages. Syft reported `NOASSERTION`
for the declared license of 41 operator packages and 235 instrumentation packages,
including .NET assemblies. This does not establish that a license is absent;
the operator notice snapshot separately contains 52 redistribution files.
The instrumentation archive scan preserved 541 existing notice files before
adding the Injector license. Scanner declarations alone cannot approve the review.

The pinned Alpine runtime includes 16 packages. Its package metadata identifies
GPL terms for alpine-baselayout, apk-tools/libapk, BusyBox, musl-utils, and scanelf,
along with MIT, Apache, MPL, BSD, and Zlib terms for other packages. Preserving the
package database does not supply all associated license/notice texts or verify
corresponding-source availability. Those base-image materials, the Injector's
embedded dependencies, and .NET's embedded dependency redistribution material
are concrete outstanding gaps. The Java agent already embeds upstream notices;
Node.js, Python, and Ruby package material is retained and still needs reconciliation
against the full per-architecture inventory.

## Outstanding release gate

`THIRD_PARTY_REVIEW.json` is deliberately `pending`. Complete the following before
approving publication:

- Reconcile all four final-image SBOMs with actual redistributed packages and
  native binaries, including Injector and .NET embedded dependencies.
- Check each package's required notices against the final-image inventory. Add
  missing redistribution files to the image build inputs and retest those images.
- Verify and record corresponding-source availability for the Alpine runtime
  packages and any other components that require it. Preserve the applicable
  license/notice texts in the final image and source distribution.
- Record the review evidence and reviewer in `THIRD_PARTY_REVIEW.json`, set
  `status` to `approved`, and set `inputs_sha256` to the output of
  `python3 scripts/check_license_review.py --digest` for the reviewed commit.

The digest covers packaging, Go compilation sources, dependency locks, RIO
attribution, payload inputs, and the notice collector. Source changes can activate
an already-locked dependency, so locks alone cannot bind a review. Changing an
input invalidates approval. CI refuses to
reserve a draft or push release images while the review is pending or stale.
Do not mark the review approved solely because scans or file-presence checks pass.
