"""Bind preserved NuGet notices to every managed DLL in the pinned distribution.

This checks inventory coverage and byte identity; it does not approve native
dependencies or make the separate redistribution review unnecessary.
"""

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath


def child(root, name):
    path = PurePosixPath(name)
    if not name or path.is_absolute() or ".." in path.parts or "\\" in name:
        raise ValueError(f"Unsafe inventory path: {name}")
    target = root / name
    if not target.resolve().is_relative_to(root.resolve()):
        raise ValueError(f"Inventory path escapes its root: {name}")
    return target


def check(inventory, material_root, distribution, versions):
    if inventory.get("schema") != 1 or inventory["upstream"] != versions["dotnet"]:
        raise ValueError("Managed notice inventory does not match the pinned .NET assets")
    materials = inventory["materials"]
    for name, item in materials.items():
        data = child(material_root, name).read_bytes()
        if hashlib.sha256(data).hexdigest() != item["sha256"]:
            raise ValueError(f"Modified notice material: {name}")
        if not item.get("sources"):
            raise ValueError(f"Notice material has no source: {name}")
    packages = {**inventory["packages"], "source-project": inventory["source_project"]}
    for name, package in packages.items():
        if not package.get("materials") or any(n not in materials for n in package["materials"]):
            raise ValueError(f"Package has no preserved material: {name}")
    expected = inventory["assemblies"]
    actual = {p.relative_to(distribution).as_posix() for p in distribution.rglob("*")
              if p.is_file() and p.suffix.lower() == ".dll"}
    if not expected or actual != set(expected):
        raise ValueError(f"Managed DLL coverage changed: added={sorted(actual-set(expected))}, "
                         f"removed={sorted(set(expected)-actual)}")
    for name, assembly in expected.items():
        if assembly["package"] not in packages:
            raise ValueError(f"Unmapped managed DLL: {name}")
        if hashlib.sha256(child(distribution, name).read_bytes()).hexdigest() != assembly["sha256"]:
            raise ValueError(f"Managed DLL differs from reviewed package bytes: {name}")
    return len(expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("distribution", type=Path)
    parser.add_argument("materials", type=Path)
    parser.add_argument("versions", type=Path)
    args = parser.parse_args()
    inventory = json.loads((args.materials / "inventory.json").read_text())
    count = check(inventory, args.materials, args.distribution,
                  json.loads(args.versions.read_text()))
    print(f"Verified preserved notice material for {count} managed DLLs")


if __name__ == "__main__":
    main()
