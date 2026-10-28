"""Fail closed on incomplete inventories and untriaged blocking findings."""
import datetime
import json
import pathlib
import sys


def check(report, role, exceptions, today=None):
    today = today or datetime.datetime.now(datetime.timezone.utc).date()
    if role not in ("operator", "instrumentation", "dotnet", "java"):
        raise ValueError("Unknown image role")
    if report.get("SchemaVersion") != 2 or not isinstance(report.get("Results"), list):
        raise ValueError("Missing or unsupported Trivy report")
    required = {"operator": {"gobinary"}, "instrumentation": {"alpine", "node-pkg", "python-pkg", "gemspec"}, "dotnet": {"nuget"}, "java": {"jar"}}[role]
    found = {r["Type"] for r in report["Results"] if r.get("Packages")}
    if missing := required - found:
        raise ValueError(f"Missing package inventory for {sorted(missing)}")
    if not isinstance(exceptions, list):
        raise ValueError("Exceptions must be a list")
    approved = set()
    for exception in exceptions:
        keys = ("role", "target", "id", "package", "version", "owner", "reason", "issue", "reviewed", "expires")
        if any(not isinstance(exception.get(k), str) or not exception[k].strip() for k in keys):
            raise ValueError("Incomplete exception: " + repr(exception))
        reviewed = datetime.date.fromisoformat(exception["reviewed"])
        expires = datetime.date.fromisoformat(exception["expires"])
        if not reviewed <= today < expires or not 0 < (expires - reviewed).days <= 30:
            raise ValueError("Expired, future, or over-30-day exception: " + exception["id"])
        if not exception["issue"].startswith("https://github.com/otelx/rio/"):
            raise ValueError("Exception needs a repository issue or advisory")
        approved.add(tuple(exception[k] for k in ("role", "target", "id", "package", "version")))
    blocked = []
    for result in report["Results"]:
        for vulnerability in result.get("Vulnerabilities", []):
            key = (role, result["Target"], vulnerability["VulnerabilityID"], vulnerability["PkgName"], vulnerability["InstalledVersion"])
            # Unknown severity needs review too; never ignore unfixed findings.
            if vulnerability.get("Severity", "UNKNOWN") in ("CRITICAL", "HIGH", "UNKNOWN") and key not in approved:
                blocked.append(f"{key}: {vulnerability.get('Severity', 'UNKNOWN')}, fixed={vulnerability.get('FixedVersion', 'unavailable')}")
    return blocked


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("Usage: security-gate.py REPORT ROLE EXCEPTIONS")
    try:
        blocked = check(json.loads(pathlib.Path(sys.argv[1]).read_text()), sys.argv[2], json.loads(pathlib.Path(sys.argv[3]).read_text()))
    except (ValueError, KeyError, TypeError) as error:
        raise SystemExit(f"Invalid security evidence: {error}") from error
    if blocked:
        raise SystemExit("Blocking findings:\n" + "\n".join(blocked))
    print(f"Security gate passed ({sys.argv[2]})")
