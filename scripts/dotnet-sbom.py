"""Match the upstream release inventory to the DLLs extracted from the final image."""
import hashlib
import importlib.util
import json
import pathlib
import sys
import urllib.request


def inventory(directory, image_versions, arch, output):
    root = pathlib.Path(__file__).resolve().parents[1]
    versions = json.loads((root / "payload/versions.json").read_text())
    pins = versions["dotnet"]
    image = json.loads(image_versions.read_text())
    shipped = image["dotnet"]
    # Older releases lack the SBOM, but must have the same checksum-pinned binaries.
    if shipped["version"] != pins["version"] or any(
        shipped["assets"].get(name) != asset
        for name, asset in pins["assets"].items() if name.endswith(".zip")
    ):
        raise ValueError("Final .NET payload differs from the reviewed inventory pins")
    asset = json.loads((root / "security/dotnet-sboms.json").read_text())[shipped["version"]]
    sbom_path = directory / "sbom.spdx.json"
    if not sbom_path.exists():
        request = urllib.request.Request(asset["url"], headers={"User-Agent": "rio-security"})
        with urllib.request.urlopen(request, timeout=120) as response:
            sbom_path.write_bytes(response.read())
    data = sbom_path.read_bytes()
    if hashlib.sha256(data).hexdigest() != asset["sha256"]:
        raise ValueError("Upstream .NET SBOM checksum mismatch")
    sbom = json.loads(data)
    owned, relocations = set(), {}
    if "dotnetRioPropagation" in image:
        if image["dotnetRioPropagation"] != versions["dotnetRioPropagation"]:
            raise ValueError("Final RIO .NET component metadata differs from reviewed inputs")
        spec = importlib.util.spec_from_file_location("dotnet_inventory", root / "payload/scripts/dotnet_inventory.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        manifest = json.loads((directory / "rio-inventory.json").read_text())
        owned, relocations = module.verify(manifest, directory, root / "payload", versions)
        print(f"Verified {len(owned)} RIO-owned .NET assemblies per libc against source/SDK inventory")
    sbom["packages"] = select_packages(sbom, directory, arch, owned, relocations)
    sbom["files"] = []
    sbom["relationships"] = []
    output.write_text(json.dumps(sbom, indent=2) + "\n")


def select_packages(sbom, directory, arch, owned=(), relocations=None):
    selected = []
    for libc in ("glibc", "musl"):
        prefix = f"/linux-{libc}-{'x64' if arch == 'amd64' else 'arm64'}/"
        packages = [p for p in sbom["packages"] if prefix in p.get("sourceInfo", "")]
        paths = {p["sourceInfo"].split(prefix, 1)[1] for p in packages}
        # The original hook retains its upstream bytes under a new filename.
        for final, upstream in (relocations or {}).items():
            if upstream not in paths:
                raise ValueError("Relocated upstream hook is missing from the NuGet inventory")
            paths.remove(upstream)
            paths.add(final)
        paths.update(owned)
        dlls = {p.relative_to(directory / libc).as_posix() for p in (directory / libc).rglob("*")
                if p.is_file() and p.suffix.lower() == ".dll"}
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
