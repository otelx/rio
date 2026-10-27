"""Missing or changed assemblies and notice texts invalidate coverage."""

import copy
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location(
    "managed_notices", ROOT / "payload/scripts/check_managed_notices.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ManagedNoticesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.materials = self.root / "notices"
        self.distribution = self.root / "agents"
        self.materials.mkdir()
        self.distribution.mkdir()
        (self.materials / "LICENSE.txt").write_bytes(b"upstream license")
        (self.distribution / "library.dll").write_bytes(b"reviewed assembly")
        self.versions = {"dotnet": {"version": "1.0", "assets": {"pinned": "asset"}}}
        self.inventory = {
            "schema": 1, "upstream": self.versions["dotnet"],
            "materials": {"LICENSE.txt": {
                "sha256": hashlib.sha256(b"upstream license").hexdigest(),
                "sources": [{"url": "https://upstream.example/LICENSE"}]}},
            "packages": {"library/1.0": {"materials": ["LICENSE.txt"]}},
            "source_project": {"materials": ["LICENSE.txt"]},
            "assemblies": {"library.dll": {
                "package": "library/1.0",
                "sha256": hashlib.sha256(b"reviewed assembly").hexdigest()}},
        }

    def check(self, inventory=None):
        return module.check(inventory or self.inventory, self.materials,
                            self.distribution, self.versions)

    def test_matching_assembly_and_material_are_covered(self):
        self.assertEqual(self.check(), 1)

    def test_added_removed_or_modified_dll_invalidates_coverage(self):
        extra = self.distribution / "new.DLL"
        extra.write_bytes(b"unreviewed")
        with self.assertRaisesRegex(ValueError, "coverage changed"):
            self.check()
        extra.unlink()
        original = self.distribution / "library.dll"
        original.unlink()
        with self.assertRaisesRegex(ValueError, "coverage changed"):
            self.check()
        original.write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError, "differs from reviewed"):
            self.check()

    def test_changed_assets_or_material_fail_closed(self):
        stale = copy.deepcopy(self.inventory)
        stale["upstream"]["assets"]["pinned"] = "different asset"
        with self.assertRaisesRegex(ValueError, "pinned .NET assets"):
            self.check(stale)
        (self.materials / "LICENSE.txt").write_bytes(b"changed license")
        with self.assertRaisesRegex(ValueError, "Modified notice"):
            self.check()

    def test_missing_material_or_package_mapping_cannot_pass(self):
        absent = copy.deepcopy(self.inventory)
        absent["packages"]["library/1.0"]["materials"] = []
        with self.assertRaisesRegex(ValueError, "no preserved material"):
            self.check(absent)
        absent = copy.deepcopy(self.inventory)
        absent["assemblies"]["library.dll"]["package"] = "unmapped"
        with self.assertRaisesRegex(ValueError, "Unmapped managed DLL"):
            self.check(absent)

    def test_paths_and_symlinks_cannot_escape_the_inventory(self):
        for name in ("../outside", "/outside", "..\\outside"):
            with self.assertRaisesRegex(ValueError, "Unsafe inventory"):
                module.child(self.materials, name)
        outside = self.root / "outside"
        outside.write_bytes(b"outside")
        (self.materials / "link").symlink_to(outside)
        with self.assertRaisesRegex(ValueError, "escapes its root"):
            module.child(self.materials, "link")


if __name__ == "__main__":
    unittest.main()
