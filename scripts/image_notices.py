"""Inventory and preserve license/notice files from a final image filesystem.

This is a redistribution inventory, not a license-compliance certification.
Embedded native dependencies still require the review described in THIRD_PARTY.md.
"""

import hashlib
import io
import json
import re
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from pathlib import Path, PurePosixPath


def notice_name(name):
    return bool(re.search(r"(^|[-_.])(licen[cs]es?|notices?|copyright|copying)([-_.]|$)", name, re.I))


def collect(archive, output):
    inventory = []

    def save(name, data):
        path = PurePosixPath(name)
        if path.is_absolute() or ".." in path.parts:
            raise ValueError(f"Unsafe notice path: {name}")
        target = output / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        inventory.append({"path": name, "sha256": hashlib.sha256(data).hexdigest()})

    for member in archive:
        if not member.isfile():
            continue
        name = member.name.removeprefix("./")
        if notice_name(PurePosixPath(name).name) or name.startswith("licenses/"):
            save(name, archive.extractfile(member).read())
        elif name.endswith(".jar"):
            with zipfile.ZipFile(io.BytesIO(archive.extractfile(member).read())) as jar:
                for item in jar.infolist():
                    if not item.is_dir() and notice_name(PurePosixPath(item.filename).name):
                        save(f"{name}.notices/{item.filename}", jar.read(item))
        elif name == "lib/apk/db/installed":
            save("base-packages.txt", archive.extractfile(member).read())
    if not any(p["path"].startswith("licenses/") for p in inventory):
        raise ValueError("Image is missing /licenses")
    (output / "inventory.json").write_text(json.dumps(sorted(inventory, key=lambda x: x["path"]), indent=2) + "\n")


def main():
    image, destination = sys.argv[1:]
    output = Path(destination)
    output.mkdir(parents=True, exist_ok=False)
    container = subprocess.check_output(["docker", "create", "--entrypoint", "/unused", image], text=True).strip()
    try:
        with tempfile.TemporaryFile() as filesystem:
            subprocess.run(["docker", "export", container], stdout=filesystem, check=True)
            filesystem.seek(0)
            with tarfile.open(fileobj=filesystem) as archive:
                collect(archive, output)
    finally:
        subprocess.run(["docker", "rm", container], check=True, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
