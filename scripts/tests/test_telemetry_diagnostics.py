"""Integration failures must retain available evidence and their original error."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from telemetry_e2e import Harness


class TelemetryDiagnosticsTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.harness = object.__new__(Harness)
        self.harness.out = Path(directory.name)
        self.harness.current = self.harness.out
        self.harness.namespace = 'fixtures'

    def test_one_failed_signal_read_does_not_lose_other_collector_payloads(self):
        payload = '{"resourceMetrics": []}\n'

        def kube(*args, **kwargs):
            if args[0] == 'exec':
                if '/data/traces.json' in args[-1]:
                    raise subprocess.TimeoutExpired('kubectl', 40)
                return payload
            return '{"items": []}'

        self.harness.kube = Mock(side_effect=kube)
        with contextlib.redirect_stderr(io.StringIO()):
            self.harness.diagnostics()
        self.assertEqual((self.harness.out / 'collector-metrics.jsonl').read_text(), payload)
        self.assertEqual((self.harness.out / 'collector-logs.jsonl').read_text(), payload)
        reads = [call for call in self.harness.kube.call_args_list if call.args[0] == 'exec']
        self.assertEqual(len(reads), 3)
        self.assertTrue(all(call.kwargs['timeout'] == 40 for call in reads))

    def test_unavailable_collector_does_not_replace_original_integration_failure(self):
        def kube(*args, **kwargs):
            if args[0] == 'exec':
                raise RuntimeError('raw-exec-output')
            return '{"items": []}'

        self.harness.kube = Mock(side_effect=kube)
        errors = io.StringIO()
        with contextlib.redirect_stderr(errors), self.assertRaisesRegex(ValueError, 'original failure'):
            try:
                raise ValueError('original failure')
            finally:
                self.harness.diagnostics()
        self.assertEqual(errors.getvalue().count('RuntimeError'), 3)
        self.assertNotIn('raw-exec-output', errors.getvalue())

    def test_ruby_bootstrap_failure_cannot_pass_a_disabled_export_case(self):
        pod = {'metadata': {'name': 'ruby'}, 'status': {'containerStatuses': [{'restartCount': 0}]}}
        self.harness.kube = Mock(side_effect=[json.dumps({'items': [pod]}),
            'Auto-instrumentation failed to initialize. Error: unsupported exporter\n'])
        with self.assertRaises(AssertionError):
            self.harness.save_apps()
        self.assertIn('unsupported exporter', (self.harness.out / 'ruby.log').read_text())

    def test_normal_application_error_text_is_not_a_bootstrap_failure(self):
        pod = {'metadata': {'name': 'ruby'}, 'status': {'containerStatuses': [{'restartCount': 0}]}}
        self.harness.kube = Mock(side_effect=[json.dumps({'items': [pod]}),
            'INFO rio-fixture marker\nApplication initialized; error budget is available\n'])
        self.harness.save_apps()

    def test_dotnet_agent_failure_is_rejected_even_when_application_is_healthy(self):
        # Default fail-fast=false can leave a healthy application serving with
        # no SDK. A disabled-export case must not mistake this for suppression.
        pod = {'metadata': {'name': 'dotnet'}, 'status': {'containerStatuses': [{'restartCount': 0}]}}
        for failure in ('Error in StartupHook initialization',
                        'OpenTelemetry SDK load exception.',
                        '[rio-dotnet-bootstrap] Initialization failed: missing original hook'):
            logs = 'INFO rio-fixture marker\n' + failure + '\n'
            self.harness.kube = Mock(side_effect=[json.dumps({'items': [pod]}), logs])
            with self.subTest(failure=failure), self.assertRaises(AssertionError):
                self.harness.save_apps()
            self.assertEqual((self.harness.out / 'dotnet.log').read_text(), logs)


if __name__ == '__main__':
    unittest.main()
