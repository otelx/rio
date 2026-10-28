"""Regression checks for false-positive activity assertions over OTLP exports."""
import copy
import sys
from pathlib import Path
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from telemetry.metrics import activity_increase, histogram_snapshot


SPEC = {'name': 'http.server.request.duration', 'type': 'histogram', 'unit': 's', 'temporality': 2}


def record(count=1, timestamp=100, service='fixture', start=1):
    return {'resourceMetrics': [{
        'resource': {'attributes': [{'key': 'service.name', 'value': {'stringValue': service}}]},
        'scopeMetrics': [{'metrics': [{'name': SPEC['name'], 'unit': 's', 'histogram': {
            'aggregationTemporality': 2, 'dataPoints': [{
                'timeUnixNano': str(timestamp), 'startTimeUnixNano': str(start),
                'count': str(count), 'sum': .01 * count,
                'bucketCounts': [str(count), '0'], 'explicitBounds': [.5],
                'attributes': [{'key': 'http.request.method', 'value': {'stringValue': 'GET'}}],
            }],
        }}]}],
    }]}


class MetricActivityTest(unittest.TestCase):
    def setUp(self):
        self.initial = [record()]
        self.before = histogram_snapshot(self.initial, 'fixture', SPEC)

    def test_actual_request_count_increase(self):
        actual = activity_increase(self.initial + [record(4, 200)], 'fixture', SPEC, self.before, 3)
        self.assertEqual(sum(point[2] for point in actual.values()), 4)

    def test_reexporting_cumulative_samples_is_not_activity(self):
        # A naive sum over exports would report 4, even with no new requests.
        with self.assertRaises(AssertionError):
            activity_increase(self.initial * 4, 'fixture', SPEC, self.before, 3)
        with self.assertRaises(AssertionError):
            activity_increase(self.initial + [record(1, 200)], 'fixture', SPEC, self.before, 3)

    def test_other_fixture_or_disabled_export_cannot_pass(self):
        for records in ([], [record(10, 200, service='other')], self.initial + [record(10, 200, service='other')]):
            with self.subTest(records=records), self.assertRaises(AssertionError):
                activity_increase(records, 'fixture', SPEC, self.before, 3)

    def test_reset_does_not_hide_incorrect_ancestry_of_sample(self):
        with self.assertRaises(AssertionError):
            activity_increase(self.initial + [record(10, 200, start=150)], 'fixture', SPEC, self.before, 3)

    def test_malformed_data_does_not_pass(self):
        mutations = [
            lambda m: m.update(unit='ms'),
            lambda m: m['histogram'].update(aggregationTemporality=1),
            lambda m: m['histogram']['dataPoints'][0].update(bucketCounts=['0', '0']),
            lambda m: m['histogram']['dataPoints'][0].update(sum=float('nan')),
            lambda m: m['histogram']['dataPoints'][0].update(timeUnixNano='0'),
        ]
        for mutation in mutations:
            value = copy.deepcopy(record())
            mutation(value['resourceMetrics'][0]['scopeMetrics'][0]['metrics'][0])
            with self.subTest(mutation=mutation), self.assertRaises(AssertionError):
                histogram_snapshot([value], 'fixture', SPEC)


if __name__ == '__main__':
    unittest.main()
