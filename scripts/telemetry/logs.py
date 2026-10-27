"""Automatic logging-library capture scenarios over the shared admission harness.

Applications emit ordinary logging-library messages. Only the shipped agents
may bridge them to OTLP; the Collector has no container-log receiver.
"""
import json
import re
import time
import uuid

from telemetry.assertions import log_record, metric_sample, server_span


def captured_log(records, service, marker, span, started_ns):
    """Reject stale, duplicated, misattributed, and incorrectly correlated logs."""
    record = log_record(records, service, marker, span)
    # Kubernetes nodes share the host clock. Allow five seconds of skew while
    # requiring a current event; the UUID body and fresh service also isolate it.
    assert int(record['timeUnixNano']) >= started_ns - 5_000_000_000, record
    assert int(record['timeUnixNano']) <= time.time_ns() + 5_000_000_000, record
    return record


def application_logged(harness, name, marker):
    text = harness.kube('logs', '-n', harness.namespace, 'deployment/' + name, '-c', 'app')
    (harness.current / (name + '-application.log')).write_text(text)
    matches = re.findall(re.escape('rio-fixture ' + marker) + r'(?![\w.-])', text)
    assert len(matches) == 1, f'{name}: normal application logging emitted {len(matches)} copies for {marker}'


def other_signals(harness, name, marker, spec):
    span = harness.poll(lambda: server_span(harness.records('traces'), name, marker))
    metric_spec = spec.get('metrics')
    if metric_spec:
        harness.poll(lambda: metric_sample(harness.records('metrics'), name, metric_spec))
    (harness.current / (name + '-signal-independence.json')).write_text(json.dumps({
        'tracesVerified': True, 'metricsVerified': bool(metric_spec),
        'metricsBoundary': None if metric_spec else
            'No automatic metric source supplied by this agent; metrics independence remains unvalidated under #22',
    }, indent=2))
    return span


def run(harness):
    """Exercise inherited signals, overrides in both directions, and bare apps.

    Missing upstream capture support is an explicit failure, so this scenario
    cannot turn a configuration-only or documentation-only change into support.
    """
    for language in harness.args.languages:
        spec = harness.config[language]
        assert spec.get('logs'), f'{language}: no automatic logging-library integration specified'
        for libc in harness.args.libcs:
            harness.current = harness.out / f'logs-{language}-{libc}'
            harness.current.mkdir(parents=True, exist_ok=True)
            base = f'logs-{language}-{libc}'
            harness.kube('delete', 'instrumentations.rio.otelx.github.io', 'cluster',
                         '--ignore-not-found', '--wait=true', '--timeout=180s')
            bare = harness.deploy(language, libc, base + '-bare', False)
            marker = uuid.uuid4().hex
            harness.request(bare, marker)
            harness.poll(lambda: application_logged(harness, bare, marker))
            harness.assert_silent(bare, ('traces', 'metrics', 'logs'))
            harness.save_apps()
            harness.kube('delete', 'deployment', bare, '-n', harness.namespace, '--wait=true')

            cases = [
                ('inherit-on', True, None, True),
                ('override-on', False, {'logs': True}, True),
                ('inherit-off', False, None, False),
                ('override-off', True, {'logs': False}, False),
            ]
            for suffix, inherited_logs, rule_signals, enabled in cases:
                harness.policy({'traces': True, 'metrics': True, 'logs': inherited_logs},
                               rule_signals=rule_signals)
                name = harness.deploy(language, libc, base + '-' + suffix, True)
                marker = uuid.uuid4().hex
                started_ns = time.time_ns()
                harness.request(name, marker)
                harness.poll(lambda: application_logged(harness, name, marker))
                span = other_signals(harness, name, marker, spec)
                if enabled:
                    correlation = span if spec['logs'].get('correlation') else None
                    harness.poll(lambda: captured_log(harness.records('logs'), name, marker,
                                                      correlation, started_ns))
                    # Keep checking after the initial successful export. Another
                    # processor or a delayed duplicate batch must fail the case.
                    deadline = time.monotonic() + 4
                    while time.monotonic() < deadline:
                        captured_log(harness.records('logs'), name, marker, correlation, started_ns)
                        time.sleep(1)
                else:
                    harness.assert_silent(name, ('logs',), duration=8)
                print(f'PASS automatic log capture {suffix}: '
                      f'{language}/{libc}/{harness.args.arch}/{harness.args.delivery}', flush=True)
                harness.save_apps()
                harness.kube('delete', 'deployment', name, '-n', harness.namespace, '--wait=true')
