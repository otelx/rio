"""Backport OTLP trace flag encoding for the pinned Python exporter 1.45.1.

Upstream issue: https://github.com/open-telemetry/opentelemetry-python/issues/4666
Upstream fix: https://github.com/open-telemetry/opentelemetry-python/pull/5667
Only the protobuf exporter is bundled. This two-line backport preserves its
existing remote-parent flags and adds each span/link's own W3C trace flags.
Remove it when the pinned exporter includes the upstream fix.
"""
import base64
import csv
import hashlib
from pathlib import Path
import sys


SOURCE = Path('opentelemetry/exporter/otlp/proto/common/_internal/trace_encoder/__init__.py')
ORIGINAL_SHA256 = '286b1140e27f9c226795ad536db423a6781488c8e4ea4381cf91ee282caae498'


def apply(root):
    path = root / SOURCE
    original = path.read_bytes()
    if hashlib.sha256(original).hexdigest() != ORIGINAL_SHA256:
        raise RuntimeError('Unexpected Python OTLP encoder source; review/remove trace flags backport')
    record_path = root / 'opentelemetry_exporter_otlp_proto_common-1.45.1.dist-info/RECORD'
    with record_path.open(newline='') as stream:
        records = list(csv.reader(stream))
    source = original.decode()
    source = source.replace(
        '# SPDX-License-Identifier: Apache-2.0\n',
        '# SPDX-License-Identifier: Apache-2.0\n'
        '# Modified by RIO: preserve W3C span/link trace flags alongside remote flags.\n'
        '# Backport of https://github.com/open-telemetry/opentelemetry-python/pull/5667\n'
        '# (head bc72d0c8f1a1ecc72a998546a88a3c165377f806; upstream issue #4666).\n',
        1,
    )
    for before, after in (
        ('flags=_span_flags(sdk_span.parent),',
         'flags=_span_flags(sdk_span.parent) | int(span_context.trace_flags),'),
        ('flags=_span_flags(link.context),',
         'flags=_span_flags(link.context) | int(link.context.trace_flags),'),
    ):
        if source.count(before) != 1:
            raise RuntimeError('Unexpected Python OTLP encoder patch location')
        source = source.replace(before, after)
    updated = source.encode()
    matching = [row for row in records if row[0] == SOURCE.as_posix()]
    if len(matching) != 1:
        raise RuntimeError('Unexpected Python OTLP exporter package RECORD')
    digest = base64.urlsafe_b64encode(hashlib.sha256(updated).digest()).decode().rstrip('=')
    matching[0][1:] = ['sha256=' + digest, str(len(updated))]
    path.write_bytes(updated)
    with record_path.open('w', newline='') as stream:
        csv.writer(stream).writerows(records)


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('Usage: patch_trace_flags.py PAYLOAD_DIRECTORY')
    apply(Path(sys.argv[1]))
