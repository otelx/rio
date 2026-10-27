# Release publication and verification

Release tags use `vMAJOR.MINOR.PATCH`, optionally followed by SemVer prerelease
identifiers and build metadata. Numeric fields and numeric prerelease identifiers
cannot have leading zeros. Stable tags create normal releases; tags with a
prerelease suffix create GitHub prereleases. Container tags replace SemVer's `+`
with `_` because registries do not accept `+`; for example, `v1.2.3+build.7`
uses image tag `v1.2.3_build.7`. Version strings are limited to 120 characters so
architecture suffixes fit. No moving `latest` tag is published.

Before creating a tag, add a nonempty `## VERSION` section to `CHANGELOG.md` and
update the installer, install template, example policies, quickstart, and
configuration reference to the image tag. Release notes contain that section's
changes, the two image-index digests, and the source checksum.

## Publication gates

The source guard requires the exact resulting commit of a merged PR to protected
`main`, plus a successful CI push run on `main` for that commit. It runs before
registry authentication and again before draft reservation. A passing tag build
alone does not authorize a release. See [maintainer controls](maintainers.md).

The tag workflow:

1. Runs source checks, native amd64/arm64 builds, all five language loaders with
   glibc and musl, both delivery integration suites, and execution through
   multi-platform tags in a disposable registry.
2. Inventories each final image with Syft, extracts its notice files, and archives
   the tested images with their local image IDs and archive checksums. Publication
   loads those archives and checks those identities; it never rebuilds images.
3. Requires the redistribution review described in [THIRD_PARTY.md](../THIRD_PARTY.md)
   and verifies that repository release immutability is enabled.
4. Rejects every existing GitHub release/draft for the version, and every existing
   common or architecture image tag in **both** image repositories. Authentication,
   transport, rate-limit, and server errors stop publication. Only an explicit
   manifest-not-found response permits a write.
5. Reserves a GitHub draft before pushing any release image. A serialized publication
   job and GitHub's unique draft/tag reservation guard concurrent attempts. The
   image publisher independently checks all six destination tags before any push.
6. Pushes the tested architecture images and combines their digests into the common
   indexes. Records all six manifest/index digests in `IMAGE_DIGESTS` and immutable
   references in `release.json`.
7. Creates a source distribution from the tag, including `third_party/` notice
   snapshots extracted from those exact images. Scans that distribution and signs
   authenticated source/index provenance and source/architecture SBOM attestations
   with GitHub Actions OIDC. Registry attestations are OCI referrers; they do not
   change the already-tested runnable image manifests or index selection.
8. On fresh native amd64 and arm64 runners, uses empty Docker authentication configs
   to pull the final references, checks and executes the common tags, and runs the
   [quickstart](quickstart.md) in new kind clusters. Kubernetes pulls both images
   directly from GHCR, with no preloading or pull secrets. The test verifies HTTP
   server spans, policy removal, and uninstall. Successful records bind the commit,
   workflow run, architecture, and exact digests.
9. Verifies the authenticated attestations and all six version references again,
   requires both architecture verification records, attaches all assets to the
   reserved draft, and then publishes it. Confirms the published release is immutable.

`SHA256SUMS` covers the source archive, image digest list, release metadata, five
SPDX SBOMs, signed bundles, and both architecture verification records. The source
archive also has a separate `SOURCE_SHA256SUMS` file. The added `third_party/`
directory means the distribution is not byte-identical to a bare `git archive`.

## Required repository configuration

An administrator must enable [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
and retain the active `Protect published version tags` ruleset: target tags,
`refs/tags/v*`, restrict updates and deletions, no bypass actors. New version tags
may be created. On 2026-10-27 immutability was enabled and ruleset
[25984878](https://github.com/otelx/rio/rules/25984878) was applied and read back.

Configure Actions secret `RELEASE_SETTINGS_TOKEN` with access only to this
repository and **Administration: read** permission. It is used only to read the
effective immutability setting before any image push and immediately before release
publication. GitHub's standard workflow token has no Administration permission;
use the workflow token for artifact/package writes, not this settings token.
Missing credentials or disabled immutability stop publication.

Keep both GHCR packages public, linked to `otelx/rio`, and restrict package write
and deletion access to release maintainers and the repository's Actions workflow.
GHCR version tags are mutable references; the workflow's checks and reservation
prevent accidental replacement through this publication path, but do not prevent
an independently authorized package writer from changing a tag or deleting a
package. Digest references pin content; retain their package versions as well.
Review package access before a release. The current local OAuth credential lacks
`read:packages`, so package ACLs could not be audited or changed during issue #5.

Enabling immutability does **not** retroactively lock alpha.1, alpha.2, or alpha.3
release assets. Their source tags are now protected by the tag ruleset, but their
assets remain mutable. GitHub requires republishing to make those releases
immutable. That migration needs separate authorization; do not delete/recreate,
unpublish, or republish existing releases as part of normal publication.

## Retry after failure

If preparation fails after reserving a draft or pushing any tag, the version is
consumed. Never delete the reservation, overwrite a tag, use `--clobber`, or resume
with rebuilt images. Preserve the draft, image tags/digests, and failed workflow
diagnostics. Correct the cause on the supported branch, choose a **new version**,
update its references and notes, and rerun all gates. A whole-workflow rerun after
reservation is refused, as is any attempt to replace a published release.

After successful preparation, read-only verification jobs and finalization may be
retried using the **same run's immutable prepared artifact**, provided the draft ID,
source commit, image digests, and verification records still agree and no release
asset has been uploaded. Finalization refuses a draft containing any assets, so a
partial asset upload also requires a new version. This permits transient check
failures to be retried without rebuilding or replacing anything. Before any draft
or push, a whole-workflow retry is safe if all destinations still pass preflight.

## Verify and install by digest

Use a recent GitHub CLI supporting `gh attestation verify` and its
`--source-digest` flag. These checks apply to releases produced by this workflow;
earlier alphas do not contain these attestations or SBOM assets.

```bash
VERSION=v1.2.3 # replace with the intended published version
gh release download "$VERSION" --repo otelx/rio --dir release
cd release
sha256sum --check SHA256SUMS
COMMIT=$(python3 -c 'import json; print(json.load(open("release.json"))["commit"])')
OPERATOR=$(python3 -c 'import json; print(json.load(open("release.json"))["operator"])')
INSTRUMENTATION=$(python3 -c 'import json; print(json.load(open("release.json"))["instrumentation"])')
gh attestation verify "rio-$VERSION.tar.gz" --repo otelx/rio \
  --signer-workflow otelx/rio/.github/workflows/ci.yml --source-digest "$COMMIT"
gh attestation verify "rio-$VERSION.tar.gz" --repo otelx/rio \
  --signer-workflow otelx/rio/.github/workflows/ci.yml --source-digest "$COMMIT" \
  --predicate-type https://spdx.dev/Document/v2.3
# The CLI's OCI verifier requires registry authentication, even for public images.
docker login ghcr.io
gh attestation verify "oci://$OPERATOR" --repo otelx/rio \
  --signer-workflow otelx/rio/.github/workflows/ci.yml --source-digest "$COMMIT"
gh attestation verify "oci://$INSTRUMENTATION" --repo otelx/rio \
  --signer-workflow otelx/rio/.github/workflows/ci.yml --source-digest "$COMMIT"
ARCH=amd64 # or arm64
for IMAGE in operator instrumentation; do
  REFERENCE=$(python3 -c 'import json,sys; print(json.load(open("release.json"))[sys.argv[1]+"-"+sys.argv[2]])' "$IMAGE" "$ARCH")
  gh attestation verify "oci://$REFERENCE" --repo otelx/rio \
    --signer-workflow otelx/rio/.github/workflows/ci.yml --source-digest "$COMMIT" \
    --predicate-type https://spdx.dev/Document/v2.3
done
```

The signed index provenance binds the multi-platform reference; each signed image
SBOM describes its specific architecture child manifest. Inspect `release.json`
and the common index to see that child relationship. See GitHub's
[attestation verification documentation](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

After verification, extract the source archive and follow its quickstart,
passing `"$OPERATOR"` to `scripts/install.sh` and setting the policy's `spec.image`
to `"$INSTRUMENTATION"`. Both references include `@sha256:...` and select the
appropriate architecture without depending on a mutable version tag. Digests
also work in rule image overrides. Retain the release metadata and verified
checksums with deployment records.
