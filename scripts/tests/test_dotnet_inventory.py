"""RIO .NET builds reject compiler or framework images outside their inventory."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2] / "payload"
spec = importlib.util.spec_from_file_location("dotnet_inventory", ROOT / "scripts/dotnet_inventory.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class DotnetInventoryTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.compiled = Path(temporary.name)
        for name in module.OUTPUTS.values():
            path = self.compiled / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"compiled assembly")
        self.versions = json.loads((ROOT / "versions.json").read_text())

    def test_inventory_records_both_sdk_images(self):
        manifest = module.generate(ROOT, self.compiled, self.versions)
        self.assertEqual(manifest["component"], self.versions["dotnetRioPropagation"])
        self.assertIn("net8SdkImage", manifest["component"])
        self.assertEqual(set(manifest["assemblies"]), set(module.OUTPUTS))

    def test_unreviewed_compiler_or_framework_image_is_rejected(self):
        for field in ("sdkImage", "net8SdkImage"):
            changed = copy.deepcopy(self.versions)
            changed["dotnetRioPropagation"][field] = "unreviewed:latest"
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "differs from its reviewed pin"):
                module.generate(ROOT, self.compiled, changed)


if __name__ == "__main__":
    unittest.main()
