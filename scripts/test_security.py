"""Regression checks for incomplete scans and the exception boundary."""
import datetime
import importlib.util
import hashlib
import json
import pathlib
import re
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("security_gate", ROOT / "scripts/security-gate.py")
GATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)
DOTNET_SPEC = importlib.util.spec_from_file_location("dotnet_sbom", ROOT / "scripts/dotnet-sbom.py")
DOTNET = importlib.util.module_from_spec(DOTNET_SPEC)
DOTNET_SPEC.loader.exec_module(DOTNET)
OWNED_SPEC = importlib.util.spec_from_file_location("dotnet_inventory", ROOT / "payload/scripts/dotnet_inventory.py")
OWNED = importlib.util.module_from_spec(OWNED_SPEC)
OWNED_SPEC.loader.exec_module(OWNED)


class SecurityGateTest(unittest.TestCase):
    def setUp(self):
        self.report = {"SchemaVersion": 2, "Results": [{"Type": "gobinary", "Target": "operator", "Packages": [{"Name": "stdlib"}], "Vulnerabilities": []}]}
        self.finding = {"VulnerabilityID": "CVE-test", "PkgName": "stdlib", "InstalledVersion": "1.0", "Severity": "HIGH"}
        self.today = datetime.date(2026, 10, 27)
        self.exception = dict(role="operator", target="operator", id="CVE-test", package="stdlib", version="1.0", owner="MrAlias", reason="Test applicability evidence", issue="https://github.com/otelx/rio/issues/3", reviewed="2026-10-27", expires="2026-11-03")

    def check(self, exceptions=None):
        return GATE.check(self.report, "operator", exceptions or [], self.today)

    def test_clean_report(self):
        self.assertEqual([], self.check())

    def test_missing_inventory_is_not_clean(self):
        for report in ({}, {"SchemaVersion": 2, "Results": []}, {"SchemaVersion": 2, "Results": [{"Type": "gobinary", "Packages": []}]}):
            with self.subTest(report=report), self.assertRaises(ValueError):
                GATE.check(report, "operator", [])

    def test_each_payload_ecosystem_is_required(self):
        results = [{"Type": kind, "Packages": [{}]} for kind in ("alpine", "node-pkg", "python-pkg", "gemspec")]
        for index in range(len(results)):
            with self.subTest(index=index), self.assertRaises(ValueError):
                GATE.check({"SchemaVersion": 2, "Results": results[:index] + results[index + 1:]}, "instrumentation", [])
        with self.assertRaises(ValueError):
            GATE.check({"SchemaVersion": 2, "Results": results}, "dotnet", [])
        with self.assertRaises(ValueError):
            GATE.check({"SchemaVersion": 2, "Results": results}, "java", [])

    def test_unfixed_and_unknown_findings_block(self):
        for severity in ("HIGH", "CRITICAL", "UNKNOWN"):
            finding = dict(self.finding, Severity=severity)
            self.report["Results"][0]["Vulnerabilities"] = [finding]
            self.assertTrue(self.check())

    def test_exception_must_match_target_package_and_version(self):
        self.report["Results"][0]["Vulnerabilities"] = [self.finding]
        self.assertEqual([], self.check([self.exception]))
        for key in ("role", "target", "id", "package", "version"):
            exception = dict(self.exception, **{key: "other"})
            self.assertTrue(self.check([exception]))

    def test_expired_future_long_and_incomplete_exceptions_fail(self):
        for change in ({"expires": "2026-10-27"}, {"reviewed": "2026-10-28"}, {"expires": "2026-12-01"}, {"owner": ""}, {"issue": "untracked"}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.check([dict(self.exception, **change)])

    def test_builder_and_ci_use_same_patch_release(self):
        version = re.search(r"^go (\S+)$", (ROOT / "go.mod").read_text(), re.M)[1]
        builder = re.search(r"golang:([^@]+)@sha256:[a-f0-9]{64}", (ROOT / "Dockerfile").read_text())[1]
        self.assertEqual(f"{version}-bookworm", builder)
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        self.assertIn("go-version-file: go.mod", workflow)

    def test_dotnet_inventory_must_cover_both_libcs_and_actual_dlls(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            packages = []
            for libc in ("glibc", "musl"):
                (directory / libc).mkdir()
                (directory / libc / "agent.dll").write_bytes(b"fixture")
                packages.append({"sourceInfo": f"release: /linux-{libc}-x64/agent.dll", "externalRefs": [{"referenceLocator": "pkg:nuget/agent@1.0"}]})
            sbom = {"packages": packages}
            self.assertEqual(packages, DOTNET.select_packages(sbom, directory, "amd64"))
            with self.assertRaises(ValueError):
                DOTNET.select_packages(sbom, directory, "arm64")
            (directory / "musl" / "unindexed.dll").write_bytes(b"fixture")
            with self.assertRaises(ValueError):
                DOTNET.select_packages(sbom, directory, "amd64")

    def test_java_inventory_pin_matches_current_agent(self):
        import json
        java = json.loads((ROOT / "payload/versions.json").read_text())["java"]
        manifest = json.loads((ROOT / "security/java-sboms.json").read_text())[java["version"]]
        self.assertEqual(java["assets"]["opentelemetry-javaagent.jar"]["sha256"], manifest["jarSha256"])

    def test_owned_dotnet_sources_outputs_and_original_hook_fail_closed(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp) / "source"
            compiled = pathlib.Path(temp) / "compiled"
            distribution = pathlib.Path(temp) / "distribution"
            for name in OWNED.INPUTS:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b"reviewed source")
            versions = {"dotnet": {"version": "fixture"}, "dotnetRioPropagation": {"sdkImage": "pinned-sdk"}}
            (root / "Dockerfile").write_text("FROM --platform=$BUILDPLATFORM pinned-sdk AS dotnet-propagation\n")
            for name in ("dotnet/Rio.Propagation.csproj", "dotnet/bootstrap/Rio.Bootstrap.csproj"):
                (root / name).write_text("<Project />")
            upstream = {"upstream": versions["dotnet"], "assemblies": {}}
            for final, build in OWNED.OUTPUTS.items():
                path = compiled / build
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b"compiled project")
                for libc in ("glibc", "musl"):
                    path = distribution / libc / final
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(b"compiled project")
            for libc in ("glibc", "musl"):
                for final, original in OWNED.RELOCATIONS.items():
                    path = distribution / libc / final
                    path.write_bytes(b"upstream bytes")
                    upstream["assemblies"][libc + "/" + original] = {"sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            legal = root / "licenses/dotnet/managed/inventory.json"
            legal.parent.mkdir(parents=True)
            legal.write_text(json.dumps(upstream))
            manifest = OWNED.generate(root, compiled, versions)
            self.assertEqual(set(OWNED.OUTPUTS), OWNED.verify(manifest, distribution, root, versions)[0])
            mismatched = json.loads(json.dumps(versions))
            mismatched["dotnetRioPropagation"]["sdkImage"] = "different-sdk"
            with self.assertRaisesRegex(ValueError, "SDK image"):
                OWNED.generate(root, compiled, mismatched)
            project = root / "dotnet/Rio.Propagation.csproj"
            project.write_text('<Project><ItemGroup><PackageReference Include="unreviewed" /></ItemGroup></Project>')
            with self.assertRaisesRegex(ValueError, "package dependency"):
                OWNED.generate(root, compiled, versions)
            project.write_text("<Project />")
            first_source = root / OWNED.INPUTS[0]
            source_before = first_source.read_bytes()
            first_source.write_bytes(b"unreviewed change")
            with self.assertRaisesRegex(ValueError, "source inputs"):
                OWNED.verify(manifest, distribution, root, versions)
            first_source.write_bytes(source_before)
            first_dll = distribution / "glibc" / next(iter(OWNED.OUTPUTS))
            first_dll.write_bytes(b"tampered project")
            with self.assertRaisesRegex(ValueError, "assembly bytes changed"):
                OWNED.verify(manifest, distribution, root, versions)
            first_dll.write_bytes(b"compiled project")
            hook = distribution / "glibc" / next(iter(OWNED.RELOCATIONS))
            hook.write_bytes(b"tampered upstream")
            with self.assertRaisesRegex(ValueError, "upstream .NET assembly bytes"):
                OWNED.verify(manifest, distribution, root, versions)


if __name__ == "__main__":
    unittest.main()
