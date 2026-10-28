# Redistributed components

RIO's Apache-2.0 license covers RIO's own code. The instrumentation image contains
other packages with their own terms. An image label is not a license inventory.

| Distribution | Version/source inventory | Preserved material |
| --- | --- | --- |
| Operator Go dependencies | `go.mod`, `go.sum`, and compiled-image SBOM | `go-licenses/v2@v2.0.1 save` follows the compiled dependency graph and copies redistribution material into `/licenses/go`; the Go runtime license is included there too. |
| Injector shared library | `payload/versions.json`, Injector v0.11.0 asset SHA-256 per architecture | Upstream Apache-2.0 license and Zig 0.15.2 standard-library MIT notice in `/licenses/upstream/injector/`. Its source build disables libc and declares no external Zig packages. |
| Java agent | `payload/versions.json`, Java instrumentation v2.30.0 asset SHA-256 | Embedded `META-INF/licenses/` and other license/notice entries remain in the agent JAR. The notice inventory extracts these entries as well. |
| .NET agents, glibc and musl | `payload/versions.json`, .NET instrumentation v1.17.0 asset SHA-256; `payload/licenses/dotnet/managed/inventory.json` | Upstream distribution `LICENSE` files remain under `/dist/agents/dotnet/{glibc,musl}`. Supplemental fmt, spdlog 1.15.3, .NET Foundation MIT, and GNU runtime license texts are in `/licenses/upstream/dotnet/`. The `managed/` inventory maps every upstream managed DLL to original NuGet package bytes or the instrumentation source project, with preserved package metadata, licenses, and third-party notices. Exact native runtime inputs require further review. |
| Node.js agent and dependencies | `payload/node/package-lock.json` | npm package license and notice files remain under `/dist/agents/node/node_modules`. |
| Python agents and dependencies, glibc and musl | `payload/python/requirements.lock` | Wheel license/notice and dist-info material remains under `/dist/agents/python/{glibc,musl}`. |
| Ruby agents and dependencies, glibc and musl | `payload/ruby/Gemfile.lock`, source revision in `payload/versions.json` | Gem license and notice files remain under `/dist/agents/ruby/{glibc,musl}`. |
| Alpine runtime packages | Pinned `RUNTIME_IMAGE` in `payload/Dockerfile`; `/lib/apk/db/installed`; `payload/alpine-sources.lock.json` | `/licenses/sources/alpine-source-inputs.tar.gz` preserves upstream source archives, patches, build recipes, and packaging scripts for every installed origin. The source distribution includes the same bundle through the final-image snapshot. |

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

The collector also retains README and AUTHORS files: `data-uri-to-buffer` and
`pg-types`, for example, put the complete MIT grant and copyright attribution in
their README instead of a separate LICENSE. Nested `web-streams-polyfill` entry
points share their parent package's LICENSE. Supplemental material is copied
verbatim from immutable upstream commits; [the material manifest](payload/licenses/manifest.json)
records its source and SHA-256. GNU runtime texts do not identify or certify the
actual compiler/runtime build used upstream, and the .NET Foundation MIT text is
not a claim about the version of the copied CoreCLR headers.
The native [ELF inventory](payload/licenses/dotnet/native-inventory.json) records
all four profiler hashes, dynamic dependencies, and compiler comments. Those
comments identify GCC 9.4.0/Clang 5.0.2 for glibc amd64, GCC 12.3.0/Clang 14 for
glibc arm64, and Alpine GCC 15.2.0/Clang 21.1.2 for both musl targets. They do not
identify every statically linked runtime object or establish all of its terms.
The profiler's miniutf implementation carries Dropbox's MIT grant, and the
vendored CoreCLR LLVM headers retain the University of Illinois/NCSA license.
Their complete original license headers are preserved in `MINIUTF-LICENSE` and
`LLVM-NOTICE` alongside the other native notices; the manifest records extraction
boundaries and the full source-file hashes.

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
package database alone does not supply source material. The bundled inputs now
cover all 10 origins: 139 packaging files and 126 checksum-listed inputs, stored
as 148 unique files. Git blob identities and the recipes' SHA-512 sums were
independently checked when producing the lock. Builds validate the actual package
inventory and recipe versions, then verify SHA-256, applicable Git blob identities,
and APorts SHA-512 checksums before creating the bundle. Recipes are never executed.
The upstream archives retain their license/notice texts. This is a source-input
inventory, not a claim of bit-for-bit binary reproducibility or a completed legal
review. .NET's native runtime inventory remains an outstanding gap.
The Java agent already embeds upstream notices;
Node.js, Python, and Ruby package material is retained and still needs reconciliation
against the full per-architecture inventory.

The managed .NET comparison covers all 228 DLL paths (114 unique binaries) in
each architecture's extracted upstream distribution. Those upstream bytes are
identical between architectures. RIO retains the original startup hook unchanged
as `OpenTelemetry.AutoInstrumentation.StartupHook.original.dll` and adds its own
bootstrap and propagation assemblies from [payload/dotnet](payload/dotnet/README.md).
These two RIO assemblies use the project's Apache-2.0 license; their build inputs
are recorded separately from the upstream agent pins in `payload/versions.json`.
There are 71 distinct NuGet package/version pairs, matched by DLL SHA-256 to
members of packages downloaded from NuGet's official flat-container endpoint.
Five instrumentation project assemblies occur once per libc and are covered by
the pinned instrumentation source license. Package product versions are not
reliable package identifiers: the Binder DLL reporting `8.0.7`, for example,
matches package `Microsoft.Extensions.Configuration.Binder/8.0.2` byte for byte.

`payload/licenses/dotnet/managed/` preserves 84 unique text files, including
original package metadata and every packaged license/third-party notice. When
a package supplies only a license expression or URL, its license text is copied
from the package's recorded source commit (the OpenTracing release tag supplies
its source commit). The inventory records each text's SHA-256 and original URL
or package URL, package checksum, and archive member. The image build verifies
all preserved texts and the exact complete DLL set against that inventory after
extracting the four pinned upstream distributions. New, removed, or changed DLLs
fail the build until their coverage is reconciled. These checks bind evidence to
the shipped bytes; they do not approve native dependencies or the overall review.

To refresh this inventory, extract all four pinned .NET assets, match each
dependency DLL's SHA-256 against its original NuGet package member, and record
the package checksum/member and license material sources. Use source project
coverage only for the instrumentation's own assemblies. Retain package metadata
and all packaged notices, supplying source license texts where necessary. Compare
both architectures, then run `payload/scripts/check_managed_notices.py` with the
extracted distribution, material directory, and `payload/versions.json`. Rebuild
and inspect both final images before renewing the review.

## Refresh Alpine source inputs

Changing the pinned runtime image requires a matching source lock. Read its APK
database, then run the updater with GitHub CLI access to the public APorts repo:

```bash
mkdir -p .artifacts/alpine-sources
SOURCE_BASE_IMAGE=$(python3 -c 'from pathlib import Path; print(next(line.split("=",1)[1] for line in Path("payload/Dockerfile").read_text().splitlines() if line.startswith("ARG RUNTIME_IMAGE=")))')
docker run --rm "$SOURCE_BASE_IMAGE" cat /lib/apk/db/installed > .artifacts/alpine-sources/installed
python3 payload/scripts/lock_alpine_sources.py .artifacts/alpine-sources/installed \
  "$SOURCE_BASE_IMAGE" payload/alpine-sources.lock.json --cache .artifacts/alpine-sources/cache
```

The updater retrieves the complete origin directories at their package-recorded
commits and verifies archive inputs against the recipes. It updates the lock only
after every input passes, without executing third-party scripts. Review the lock
diff, rebuild both architectures, and reconcile their final-image inventories
before renewing the release review. Unsupported packaging layouts fail closed.
The bundle is about 57 MiB for the current base; including it makes the sources
available directly with each image and in the source distribution, without
depending on a future external mirror remaining online.

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
