"""Match the upstream release inventory to the DLLs extracted from the final image."""
import hashlib
import json
import pathlib
import sys
import urllib.request


def inventory(directory, image_versions, arch, output):
    pins = json.loads((pathlib.Path(__file__).resolve().parents[1] / "payload/versions.json").read_text())["dotnet"]
    shipped = json.loads(image_versions.read_text())["dotnet"]
    # Older releases lack the SBOM, but must have the same checksum-pinned binaries.
    if shipped["version"] != pins["version"] or any(
        shipped["assets"].get(name) != asset
        for name, asset in pins["assets"].items() if name.endswith(".zip")
    ):
        raise ValueError("Final .NET payload differs from the reviewed inventory pins")
    asset = pins["assets"]["sbom.spdx.json"]
    sbom_path = directory / "sbom.spdx.json"
    if not sbom_path.exists():
        request = urllib.request.Request(asset["url"], headers={"User-Agent": "rio-security"})
        with urllib.request.urlopen(request, timeout=120) as response:
            sbom_path.write_bytes(response.read())
    data = sbom_path.read_bytes()
    if hashlib.sha256(data).hexdigest() != asset["sha256"]:
        raise ValueError("Upstream .NET SBOM checksum mismatch")
    sbom = json.loads(data)
    sbom["packages"] = select_packages(sbom, directory, arch)
    sbom["files"] = []
    sbom["relationships"] = []
    output.write_text(json.dumps(sbom, indent=2) + "\n")


def select_packages(sbom, directory, arch):
    selected = []
    for libc in ("glibc", "musl"):
        prefix = f"/linux-{libc}-{'x64' if arch == 'amd64' else 'arm64'}/"
        packages = [p for p in sbom["packages"] if prefix in p.get("sourceInfo", "")]
        paths = {p["sourceInfo"].split(prefix, 1)[1] for p in packages}
        dlls = {p.relative_to(directory / libc).as_posix() for p in (directory / libc).rglob("*.dll")}
        if not dlls or dlls != paths:
            raise ValueError(f"Incomplete .NET {libc} inventory: missing={dlls - paths}, extra={paths - dlls}")
        if any(not any(ref.get("referenceLocator", "").startswith("pkg:nuget/")
                       for ref in p.get("externalRefs", [])) for p in packages):
            raise ValueError("Missing NuGet identity in .NET inventory")
        selected.extend(packages)
        print(f"Matched {len(dlls)} .NET DLLs ({libc}, {arch})")
    # Keep only packages present in this architecture's final image. The upstream
    # inventory also includes macOS, Windows, and other Linux architectures.
    return selected


if __name__ == "__main__":
    if len(sys.argv) != 5 or sys.argv[3] not in ("amd64", "arm64"):
        raise SystemExit("Usage: dotnet-sbom.py DOTNET_DIRECTORY IMAGE_VERSIONS ARCH OUTPUT")
    inventory(pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3], pathlib.Path(sys.argv[4]))
