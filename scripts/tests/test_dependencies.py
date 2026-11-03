"""Keep the Python inputs consistent with the lockfile used by image builds."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class PythonRequirementsTests(unittest.TestCase):
    def test_pinned_inputs_match_installed_lock(self):
        inputs = ROOT / 'payload/python/requirements.in'
        lock = ROOT / 'payload/python/requirements.lock'
        pins = dict(re.findall(r'^([\w.-]+)==([^\s;\\]+)',
                               lock.read_text(), re.MULTILINE))
        for package, version in re.findall(r'^([\w.-]+)==([^\s;\\]+)',
                                           inputs.read_text(), re.MULTILINE):
            with self.subTest(package=package):
                self.assertEqual(pins.get(package), version,
                                 'Regenerate payload/python/requirements.lock')


if __name__ == '__main__':
    unittest.main()
