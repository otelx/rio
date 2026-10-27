"""Require an explicit redistribution review of the current packaging inputs."""

import argparse
import hashlib
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def input_digest(root=ROOT):
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--", "Dockerfile", "go.mod", "go.sum", "cmd", "internal", "LICENSE", "NOTICE", "payload", "scripts/image_notices.py"],
        cwd=root,
    ).decode().split("\0")
    digest = hashlib.sha256()
    for name in sorted(filter(None, paths)):
        digest.update(name.encode() + b"\0" + (root / name).read_bytes() + b"\0")
    return digest.hexdigest()


def check_review(review, digest):
    if review.get("status") != "approved" or review.get("inputs_sha256") != digest:
        raise ValueError("Redistribution review is pending or stale; complete THIRD_PARTY.md for these inputs")
    if not review.get("reviewer") or not review.get("evidence"):
        raise ValueError("Redistribution approval requires a reviewer and evidence")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--digest", action="store_true", help="Print current input digest without approving it")
    args = parser.parse_args()
    digest = input_digest()
    if args.digest:
        print(digest)
        return
    check_review(json.loads((ROOT / "THIRD_PARTY_REVIEW.json").read_text()), digest)


if __name__ == "__main__":
    main()
