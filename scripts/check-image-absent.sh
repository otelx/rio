#!/usr/bin/env bash
# Fail closed: only the registry's explicit manifest-not-found response is absence.
set -Eeuo pipefail
[[ $# -gt 0 ]] || { printf 'Usage: scripts/check-image-absent.sh IMAGE...\n' >&2; exit 2; }
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
for reference in "$@"; do
  if docker buildx imagetools inspect "$reference" > "$temporary/result" 2>&1; then
    printf 'Refusing to replace published image %s\n' "$reference" >&2
    exit 1
  fi
  # Do not confuse authentication, transport, rate limiting, or server failures
  # with an unused tag. No write is permitted after an ambiguous response.
  if ! grep -Eiq '(^|[ :])manifest unknown([ :]|$)|^ERROR: [^[:space:]]+: not found$' "$temporary/result"; then
    printf 'Cannot establish that image is absent: %s\n' "$reference" >&2
    cat "$temporary/result" >&2
    exit 1
  fi
done
