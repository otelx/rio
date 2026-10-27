"""Propagation assertions must reject shared IDs with broken ancestry/headers."""
import copy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from telemetry.propagation import check_headers, relationship


class PropagationAssertions(unittest.TestCase):
    def setUp(self):
        self.trace = '1' * 32
        def span(kind, marker, ident, parent):
            return {'kind': kind, 'traceId': self.trace, 'spanId': ident * 16, 'parentSpanId': parent * 16,
                    'startTimeUnixNano': '100', 'endTimeUnixNano': '200', 'attributes': [
                        {'key': key, 'value': {'stringValue': value}} for key, value in {
                            'url.path': '/work/' + marker, 'http.request.method': 'GET',
                            'http.response.status_code': '200'}.items()]}
        self.caller = span(2, 'first', '2', '3')
        self.client = span(3, 'second', '4', '2')
        self.callee = span(2, 'second', '5', '4')
        self.records = [{'resourceSpans': [
            {'resource': {'attributes': [{'key': 'service.name', 'value': {'stringValue': service}}]},
             'scopeSpans': [{'spans': spans}]} for service, spans in [
                ('caller', [self.caller, self.client]), ('callee', [self.callee])]]}]

    def test_valid_remote_parent_and_client_server_ancestry(self):
        self.assertEqual(relationship(self.records, 'caller', 'callee', 'first', 'second',
                                     (self.trace, '3' * 16)), (self.caller, self.client, self.callee))

    def test_shared_trace_cannot_hide_wrong_parentage(self):
        self.callee['parentSpanId'] = '6' * 16
        with self.assertRaises(AssertionError):
            relationship(self.records, 'caller', 'callee', 'first', 'second')

    def test_baggage_and_traceparent_duplicates_fail(self):
        headers = {'traceparent': [f'00-{self.trace}-{self.client["spanId"]}-01'],
                   'tracestate': ['rio=state'], 'baggage': ['rio_marker=first']}
        body = {'downstream': {'headers': headers}}
        check_headers(body, self.trace, self.client, 'rio=state', {'rio_marker=first'})
        for mutation in ({'traceparent': headers['traceparent'] * 2},
                         {'baggage': ['rio_marker=first,rio_marker=first']}):
            changed = copy.deepcopy(body)
            changed['downstream']['headers'].update(mutation)
            with self.assertRaises(AssertionError):
                check_headers(changed, self.trace, self.client, 'rio=state', {'rio_marker=first'})

    def test_disabled_baggage_requires_header_absence(self):
        body = {'downstream': {'headers': {'traceparent': [f'00-{self.trace}-{self.client["spanId"]}-01'],
                                         'tracestate': ['rio=state'], 'baggage': ['']}}}
        with self.assertRaises(AssertionError):
            check_headers(body, self.trace, self.client, 'rio=state', set())
