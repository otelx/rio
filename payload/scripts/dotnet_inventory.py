"""Bind RIO-owned .NET build outputs to the reviewed source and SDK inputs.

The upstream NuGet inventory remains separate. These projects have no package
downloads: the plugin references the already inventoried agent assemblies, and
framework references are supplied by the application's .NET runtime.
"""
import argparse
import hashlib
import json
from pathlib import Path
import xml.etree.ElementTree as ET


INPUTS = (
    "Dockerfile",
    "dotnet/NuGet.Config", "dotnet/Rio.Propagation.csproj",
    "dotnet/PropagationPlugin.cs", "dotnet/bootstrap/Rio.Bootstrap.csproj",
    "dotnet/bootstrap/StartupHook.cs", "scripts/dotnet_inventory.py",
)
OUTPUTS = {
    "net/Rio.Propagation.dll": "plugin/Rio.Propagation.dll",
    "net/OpenTelemetry.AutoInstrumentation.StartupHook.dll": "bootstrap/Rio.Bootstrap.dll",
}
RELOCATIONS = {
    "net/OpenTelemetry.AutoInstrumentation.StartupHook.original.dll":
        "net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sources(root):
    return {name: digest(root / name) for name in INPUTS}


def generate(root, compiled, versions):
    sdk = versions["dotnetRioPropagation"]["sdkImage"]
    if f"FROM --platform=$BUILDPLATFORM {sdk} AS dotnet-propagation" not in (root / "Dockerfile").read_text().splitlines():
        raise ValueError("RIO .NET SDK image differs from its reviewed pin")
    net8_sdk = versions["dotnetRioPropagation"]["net8SdkImage"]
    if f"FROM --platform=$BUILDPLATFORM {net8_sdk} AS dotnet-net8" not in (root / "Dockerfile").read_text().splitlines():
        raise ValueError("RIO .NET 8 SDK image differs from its reviewed pin")
    for project in ("dotnet/Rio.Propagation.csproj", "dotnet/bootstrap/Rio.Bootstrap.csproj"):
        if any(element.tag.rsplit("}", 1)[-1] == "PackageReference" for element in ET.parse(root / project).iter()):
            raise ValueError("New RIO .NET package dependency needs inventory review")
    return {
        "schema": 1,
        "component": versions["dotnetRioPropagation"],
        "sources": sources(root),
        "assemblies": {name: digest(compiled / build_name) for name, build_name in OUTPUTS.items()},
        "upstream_relocations": RELOCATIONS,
    }


def verify(manifest, directory, root, versions):
    if manifest.get("schema") != 1 or manifest.get("component") != versions.get("dotnetRioPropagation"):
        raise ValueError("RIO .NET build inputs differ from reviewed metadata")
    if manifest.get("sources") != sources(root):
        raise ValueError("RIO .NET source inputs differ from reviewed source")
    # Revalidate the Docker SDK pin and the absence of additional package inputs.
    sdk = versions["dotnetRioPropagation"]["sdkImage"]
    if f"FROM --platform=$BUILDPLATFORM {sdk} AS dotnet-propagation" not in (root / "Dockerfile").read_text().splitlines():
        raise ValueError("RIO .NET SDK image differs from its reviewed pin")
    net8_sdk = versions["dotnetRioPropagation"]["net8SdkImage"]
    if f"FROM --platform=$BUILDPLATFORM {net8_sdk} AS dotnet-net8" not in (root / "Dockerfile").read_text().splitlines():
        raise ValueError("RIO .NET 8 SDK image differs from its reviewed pin")
    if set(manifest.get("assemblies", {})) != set(OUTPUTS) or manifest.get("upstream_relocations") != RELOCATIONS:
        raise ValueError("Unexpected RIO .NET assembly or upstream relocation")
    for libc in ("glibc", "musl"):
        for name, expected in manifest["assemblies"].items():
            if digest(directory / libc / name) != expected:
                raise ValueError(f"RIO .NET assembly bytes changed: {libc}/{name}")
    upstream = json.loads((root / "licenses/dotnet/managed/inventory.json").read_text())
    if upstream["upstream"] != versions["dotnet"]:
        raise ValueError("Upstream .NET byte inventory differs from reviewed asset pins")
    inverse = {original: final for final, original in RELOCATIONS.items()}
    for name, assembly in upstream["assemblies"].items():
        libc, relative = name.split("/", 1)
        final = directory / libc / inverse.get(relative, relative)
        if digest(final) != assembly["sha256"]:
            raise ValueError(f"Final upstream .NET assembly bytes changed: {name}")
    return set(OUTPUTS), RELOCATIONS


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source_root", type=Path)
    parser.add_argument("compiled", type=Path)
    parser.add_argument("versions", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    result = generate(args.source_root, args.compiled, json.loads(args.versions.read_text()))
    args.output.write_text(json.dumps(result, indent=2) + "\n")
