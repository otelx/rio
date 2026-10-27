"""Assertions over the real Collector file exporter's OTLP JSON lines."""
import json
import re


def read_records(text):
    records = []
    for line in text.splitlines():
        if line.strip():
            records.append(json.loads(line))
    return records


def attributes(items):
    return {a['key']: next(iter(a['value'].values())) for a in items}


def items(records, signal, service):
    resource_key, scope_key, item_key = {
        'traces': ('resourceSpans', 'scopeSpans', 'spans'),
        'metrics': ('resourceMetrics', 'scopeMetrics', 'metrics'),
        'logs': ('resourceLogs', 'scopeLogs', 'logRecords'),
    }[signal]
    for record in records:
        for resource in record.get(resource_key, []):
            if attributes(resource.get('resource', {}).get('attributes', [])).get('service.name') != service:
                continue
            for scope in resource.get(scope_key, []):
                yield from scope.get(item_key, [])


def valid_id(value, length):
    return bool(re.fullmatch('[0-9a-fA-F]{%d}' % length, value or '')) and int(value, 16) != 0


def server_span(records, service, marker):
    matches = []
    for span in items(records, 'traces', service):
        attrs = attributes(span.get('attributes', []))
        if span.get('kind') not in (2, 'SPAN_KIND_SERVER'):
            continue
        url = str(attrs.get('url.full', attrs.get('http.url', attrs.get('http.target', attrs.get('url.path', '')))))
        if '/work/' + marker not in url:
            continue
        assert valid_id(span.get('traceId'), 32), span
        assert valid_id(span.get('spanId'), 16), span
        assert attrs.get('http.request.method', attrs.get('http.method')) == 'GET', attrs
        assert int(attrs.get('http.response.status_code', attrs.get('http.status_code', 0))) == 200, attrs
        assert int(span['endTimeUnixNano']) >= int(span['startTimeUnixNano']) > 0, span
        matches.append(span)
    assert len(matches) == 1, f'{service}: expected one fresh server span for {marker}, got {len(matches)}'
    return matches[0]


def log_record(records, service, marker, span=None, body_suffix=''):
    matches = [r for r in items(records, 'logs', service)
               if r.get('body', {}).get('stringValue') == 'rio-fixture ' + marker + body_suffix]
    assert len(matches) == 1, f'{service}: expected one captured log for {marker}, got {len(matches)}'
    record = matches[0]
    assert 9 <= int(record['severityNumber']) <= 12, record
    assert int(record['timeUnixNano']) > 0, record
    assert int(record.get('observedTimeUnixNano', record['timeUnixNano'])) > 0, record
    if span:
        assert record['traceId'] == span['traceId'], record
        assert record['spanId'] == span['spanId'], record
    return record


def metric_sample(records, service, spec):
    matches = [m for m in items(records, 'metrics', service) if m['name'] == spec['name']]
    assert matches, f'{service}: missing automatic metric {spec["name"]}'
    points = []
    for metric in matches:
        assert metric.get('unit', '') == spec['unit'], metric
        data = metric[spec['type']]
        if 'temporality' in spec:
            assert data.get('aggregationTemporality') == spec['temporality'], data
        for point in data.get('dataPoints', []):
            assert int(point['timeUnixNano']) > 0, point
            value = point.get('count', point.get('asInt', point.get('asDouble', 0)))
            points.append(float(value))
    assert points and max(points) > 0, points
    return points
