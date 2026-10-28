"""Bundle verified Alpine source inputs; never execute an upstream build recipe."""

import argparse
import gzip
import hashlib
import io
import json
import os
import re
import tarfile
import tempfile
import urllib.parse
import urllib.request
from pathlib import Path, PurePosixPath

FIELDS = ("P", "V", "o", "c", "L")
LIMIT = 128 * 1024 * 1024


def packages_from_database(text):
    packages = []
    for block in text.split("\n\n"):
        fields = {}
        for line in block.splitlines():
            if len(line) > 1 and line[1] == ":" and line[0] in FIELDS:
                fields[line[0]] = line[2:]
        if "P" in fields:
            if set(fields) != set(FIELDS):
                raise ValueError(f"Incomplete package source metadata: {fields['P']}")
            packages.append(fields)
    return sorted(packages, key=lambda item: item["P"])


def validate(lock, database, base_image=None):
    if lock.get("schema") != 1:
        raise ValueError("Unsupported Alpine source-lock schema")
    if base_image is not None and lock.get("base_image") != base_image:
        raise ValueError("Runtime image reference changed; reconcile the source lock")
    expected = sorted(({key: package[key] for key in FIELDS} for package in lock["packages"]), key=lambda p: p["P"])
    if not expected or packages_from_database(database) != expected:
        raise ValueError("Base-image packages changed; reconcile alpine-sources.lock.json before building")
    origins = {(package["o"], package["c"]) for package in expected}
    actual = set()
    for origin in lock["origins"]:
        key = (origin["origin"], origin["commit"])
        if key in actual or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.+-]*", key[0]) or not re.fullmatch(r"[a-f0-9]{40}", key[1]):
            raise ValueError("Invalid or repeated package origin")
        actual.add(key)
        names = set()
        for item in origin["files"]:
            name = item["name"]
            if not re.fullmatch(r"[a-zA-Z0-9_@.+-]+", name) or PurePosixPath(name).name != name or name in (".", "..") or name in names:
                raise ValueError(f"Invalid or repeated source filename: {name}")
            names.add(name)
            if not re.fullmatch(r"[a-f0-9]{64}", item["sha256"]):
                raise ValueError("Invalid source checksum")
            url = urllib.parse.urlsplit(item["url"])
            if url.scheme != "https" or url.netloc not in ("raw.githubusercontent.com", "distfiles.alpinelinux.org"):
                raise ValueError("Unapproved source host")
            if url.netloc == "raw.githubusercontent.com" and not url.path.startswith(f"/alpinelinux/aports/{key[1]}/main/{key[0]}/"):
                raise ValueError("Packaging source does not match its pinned origin commit")
            if url.netloc == "distfiles.alpinelinux.org" and not re.fullmatch(r"[a-f0-9]{128}", item.get("aports_sha512", "")):
                raise ValueError("Archive source is missing its APorts checksum")
            if item.get("git_blob") and not re.fullmatch(r"[a-f0-9]{40}", item["git_blob"]):
                raise ValueError("Invalid packaging Git blob checksum")
        if "APKBUILD" not in names:
            raise ValueError("Package origin is missing its build recipe")
    if origins != actual:
        raise ValueError("Source-lock origins do not cover the installed packages")


def download(url):
    request = urllib.request.Request(url, headers={"User-Agent": "rio-source-inventory"})
    with urllib.request.urlopen(request, timeout=90) as response:
        data = response.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise ValueError("Source input exceeds the download limit")
    return data


def verify(item, data):
    if hashlib.sha256(data).hexdigest() != item["sha256"]:
        raise ValueError(f"SHA-256 mismatch: {item['name']}")
    if item.get("aports_sha512") and hashlib.sha512(data).hexdigest() != item["aports_sha512"]:
        raise ValueError(f"APorts SHA-512 mismatch: {item['name']}")
    if item.get("git_blob"):
        digest = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        if digest != item["git_blob"]:
            raise ValueError(f"Git blob mismatch: {item['name']}")


def check_recipe(directory, origin, versions):
    text = (directory / "APKBUILD").read_text()
    version = re.search(r'^pkgver=[\"\']?([a-zA-Z0-9_.+~-]+)[\"\']?\s*(?:#.*)?$', text, re.M)
    revision = re.search(r'^pkgrel=[\"\']?([0-9]+)[\"\']?\s*(?:#.*)?$', text, re.M)
    if not version or not revision or versions != {version[1] + "-r" + revision[1]}:
        raise ValueError("Build recipe version does not match the installed packages")
    match = re.search(r'^sha512sums="(.*?)"', text, re.M | re.S)
    if not match:
        if re.search(r"^source=", text, re.M):
            raise ValueError("Recipe declares sources without supported SHA-512 checksums")
        return
    files = {item["name"]: item for item in origin["files"]}
    rows = [line for line in match[1].splitlines() if line.strip()]
    if not rows and re.search(r"^source=", text, re.M):
        raise ValueError("Source declaration has an empty checksum table")
    for line in rows:
        if not line.strip():
            continue
        entry = re.fullmatch(r"([a-f0-9]{128})\s+(\S+)", line.strip())
        if not entry or entry[2] not in files or files[entry[2]].get("aports_sha512") != entry[1]:
            raise ValueError("Source lock does not cover every recipe checksum")


def bundle(lock, database, output, fetch=download, base_image=None):
    validate(lock, database, base_image)
    output = Path(output)
    if output.exists() or output.is_symlink():
        raise FileExistsError("Refusing to replace an existing source bundle")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="rio-source-", dir=output.parent) as temporary:
        temporary = Path(temporary)
        tree = temporary / "sources"
        tree.mkdir()
        for origin in lock["origins"]:
            directory = tree / (origin["origin"] + "-" + origin["commit"])
            directory.mkdir()
            for item in origin["files"]:
                data = fetch(item["url"])
                verify(item, data)
                (directory / item["name"]).write_bytes(data)
            versions = {p["V"] for p in lock["packages"] if (p["o"], p["c"]) == (origin["origin"], origin["commit"])}
            check_recipe(directory, origin, versions)
        (tree / "SOURCE_INPUTS.json").write_text(json.dumps(lock, sort_keys=True, indent=2) + "\n")
        (tree / "README.txt").write_text(
            "RIO Alpine source inputs\n\n"
            "SOURCE_INPUTS.json binds installed package versions and source-origin commits\n"
            "to the preserved upstream archives, patches, APKBUILD recipes, and packaging\n"
            "scripts. Source archives contain their upstream license/notice material.\n"
            "Archive inputs are unchanged: consult the corresponding APKBUILD for build\n"
            "configuration and prerequisites, using the appropriate Alpine SDK and CARCH.\n"
            "This bundle does not certify bit-for-bit build reproducibility or cover the\n"
            "separately bundled language agents; see the distribution's THIRD_PARTY.md.\n"
        )
        archive = temporary / "sources.tar.gz"
        with archive.open("wb") as raw, gzip.GzipFile(fileobj=raw, mode="wb", filename="", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as tar:
                for path in sorted(tree.rglob("*")):
                    if not path.is_file():
                        continue
                    data = path.read_bytes()
                    info = tarfile.TarInfo(path.relative_to(tree).as_posix())
                    info.size = len(data)
                    info.mode = 0o644
                    tar.addfile(info, io.BytesIO(data))
        # Both paths are on the same filesystem. A hard link also refuses a
        # competing writer that created output after the initial existence check.
        os.link(archive, output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("database", type=Path)
    parser.add_argument("lock", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--base-image", required=True)
    args = parser.parse_args()
    bundle(json.loads(args.lock.read_text()), args.database.read_text(), args.output, base_image=args.base_image)


if __name__ == "__main__":
    main()
