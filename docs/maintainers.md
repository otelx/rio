# Maintainer access and release authority

RIO executes instrumentation inside application containers. Keep repository access, release authority, and credential handling limited to the people and automation that need them. Review these controls before a stable release and whenever maintainer membership or the release workflow changes. [Issue #4](https://github.com/otelx/rio/issues/4) tracks completion of the initial configuration.

## Protected development history

The active [`main-reviewed-ci` ruleset](https://github.com/otelx/rio/rules/25984807) requires pull requests, resolved review conversations, and successful GitHub Actions checks on an up-to-date branch. It blocks deletion and force pushes, including for administrators, with no configured bypass actors. Required checks are Go and source checks, both Build and load agents jobs, all four Integration jobs, and Verify multi-platform image selection. The checks are bound to the GitHub Actions application (ID 15368), so another status provider cannot satisfy them.

The initial access review on October 27, 2026 found one writable maintainer: `MrAlias` (administrator). `grcevski` has read access, organization base permission is read, and there are no repository teams, outside collaborators, deploy keys, or Actions repository secrets. With one writable maintainer, the initial policy requires a PR and CI with zero approving reviews; it does not claim independent approval. The maintainer must inspect the diff, CI results, and security implications before merging. When a second trusted reviewer receives write access, require at least one approving review and approval of the most recent push. Do not grant access merely to satisfy a review count.

Repository administrators can edit repository rules and are the exceptional recovery authority. No standing bypass is configured for `main` or tag immutability. Record any necessary recovery in a public issue (with confidential incident details reported privately), including reason, actor, scope, and before/after settings. Restore and reverify the documented protections immediately. Do not disable rules to expedite a release or merge failing changes.

## Release tags and publication

The [`release-tag-creation` ruleset](https://github.com/otelx/rio/rules/25984679) restricts creation of `v*` tags to repository administrators (repository role ID 5). At the initial review, that means `MrAlias`. Other contributors propose releases through PRs. The separate [`release-tag-immutability` ruleset](https://github.com/otelx/rio/rules/25984680) prevents updating, force pushing, or deleting any `v*` tag with no bypass, including for the tag creator. Correct a bad release with a new version; preserve its original source tag.

Merge release changes through the protected PR process, wait for the exact merged commit's CI push run on `main` to succeed, then create a new SemVer release tag on that commit. Stable and prerelease versions follow the [publication process](releases.md). Do not tag a feature branch, a PR merge ref, or an untested commit. Release candidates must include `scripts/check-release-source.sh` and its workflow gate; historical tags and their old workflows predate this policy and must not be rerun to republish images. The source guard checks the fetched remote tag, workflow SHA, checkout, ancestry of remote `main`, a merged PR targeting `otelx/rio`'s `main` whose resulting merge commit is the exact release commit, and a successful CI push run on `main` for that exact commit. This rejects historical direct pushes even if they passed CI. It runs before registry authentication and again inside `scripts/release.sh` before publication. A failed lookup or missing successful run blocks publication. A tag build alone cannot satisfy the source check.

Publication uses the job-scoped `GITHUB_TOKEN`: `actions: read` for CI evidence, `pull-requests: read` for merged-PR evidence, `contents: write` for the prerelease, and `packages: write` for the two supported image packages. Other jobs and the repository default remain read-only; Actions cannot approve PRs. Do not add a long-lived publication PAT. The publication script uses only `ghcr.io/otelx/rio/operator` and `ghcr.io/otelx/rio/instrumentation`, or the corresponding names in CI's disposable registry. It must never push the retired `ghcr.io/otelx/rio` or `ghcr.io/otelx/rio-payload` packages. No stable release or `latest` tag is authorized by the current alpha workflow.

## Privileged access and packages

Organization owners must enable **Require two-factor authentication for everyone in your organization** in [otelx Authentication security](https://github.com/organizations/otelx/settings/security), after checking members, service accounts, and outside collaborators. GitHub documents the [enforcement and access effects](https://docs.github.com/en/organizations/keeping-your-organization-secure/managing-two-factor-authentication-for-your-organization/requiring-two-factor-authentication-in-your-organization). Existing member 2FA enrollment alone does not enforce future access. Recheck that the organization API returns `two_factor_requirement_enabled: true`.

For **both** [`rio/operator`](https://github.com/orgs/otelx/packages/container/rio%2Foperator/settings) and [`rio/instrumentation`](https://github.com/orgs/otelx/packages/container/rio%2Finstrumentation/settings), verify the source repository is `otelx/rio`, repository permission inheritance is enabled, only the intended maintainers have administrative access, and only `otelx/rio` has Actions write access. Remove unnecessary explicit users, teams, and repository grants after reviewing them with the owner; do not expand privileges as a workaround. Preserve public read access and verify anonymous release pulls. Check the organization's complete container package inventory to confirm `rio` and `rio-payload` remain absent. Review installed GitHub Apps and organization tokens with package/repository write access in the owner settings, including expiry and least privilege.

The October 27 configuration session could review repository collaborators, teams, deploy keys, and repository secrets, but package API calls returned HTTP 403 because its OAuth token lacked `read:packages`. Package permission inheritance, extra package grants, and the authenticated inventory of retired package names remain unverified. The organization still returned `two_factor_requirement_enabled: false`; its enforcement requires an owner session in the web settings. Do not mark issue #4 resolved until these owner checks and the supported-branch source gate are verified.

## Credentials and vulnerability reports

Repository secret scanning and secret-scanning push protection must remain enabled. Preserve [private vulnerability reporting](../SECURITY.md) and the read-only default Actions token. Never put credentials, scan matches, kubeconfigs, or private application data in public issues or CI logs.

For an exposed credential, revoke or rotate it at its issuer immediately, including credentials blocked by push protection if they have escaped a trusted environment. Privately notify the maintainer through a security advisory, determine its permissions and exposure window, inspect provider and GitHub audit logs for use, and update affected deployments or Actions secrets. Remove the credential from current source and coordinate any history cleanup with the owner; deleting a file does not revoke access. Close the scanning alert only after revocation and investigation, recording sanitized evidence. Bypass push protection only for a verified non-secret or test fixture with a documented reason; never bypass for a live credential.

## Verify effective settings

Expected ruleset payloads are versioned under [`.github/rulesets/`](../.github/rulesets/). They describe the intended configuration; committing them does not apply settings. Query effective GitHub state after changes and retain sanitized evidence in the tracking issue. Repository branch-protection endpoints alone do not enumerate ruleset enforcement; use the rules API as well.

```bash
gh api 'repos/otelx/rio/rulesets?includes_parents=true'
gh api repos/otelx/rio/rules/branches/main
gh api repos/otelx/rio/rulesets/25984807
gh api repos/otelx/rio/rulesets/25984679
gh api repos/otelx/rio/rulesets/25984680
gh api repos/otelx/rio --jq '.security_and_analysis'
gh api repos/otelx/rio/actions/permissions/workflow
gh api repos/otelx/rio/private-vulnerability-reporting
gh api orgs/otelx --jq '{two_factor_requirement_enabled,default_repository_permission}'
gh api repos/otelx/rio/collaborators --paginate --jq '.[] | {login,role_name}'
gh api repos/otelx/rio/teams --paginate
gh api repos/otelx/rio/keys --paginate --jq '.[] | {id,title,read_only}'
gh api 'orgs/otelx/packages?package_type=container' --paginate --jq '.[] | {name,visibility,repository:.repository.full_name}'
```

Confirm all three rulesets are active with the intended ref patterns and bypass actors, all eight required checks are present with strict enforcement, and secret-scanning statuses are `enabled`. Confirm private reporting remains enabled, workflow defaults remain read-only with PR approvals disabled, and 2FA is enforced. Use an owner session with package access for the package inventory and web permission review; an HTTP 403 or anonymous pull cannot prove package write permissions or absence. Record the reviewed access list, package grants, date, and reviewer without credential values.
