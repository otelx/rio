"""Guard against telemetry false positives; integration uses real Collector files."""
import copy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from telemetry.assertions import server_span


class TraceAssertionTests(unittest.TestCase):
    def setUp(self):
        self.span = {'kind': 2, 'traceId': '1' * 32, 'spanId': '2' * 16,
                     'startTimeUnixNano': '100', 'endTimeUnixNano': '200',
                     'attributes': [{'key': k, 'value': {'stringValue': v}} for k, v in {
                         'url.path': '/work/fresh', 'http.request.method': 'GET',
                         'http.response.status_code': '200'}.items()]}
        self.records = [{'resourceSpans': [{'resource': {'attributes': [
            {'key': 'service.name', 'value': {'stringValue': 'fixture'}}]},
            'scopeSpans': [{'spans': [self.span]}]}]}]

    def test_accepts_expected_server_span(self):
        self.assertEqual(server_span(self.records, 'fixture', 'fresh'), self.span)

    def test_rejects_stale_request_wrong_service_and_missing_export(self):
        for records, service, marker in [(self.records, 'other', 'fresh'),
                                         (self.records, 'fixture', 'stale'), ([], 'fixture', 'fresh')]:
            with self.assertRaises(AssertionError):
                server_span(records, service, marker)

    def test_rejects_client_span_zero_id_duplicate_and_http_failure(self):
        for field, value in [('kind', 3), ('traceId', '0' * 32), ('spanId', '0' * 16)]:
            records = copy.deepcopy(self.records)
            records[0]['resourceSpans'][0]['scopeSpans'][0]['spans'][0][field] = value
            with self.assertRaises(AssertionError):
                server_span(records, 'fixture', 'fresh')
        self.records[0]['resourceSpans'][0]['scopeSpans'][0]['spans'].append(self.span)
        with self.assertRaises(AssertionError):
            server_span(self.records, 'fixture', 'fresh')
