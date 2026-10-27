"""Refresh source inputs from pinned package-origin commits. Requires gh."""

import argparse
import concurrent.futures
import hashlib
import json
import os
import re
import subprocess
import tempfile
import urllib.parse
from pathlib import Path

from alpine_sources import check_recipe, download, packages_from_database, validate, verify


def create_lock(database, base_image, cache):
    packages = packages_from_database(database)
    release = next(package["V"] for package in packages if package["P"] == "alpine-release")
    branch = re.match(r"[0-9]+\.[0-9]+", release)[0]
    cache = Path(cache)
    cache.mkdir(parents=True, exist_ok=True)

    def collect(pair):
        name, commit = pair
        if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.+-]*", name) or not re.fullmatch(r"[a-f0-9]{40}", commit):
            raise ValueError("Invalid package-origin identity")
        directory = cache / (name + "-" + commit)
        directory.mkdir(exist_ok=True)
        listing = json.loads(subprocess.check_output([
            "gh", "api", "--hostname", "github.com",
            f"repos/alpinelinux/aports/contents/main/{name}?ref={commit}",
        ]))
        files = {}
        for entry in listing:
            filename = entry["name"]
            if entry["type"] != "file" or not re.fullmatch(r"[a-zA-Z0-9_@.+-]+", filename) or filename in (".", ".."):
                raise ValueError("Packaging directory contains an unsupported entry")
            url = entry["download_url"]
            if not url.startswith(f"https://raw.githubusercontent.com/alpinelinux/aports/{commit}/main/{name}/"):
                raise ValueError("Packaging URL does not match the pinned origin")
            target = directory / filename
            if target.is_symlink():
                raise ValueError("Refusing a symlink in the source cache")
            data = target.read_bytes() if target.exists() else download(url)
            item = {"name": filename, "url": url, "sha256": hashlib.sha256(data).hexdigest(), "git_blob": entry["sha"]}
            verify(item, data)
            target.write_bytes(data)
            files[filename] = item
        recipe = (directory / "APKBUILD").read_text()
        checksums = re.search(r'^sha512sums="(.*?)"', recipe, re.M | re.S)
        if checksums:
            for line in checksums[1].splitlines():
                if not line.strip():
                    continue
                row = re.fullmatch(r"([a-f0-9]{128})\s+([a-zA-Z0-9_@.+-]+)", line.strip())
                if not row or row[2] in (".", ".."):
                    raise ValueError("Unsupported source checksum entry")
                checksum, filename = row.groups()
                target = directory / filename
                if filename not in files:
                    url = f"https://distfiles.alpinelinux.org/distfiles/v{branch}/" + urllib.parse.quote(filename)
                    if target.is_symlink():
                        raise ValueError("Refusing a symlink in the source cache")
                    data = target.read_bytes() if target.exists() else download(url)
                    files[filename] = {"name": filename, "url": url, "sha256": hashlib.sha256(data).hexdigest()}
                    target.write_bytes(data)
                files[filename]["aports_sha512"] = checksum
                verify(files[filename], target.read_bytes())
        origin = {"origin": name, "commit": commit, "files": [files[key] for key in sorted(files)]}
        versions = {package["V"] for package in packages if (package["o"], package["c"]) == pair}
        check_recipe(directory, origin, versions)
        return origin

    pairs = sorted({(package["o"], package["c"]) for package in packages})
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        origins = list(pool.map(collect, pairs))
    lock = {"schema": 1, "base_image": base_image, "packages": packages, "origins": origins}
    validate(lock, database, base_image)
    return lock


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("database", type=Path)
    parser.add_argument("base_image")
    parser.add_argument("output", type=Path)
    parser.add_argument("--cache", type=Path, required=True)
    args = parser.parse_args()
    lock = create_lock(args.database.read_text(), args.base_image, args.cache)
    # Keep the previous lock intact on network, metadata, or checksum failure.
    args.output.parent.mkdir(parents=True, exist_ok=True)
    temporary_name = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", dir=args.output.parent, delete=False) as temporary:
            temporary_name = temporary.name
            json.dump(lock, temporary, indent=2)
            temporary.write("\n")
        os.replace(temporary_name, args.output)
    finally:
        if temporary_name is not None:
            Path(temporary_name).unlink(missing_ok=True)


if __name__ == "__main__":
    main()
