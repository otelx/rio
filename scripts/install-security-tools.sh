#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  printf 'Usage: scripts/install-security-tools.sh (Linux amd64/arm64; installs into .artifacts/bin)\n'
  exit 0
fi
[[ $# -eq 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for required in curl sha256sum tar go uname mktemp; do
  command -v "$required" >/dev/null || { printf 'Missing command: %s\n' "$required" >&2; exit 1; }
done
[[ $(uname -s) == Linux ]] || { printf 'Linux required\n' >&2; exit 1; }
case $(uname -m) in
  x86_64) arch=64bit; checksum=c7cbe3137fb8a48a17574bede3dd6beb16742d1e9772929940fc90082c39d9c0 ;;
  aarch64|arm64) arch=ARM64; checksum=e174dff2a6dba5e3ae41f8d0537a6324405bd08132497340304c6b20e0f16a1f ;;
  *) printf 'Unsupported architecture\n' >&2; exit 1 ;;
esac
mkdir -p "$root/.artifacts/bin" "$root/.artifacts/tmp"
temp=$(mktemp -d "$root/.artifacts/tmp/security-tools.XXXXXX")
trap 'rm -rf -- "$temp"' EXIT
trap 'printf "Security tool installation failed at line %s\n" "$LINENO" >&2' ERR
curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/aquasecurity/trivy/releases/download/v0.76.1/trivy_0.76.1_Linux-$arch.tar.gz" -o "$temp/trivy.tar.gz"
printf '%s  %s\n' "$checksum" "$temp/trivy.tar.gz" | sha256sum --check --status
tar -xzf "$temp/trivy.tar.gz" -C "$temp" trivy
install -m 755 "$temp/trivy" "$root/.artifacts/bin/trivy"
GOBIN="$root/.artifacts/bin" go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
"$root/.artifacts/bin/trivy" --version
"$root/.artifacts/bin/govulncheck" -version
