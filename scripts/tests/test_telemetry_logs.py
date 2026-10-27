"""False-positive guards for the real Collector log assertions."""
import copy
from pathlib import Path
import sys
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from telemetry.logs import captured_log


class CapturedLogTests(unittest.TestCase):
    def setUp(self):
        self.started_ns = time.time_ns()
        self.span = {'traceId': 'a' * 32, 'spanId': 'b' * 16}
        self.record = {'body': {'stringValue': 'rio-fixture current'}, 'severityNumber': 9,
                       'timeUnixNano': str(self.started_ns), 'observedTimeUnixNano': str(self.started_ns),
                       **self.span}
        self.records = [{'resourceLogs': [{'resource': {'attributes': [
            {'key': 'service.name', 'value': {'stringValue': 'application'}}]},
            'scopeLogs': [{'logRecords': [self.record]}]}]}]

    def test_accepts_current_correlated_normal_library_log(self):
        self.assertEqual(captured_log(self.records, 'application', 'current', self.span, self.started_ns), self.record)

    def test_rejects_missing_export_wrong_marker_and_resource(self):
        for records, service, marker in [(self.records, 'unrelated', 'current'),
                                         (self.records, 'application', 'stale'), ([], 'application', 'current')]:
            with self.assertRaises(AssertionError):
                captured_log(records, service, marker, self.span, self.started_ns)

    def test_rejects_wrong_severity_stale_or_future_time_and_wrong_correlation(self):
        for key, value in [('severityNumber', 5), ('timeUnixNano', '0'),
                           ('timeUnixNano', str(self.started_ns - 6_000_000_000)),
                           ('timeUnixNano', str(self.started_ns + 60_000_000_000)),
                           ('observedTimeUnixNano', '0'), ('traceId', 'c' * 32), ('spanId', 'd' * 16)]:
            records = copy.deepcopy(self.records)
            records[0]['resourceLogs'][0]['scopeLogs'][0]['logRecords'][0][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(AssertionError):
                captured_log(records, 'application', 'current', self.span, self.started_ns)

    def test_rejects_duplicate_in_later_export_batch(self):
        # A later batch containing the same event must fail even when the first
        # batch by itself previously satisfied a polling assertion.
        captured_log(self.records, 'application', 'current', self.span, self.started_ns)
        self.records.extend(copy.deepcopy(self.records))
        with self.assertRaises(AssertionError):
            captured_log(self.records, 'application', 'current', self.span, self.started_ns)

    def test_declared_newline_body_is_exact_without_normalization(self):
        # Ruby Logger captures its normal formatted body, including the CRI
        # line terminator. Missing, repeated, or different terminators must fail.
        for suffix in ('', '\n\n', '\r\n', ' '):
            self.record['body']['stringValue'] = 'rio-fixture current' + suffix
            with self.subTest(suffix=suffix), self.assertRaises(AssertionError):
                captured_log(self.records, 'application', 'current', self.span,
                             self.started_ns, body_suffix='\n')
        self.record['body']['stringValue'] = 'rio-fixture current\n'
        self.assertEqual(captured_log(self.records, 'application', 'current', self.span,
                                     self.started_ns, body_suffix='\n'), self.record)
        with self.assertRaises(AssertionError):
            captured_log(self.records, 'application', 'current', self.span, self.started_ns)
        self.records.extend(copy.deepcopy(self.records))
        with self.assertRaises(AssertionError):
            captured_log(self.records, 'application', 'current', self.span,
                         self.started_ns, body_suffix='\n')

    def test_allows_no_correlation_only_when_library_does_not_claim_it(self):
        del self.record['traceId']
        del self.record['spanId']
        self.assertEqual(captured_log(self.records, 'application', 'current', None, self.started_ns), self.record)


if __name__ == '__main__':
    unittest.main()
