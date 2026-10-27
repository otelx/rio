"""Automatic HTTP metrics scenarios using the shared admission/Collector harness."""

import json
import math
import uuid

from telemetry.assertions import items, log_record, server_span


# The pinned Rack instrumentation creates traces only. Merely loading the Ruby
# metrics SDK/exporter does not install an automatic application measurement.
BLOCKED = {
    'ruby': {
        'reason': 'Bundled Rack 0.30.0 has no automatic HTTP metric instruments; '
                  'the Ruby bundle contains no automatic runtime metric source.',
        'sources': [
            'https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1129',
            'https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1213',
            'https://github.com/open-telemetry/opentelemetry-ruby-contrib/tree/'
            'opentelemetry-instrumentation-rack/v0.30.0/instrumentation/rack/lib',
            'https://github.com/open-telemetry/opentelemetry-ruby-instrumentation/'
            'blob/68af0a92d6d9c2cd0ee38a9327e98797673227d1/'
            'lib/opentelemetry-auto-instrumentation.rb',
        ],
        'remaining': 'Implement or bundle automatic Rack HTTP duration '
                     'instrumentation (or a real automatic runtime source), '
                     'then validate sample changes and all signal overrides '
                     'through admission on the complete delivery/arch/libc matrix. '
                     'Keep https://github.com/otelx/rio/issues/22 open.',
    }
}


def histogram_snapshot(records, service, spec):
    """Return latest cumulative count per time series, not summed exports.

    A Collector file contains multiple exports of the same cumulative data.
    Summing those exports would make an idle application appear active.
    """
    assert spec['type'] == 'histogram', spec
    assert spec.get('temporality') == 2, spec
    latest = {}
    for metric in items(records, 'metrics', service):
        if metric['name'] != spec['name']:
            continue
        assert metric.get('unit') == spec['unit'], metric
        data = metric['histogram']
        assert data['aggregationTemporality'] in (2, 'AGGREGATION_TEMPORALITY_CUMULATIVE'), data
        for point in data.get('dataPoints', []):
            timestamp = int(point['timeUnixNano'])
            start = int(point.get('startTimeUnixNano', 0))
            count = int(point['count'])
            assert timestamp > 0 and timestamp >= start and count >= 0, point
            assert math.isfinite(float(point.get('sum', 0))) and float(point.get('sum', 0)) >= 0, point
            buckets = point.get('bucketCounts', [])
            bounds = point.get('explicitBounds', [])
            if buckets:
                assert len(buckets) == len(bounds) + 1, point
                assert sum(int(b) for b in buckets) == count, point
                assert all(int(b) >= 0 for b in buckets), point
            assert bounds == sorted(bounds) and all(math.isfinite(float(b)) for b in bounds), point
            # Attribute order in OTLP is unspecified. A new start time denotes
            # a reset and must not be combined with the preceding process.
            attributes = sorted(point.get('attributes', []), key=lambda a: a['key'])
            key = json.dumps(attributes, sort_keys=True)
            candidate = (timestamp, start, count)
            if key not in latest or timestamp > latest[key][0]:
                latest[key] = candidate
            elif timestamp == latest[key][0]:
                assert candidate == latest[key], 'conflicting cumulative samples'
    assert latest, f'{service}: missing automatic histogram {spec["name"]}'
    assert sum(p[2] for p in latest.values()) > 0, latest
    return latest


def activity_increase(records, service, spec, before, requests):
    after = histogram_snapshot(records, service, spec)
    # Exporting the same sample again, changing service identity, resetting a
    # counter, or receiving a point from another fixture cannot satisfy activity.
    assert before.keys() <= after.keys(), (before, after)
    for key, previous in before.items():
        current = after[key]
        assert current[1] == previous[1], 'histogram start time changed'
        assert current[2] >= previous[2], 'cumulative histogram count decreased'
    assert max(p[0] for p in after.values()) > max(p[0] for p in before.values()), after
    assert sum(p[2] for p in after.values()) >= sum(p[2] for p in before.values()) + requests, (before, after)
    return after


def _request_signals(harness, name):
    marker = uuid.uuid4().hex
    harness.request(name, marker)
    span = harness.poll(lambda: server_span(harness.records('traces'), name, marker))
    harness.poll(lambda: log_record(harness.records('logs'), name, marker, span))


def _save_apps(harness):
    # Older shared-harness revisions gather these only in final diagnostics.
    if hasattr(harness, 'save_apps'):
        harness.save_apps()


def run(harness):
    """Run implemented cells and return explicit unresolved language coverage.

    The caller must report returned blockers; a successful subset cannot close
    #22. Shared setup, matrix selection, diagnostics and CI remain caller-owned.
    """
    coverage = {}
    for lang in harness.args.languages:
        if lang in BLOCKED:
            coverage[lang] = {'status': 'blocked', **BLOCKED[lang]}
            print(f'BLOCKED automatic metrics: {lang}: {BLOCKED[lang]["reason"]}', flush=True)
            continue
        spec = harness.config[lang]['metrics']
        coverage[lang] = {'status': 'pending', 'source': spec, 'cells': []}
        for libc in harness.args.libcs:
            harness.current = harness.out / f'metrics-{lang}-{libc}'
            harness.current.mkdir(exist_ok=True)
            _save_apps(harness)
            harness.kube('delete', 'deployment', '--all', '-n', harness.namespace, '--wait=true')
            harness.kube('delete', 'instrumentations.rio.otelx.github.io', 'cluster',
                         '--ignore-not-found', '--wait=true', '--timeout=180s')
            suffix = uuid.uuid4().hex[:6]
            name = harness.deploy(lang, libc, f'{lang}-{libc}-metrics-bare-{suffix}', False)
            harness.request(name, uuid.uuid4().hex)
            harness.assert_silent(name, ('traces', 'metrics', 'logs'))
            _save_apps(harness)
            harness.kube('delete', 'deployment', name, '-n', harness.namespace, '--wait=true')
            for case, inherited, override, enabled in (
                ('inherited-on', True, None, True),
                ('rule-on', False, {'metrics': True}, True),
                ('inherited-off', False, None, False),
                ('rule-off', True, {'metrics': False}, False),
            ):
                harness.policy({'traces': True, 'metrics': inherited, 'logs': True},
                               rule_signals=override,
                               env={'OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE': 'cumulative',
                                    'OTEL_METRIC_EXPORT_INTERVAL': '1000'})
                name = harness.deploy(lang, libc, f'{lang}-{libc}-{case}-{suffix}', True)
                _request_signals(harness, name)
                if enabled:
                    before = harness.poll(lambda: histogram_snapshot(harness.records('metrics'), name, spec))
                    for _ in range(3):
                        harness.request(name, uuid.uuid4().hex)
                    after = harness.poll(lambda: activity_increase(harness.records('metrics'), name, spec, before, 3))
                    (harness.current / f'{case}-activity.json').write_text(json.dumps({
                        'service': name, 'before': before, 'after': after, 'requests': 3}, indent=2))
                else:
                    harness.assert_silent(name, ('metrics',), duration=8)
                _save_apps(harness)
                harness.kube('delete', 'deployment', name, '-n', harness.namespace, '--wait=true')
                print(f'PASS automatic metrics {case}: {lang}/{libc}/'
                      f'{harness.args.arch}/{harness.args.delivery}', flush=True)
            coverage[lang]['cells'].append({'libc': libc, 'arch': harness.args.arch,
                                           'delivery': harness.args.delivery, 'status': 'passed'})
        coverage[lang]['status'] = 'passed-selected-cells'
    (harness.out / 'metrics-coverage.json').write_text(json.dumps(coverage, indent=2))
    return {lang: value for lang, value in coverage.items() if value['status'] == 'blocked'}
