"""Validate release versions and render notes from the tagged changelog."""

import argparse
import json
import re
from pathlib import Path


def version_info(version):
    number = r"(?:0|[1-9][0-9]*)"
    identifier = r"[0-9A-Za-z-]+"
    pattern = rf"v{number}\.{number}\.{number}(?:-({identifier}(?:\.{identifier})*))?(?:\+({identifier}(?:\.{identifier})*))?"
    match = re.fullmatch(pattern, version)
    if not match:
        raise ValueError(f"Invalid SemVer release tag: {version!r}")
    prerelease = match[1]
    if prerelease and any(p.isdigit() and len(p) > 1 and p[0] == "0" for p in prerelease.split(".")):
        raise ValueError("Numeric prerelease identifiers must not contain leading zeros")
    # '+' is not a container tag character. '_' cannot occur in SemVer, so this
    # mapping is reversible and cannot collide with another accepted version.
    image_tag = version.replace("+", "_")
    if len(image_tag) > 120:
        raise ValueError("Version is too long for the architecture container tags")
    return {"version": version, "image_tag": image_tag, "prerelease": bool(prerelease)}


def release_notes(changelog, version):
    lines = changelog.splitlines()
    heading = f"## {version}"
    if lines.count(heading) != 1:
        raise ValueError(f"Expected exactly one changelog section named {heading!r}")
    start = lines.index(heading) + 1
    end = next((i for i in range(start, len(lines)) if lines[i].startswith("## ")), len(lines))
    notes = "\n".join(lines[start:end]).strip()
    if not notes:
        raise ValueError("Release changelog section is empty")
    return notes


def validate_references(root, version):
    image_tag = version_info(version)["image_tag"]
    for name, image in (
        ("scripts/install.sh", "operator"),
        ("config/install.yaml", "operator"),
        ("config/example.yaml", "instrumentation"),
        ("examples/quickstart/policy.yaml", "instrumentation"),
        ("docs/configuration.md", "instrumentation"),
    ):
        reference = f"ghcr.io/otelx/rio/{image}:{image_tag}"
        # A stable version must not match a prerelease or an architecture suffix.
        if not re.search(re.escape(reference) + r"(?![0-9A-Za-z_.+-])", (root / name).read_text()):
            raise ValueError(f"Exact release image reference missing from {name}: {reference}")
    checkout = r"--branch\s+(['\"]?)" + re.escape(version) + r"\1(?=\s)"
    if not re.search(checkout, (root / "docs/quickstart.md").read_text()):
        raise ValueError("Quickstart must clone the exact release source tag")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("--changelog", type=Path)
    parser.add_argument("--root", type=Path, help="Check exact installation and quickstart references")
    args = parser.parse_args()
    info = version_info(args.version)
    if args.changelog:
        info["notes"] = release_notes(args.changelog.read_text(), args.version)
    if args.root:
        validate_references(args.root, args.version)
    print(json.dumps(info))


if __name__ == "__main__":
    main()
