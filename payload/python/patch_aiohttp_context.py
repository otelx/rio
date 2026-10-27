"""Repair full request-context activation in aiohttp-server 0.66b0.

The upstream proposal documents the same lost-baggage defect:
https://github.com/open-telemetry/opentelemetry-python-contrib/pull/2510
(closed without merging; head eee3787d8eba5c57ab66192cb8e2a093d975f8e8).
Keep existing span/metric/error behavior while attaching the full extracted
context until request handling completes, with unconditional token cleanup.
"""
import base64
import csv
import hashlib
from pathlib import Path
import sys


SOURCE = Path('opentelemetry/instrumentation/aiohttp_server/__init__.py')
ORIGINAL_SHA256 = '8f8a38a6251af33619e6ca95ad1c527bcd51143a378984e06131049d3eae14cf'
HELPER = '''@contextmanager
def _rio_request_context(request):
    extracted = extract(request, getter=getter)
    token = context_api.attach(extracted)
    try:
        yield extracted
    finally:
        context_api.detach(token)


'''


def apply(root):
    path = root / SOURCE
    original = path.read_bytes()
    if hashlib.sha256(original).hexdigest() != ORIGINAL_SHA256:
        raise RuntimeError('Unexpected aiohttp-server source; review/remove full-context repair')
    record_path = root / 'opentelemetry_instrumentation_aiohttp_server-0.66b0.dist-info/RECORD'
    with record_path.open(newline='') as stream:
        records = list(csv.reader(stream))
    replacements = (
        ('# SPDX-License-Identifier: Apache-2.0\n',
         '# SPDX-License-Identifier: Apache-2.0\n'
         '# Modified by RIO: attach/detach the full extracted aiohttp request context.\n'
         '# Related upstream proposal: opentelemetry-python-contrib#2510\n'),
        ('from timeit import default_timer\n',
         'from contextlib import contextmanager\nfrom timeit import default_timer\n'),
        ('from opentelemetry import metrics, trace\n',
         'from opentelemetry import context as context_api\nfrom opentelemetry import metrics, trace\n'),
        ('def create_aiohttp_middleware(\n', HELPER + 'def create_aiohttp_middleware(\n'),
        ('        with _tracer.start_as_current_span(\n',
         '        with _rio_request_context(request) as extracted, _tracer.start_as_current_span(\n'),
        ('            context=extract(request, getter=getter),\n',
         '            context=extracted,\n'),
    )
    source = original.decode()
    for before, after in replacements:
        if source.count(before) != 1:
            raise RuntimeError('Unexpected aiohttp-server patch location')
        source = source.replace(before, after)
    updated = source.encode()
    compile(source, str(path), 'exec')
    matching = [row for row in records if row[0] == SOURCE.as_posix()]
    if len(matching) != 1:
        raise RuntimeError('Unexpected aiohttp-server package RECORD')
    digest = base64.urlsafe_b64encode(hashlib.sha256(updated).digest()).decode().rstrip('=')
    matching[0][1:] = ['sha256=' + digest, str(len(updated))]
    path.write_bytes(updated)
    with record_path.open('w', newline='') as stream:
        csv.writer(stream).writerows(records)


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('Usage: patch_aiohttp_context.py PAYLOAD_DIRECTORY')
    apply(Path(sys.argv[1]))
