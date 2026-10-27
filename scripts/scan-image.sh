#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/scan-image.sh operator|instrumentation IMAGE OUTPUT_DIRECTORY\n'
  exit 0
fi
[[ $# -eq 3 && ( $1 == operator || $1 == instrumentation ) ]] || {
  printf 'Usage: scripts/scan-image.sh operator|instrumentation IMAGE OUTPUT_DIRECTORY\n' >&2; exit 2;
}
export PATH="$root/.artifacts/bin:$PATH"
for required in docker trivy python3 govulncheck; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s (run scripts/install-security-tools.sh)\n' "$required" >&2; exit 1; }
done
role=$1
image=$2
mkdir -p -- "$3"
out=$(cd -- "$3" && pwd)
container=''
trap 'if [[ -n $container ]]; then docker rm "$container" >/dev/null; fi' EXIT
trap 'printf "Image scan failed at line %s\n" "$LINENO" >&2' ERR
docker image inspect "$image" > "$out/image.json"
arch=$(docker image inspect "$image" --format '{{.Architecture}}')
[[ $arch == amd64 || $arch == arm64 ]] || { printf 'Unsupported image architecture\n' >&2; exit 1; }
govulncheck -version > "$out/govulncheck-version.txt"
# Keep every severity and the full inventory. Exceptions are evaluated below;
# neither scanner ignore files nor --ignore-unfixed may hide evidence.
trivy image --image-src docker --cache-dir "$root/.artifacts/trivy-cache" --scanners vuln --pkg-types os,library \
  --ignorefile '' --list-all-pkgs --format json --output "$out/image-scan.json" "$image"
status=0
python3 "$root/scripts/security-gate.py" "$out/image-scan.json" "$role" "$root/security/exceptions.json" || status=1
container=$(docker create "$image")
if [[ $role == operator ]]; then
  docker cp "$container:/operator" "$out/operator"
  govulncheck -mode=binary "$out/operator" > "$out/govulncheck-binary.txt" 2>&1 || status=1
  cat "$out/govulncheck-binary.txt"
else
  mkdir -p "$out/dotnet"
  docker cp "$container:/dist/agents/dotnet/." "$out/dotnet/"
  docker cp "$container:/dist/versions.json" "$out/versions.json"
  mkdir -p "$out/java"
  docker cp "$container:/dist/agents/java/." "$out/java/"
  python3 "$root/scripts/java-sbom.py" "$out/java" "$out/versions.json" "$out/java.spdx.json"
  trivy sbom --cache-dir "$root/.artifacts/trivy-cache" --ignorefile '' --list-all-pkgs \
    --format json --output "$out/java-scan.json" "$out/java.spdx.json"
  python3 "$root/scripts/security-gate.py" "$out/java-scan.json" java "$root/security/exceptions.json" || status=1
  python3 "$root/scripts/dotnet-sbom.py" "$out/dotnet" "$out/versions.json" "$arch" "$out/dotnet.spdx.json"
  trivy sbom --cache-dir "$root/.artifacts/trivy-cache" --ignorefile '' --list-all-pkgs \
    --format json --output "$out/dotnet-scan.json" "$out/dotnet.spdx.json"
  python3 "$root/scripts/security-gate.py" "$out/dotnet-scan.json" dotnet "$root/security/exceptions.json" || status=1
fi
trivy --version > "$out/trivy-version.txt"
exit "$status"
