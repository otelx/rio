"""Scan the shaded Java agent using its checksum-pinned upstream inventory."""
import hashlib
import io
import json
import pathlib
import sys
import urllib.request
import zipfile


def inventory(directory, image_versions, output):
    manifests = json.loads((pathlib.Path(__file__).resolve().parents[1] / "security/java-sboms.json").read_text())
    shipped = json.loads(image_versions.read_text())["java"]
    pin = manifests[shipped["version"]]
    jar = directory / "opentelemetry-javaagent.jar"
    if hashlib.sha256(jar.read_bytes()).hexdigest() != pin["jarSha256"]:
        raise ValueError("Final Java agent differs from the reviewed SBOM input")
    request = urllib.request.Request(pin["url"], headers={"User-Agent": "rio-security"})
    with urllib.request.urlopen(request, timeout=120) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != pin["sha256"]:
        raise ValueError("Java SBOM archive checksum mismatch")
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        sbom = archive.read("sboms/opentelemetry-javaagent.spdx.json")
    document = json.loads(sbom)
    if not document.get("packages") or document["packages"][0].get("versionInfo") != shipped["version"]:
        raise ValueError("Java inventory version mismatch")
    output.write_bytes(sbom)
    print(f"Verified Java agent {shipped['version']} and its {len(document['packages'])}-package upstream inventory")


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("Usage: java-sbom.py JAVA_DIRECTORY IMAGE_VERSIONS OUTPUT")
    inventory(pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]))
