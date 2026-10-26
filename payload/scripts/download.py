"""Download immutable upstream release assets, verifying their recorded hashes."""
import hashlib
import json
import pathlib
import sys
import urllib.request
import zipfile

versions = json.loads(pathlib.Path(sys.argv[1]).read_text())
arch = sys.argv[2]
if arch not in ("amd64", "arm64"):
    raise SystemExit(f"Unsupported architecture: {arch}")
destination = pathlib.Path(sys.argv[3])
destination.mkdir(parents=True, exist_ok=True)


def download(component, name):
    asset = versions[component]["assets"][name]
    request = urllib.request.Request(asset["url"], headers={"User-Agent": "rio"})
    with urllib.request.urlopen(request, timeout=120) as response:
        data = response.read()
    actual = hashlib.sha256(data).hexdigest()
    if actual != asset["sha256"]:
        raise SystemExit(f"Checksum mismatch for {name}: {actual}")
    path = destination / name
    path.write_bytes(data)
    return path


download("injector", f"libotelinject_{arch}.so").rename(destination / "libotelinject.so")
download("java", "opentelemetry-javaagent.jar")
dotnet_arch = "x64" if arch == "amd64" else "arm64"
for libc in ("glibc", "musl"):
    path = download("dotnet", f"opentelemetry-dotnet-instrumentation-linux-{libc}-{dotnet_arch}.zip")
    target = destination / "dotnet" / libc
    target.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(path) as archive:
        for item in archive.infolist():
            if not (target / item.filename).resolve().is_relative_to(target.resolve()):
                raise SystemExit(f"Unsafe archive member: {item.filename}")
        archive.extractall(target)
    path.unlink()
