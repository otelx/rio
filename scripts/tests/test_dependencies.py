"""Keep dependency inputs and version metadata consistent with image locks."""

import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class NodeVersionTests(unittest.TestCase):
    def test_reported_agent_versions_match_installed_lock(self):
        versions = json.loads((ROOT / 'payload/versions.json').read_text())
        lock = json.loads((ROOT / 'payload/node/package-lock.json').read_text())
        for metadata, package in (
            ('node', '@opentelemetry/auto-instrumentations-node'),
            ('nodeWinstonTransport', '@opentelemetry/winston-transport'),
        ):
            with self.subTest(package=package):
                self.assertEqual(versions[metadata],
                                 lock['packages'][f'node_modules/{package}']['version'])


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
