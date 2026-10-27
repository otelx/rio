"""Real HTTP caller/callee W3C checks, reusing admission-created fixtures."""
import concurrent.futures
import json
import uuid

from .assertions import items, server_span, valid_id


def baggage_values(headers):
    return {part.strip() for value in headers.get('baggage', []) for part in value.split(',') if part.strip()}


def generated_traceparent(client):
    # Fresh Java roots set W3C Level 2's Random bit as well as Sampled (03).
    # https://www.w3.org/TR/trace-context-2/#trace-flags
    flags = int(client['flags']) & 0xff
    assert flags & 1 and not flags & ~3, client
    return f'00-{client["traceId"]}-{client["spanId"]}-{flags:02x}'


def fresh_root(parent, seen_trace_ids):
    assert valid_id(parent.get('traceId'), 32), parent
    assert not parent.get('parentSpanId') or int(parent['parentSpanId'], 16) == 0, parent
    assert parent['traceId'] not in seen_trace_ids, 'Contextless request reused an earlier trace ID'
    seen_trace_ids.add(parent['traceId'])


def relationship(records, caller, callee, first, second, incoming=None, state=None):
    parent = server_span(records, caller, first)
    child = server_span(records, callee, second)
    assert parent['traceId'] == child['traceId'], (parent, child)
    if incoming:
        trace, span = incoming
        assert parent['traceId'] == trace and parent['parentSpanId'] == span, parent
    clients = [s for s in items(records, 'traces', caller)
               if s.get('kind') in (3, 'SPAN_KIND_CLIENT') and s['traceId'] == parent['traceId']
               and s.get('parentSpanId') == parent['spanId'] and s['spanId'] == child.get('parentSpanId')]
    assert len(clients) == 1, (parent, child, clients)
    assert valid_id(clients[0]['spanId'], 16)
    if state is not None:
        for span in (parent, clients[0], child):
            assert span.get('traceState') == state, span
    return parent, clients[0], child


def check_headers(body, trace, client, state, baggage):
    headers = body['downstream']['headers']
    assert headers.get('traceparent') == [f'00-{trace}-{client["spanId"]}-01'], headers
    assert headers.get('tracestate') == [state], headers
    assert baggage_values(headers) == baggage, headers
    if baggage:
        assert len(headers['baggage']) == 1, headers
        assert len([part for part in headers['baggage'][0].split(',') if part.strip()]) == len(baggage), headers
    else:
        assert 'baggage' not in headers, headers


def run(h):
    languages = h.args.languages
    # Five-language ring: node -> java -> dotnet -> python -> ruby -> node.
    # A subset run exercises its corresponding ring, never implies missing pairs.
    pairs = list(zip(languages, languages[1:] + languages[:1]))
    summary = []
    seen_trace_ids = set()
    for lang in languages:
        h.config[lang].setdefault('env', {})['RIO_FIXTURE_DELAY_MS'] = '750'
    for libc in h.args.libcs:
        for enabled in (True, False):
            h.current = h.out / f'propagation-{libc}-baggage-{enabled}'
            h.current.mkdir()
            h.kube('delete', 'deployment', '--all', '-n', h.namespace, '--wait=true')
            if hasattr(h, 'reset_collector'):
                h.reset_collector()
            h.policy({'traces': True, 'metrics': False, 'logs': False}, env={
                'OTEL_PROPAGATORS': 'tracecontext,baggage' if enabled else 'tracecontext',
                'OTEL_TRACES_SAMPLER': 'parentbased_always_on'})
            names = {lang: h.deploy(lang, libc, f'prop-{lang}-{libc}-{str(enabled).lower()}', True) for lang in languages}
            for caller, callee in pairs:
                # Overlap distinct contexts and baggage; each application must preserve isolation.
                cases = [(uuid.uuid4().hex, uuid.uuid4().hex[:16], uuid.uuid4().hex, uuid.uuid4().hex) for _ in range(3)]
                seen_trace_ids.update(case[0] for case in cases)
                def request(case):
                    trace, remote, first, second = case
                    state = 'rio=' + first
                    baggage = 'rio_marker=' + first
                    next_url = f'http://{names[callee]}.{h.namespace}.svc:8080/work/{second}'
                    body = h.request(names[caller], first, {
                        'traceparent': f'00-{trace}-{remote}-01', 'tracestate': state, 'baggage': baggage}, next_url)
                    return case, state, baggage, body
                with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
                    responses = list(pool.map(request, cases))
                caller_spans = []
                for case, state, baggage, body in responses:
                    trace, remote, first, second = case
                    parent, client, child = h.poll(lambda: relationship(h.records('traces'), names[caller], names[callee], first, second, (trace, remote), state=state))
                    caller_spans.append(parent)
                    assert body['headers']['traceparent'] == [f'00-{trace}-{remote}-01'], body
                    check_headers(body, trace, client, state, {baggage} if enabled else set())
                    for span in (parent, client, child):
                        assert int(span.get('flags', 0)) & 1, span
                assert max(int(s['startTimeUnixNano']) for s in caller_spans) < min(int(s['endTimeUnixNano']) for s in caller_spans), 'Requests did not overlap'
                if enabled:
                    for context in ({}, {'traceparent': '00-invalid-invalid-zz', 'tracestate': 'malformed'}):
                        first, second = uuid.uuid4().hex, uuid.uuid4().hex
                        body = h.request(names[caller], first, context,
                            f'http://{names[callee]}.{h.namespace}.svc:8080/work/{second}')
                        parent, client, child = h.poll(lambda: relationship(h.records('traces'), names[caller], names[callee], first, second))
                        fresh_root(parent, seen_trace_ids)
                        assert not body['downstream']['headers'].get('tracestate'), body
                        assert not baggage_values(body['downstream']['headers']), body
                        assert body['downstream']['headers']['traceparent'] == [generated_traceparent(client)], body
                    # An unsampled incoming parent must stay unsampled with parentbased_always_on.
                    trace, remote, first, second = uuid.uuid4().hex, uuid.uuid4().hex[:16], uuid.uuid4().hex, uuid.uuid4().hex
                    body = h.request(names[caller], first, {'traceparent': f'00-{trace}-{remote}-00'},
                        f'http://{names[callee]}.{h.namespace}.svc:8080/work/{second}')
                    outgoing = body['downstream']['headers']['traceparent']
                    assert len(outgoing) == 1 and outgoing[0].split('-')[1] == trace and outgoing[0].endswith('-00'), outgoing
                    assert valid_id(outgoing[0].split('-')[2], 16), outgoing
                    import time
                    time.sleep(3)
                    records = h.records('traces')
                    assert not [s for service in (names[caller], names[callee]) for s in items(records, 'traces', service) if s['traceId'] == trace]
                summary.append({'caller': caller, 'callee': callee, 'libc': libc,
                    'arch': h.args.arch, 'delivery': h.args.delivery, 'baggage': enabled})
                print(f'PASS propagation {caller}->{callee}/{libc}/baggage={enabled}', flush=True)
            h.save_apps()
            h.kube('delete', 'deployment', '--all', '-n', h.namespace, '--wait=true')
    (h.out / 'propagation-coverage.json').write_text(json.dumps(summary, indent=2))
    for lang in languages:
        h.config[lang]['env'].pop('RIO_FIXTURE_DELAY_MS', None)
