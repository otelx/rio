"""Encoder-only regression tests; application integration uses ordinary HTTP."""
import unittest

from opentelemetry.exporter.otlp.proto.common._internal.trace_encoder import _encode_span
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import ReadableSpan
from opentelemetry.trace import Link, SpanContext, TraceFlags


class TraceFlagsTest(unittest.TestCase):
    def context(self, flags, remote=False):
        return SpanContext(1, 2, is_remote=remote, trace_flags=TraceFlags(flags))

    def test_span_own_flags_and_remote_parent_are_independent(self):
        for flags in (0, 1, 2, 3, 255):
            for parent_remote in (None, False, True):
                with self.subTest(flags=flags, parent_remote=parent_remote):
                    parent = None if parent_remote is None else self.context(0, parent_remote)
                    span = ReadableSpan('encoder-regression', context=self.context(flags),
                                        parent=parent, resource=Resource({}), start_time=1, end_time=2)
                    encoded = _encode_span(span)
                    self.assertEqual(encoded.flags, flags | 0x100 | (0x200 if parent_remote else 0))

    def test_links_keep_their_own_flags_and_remote_status(self):
        for flags in (0, 1, 2, 3, 255):
            for remote in (False, True):
                with self.subTest(flags=flags, remote=remote):
                    span = ReadableSpan('encoder-regression', context=self.context(0),
                                        resource=Resource({}), start_time=1, end_time=2,
                                        links=(Link(self.context(flags, remote)),))
                    encoded = _encode_span(span)
                    self.assertEqual(encoded.links[0].flags, flags | 0x100 | (0x200 if remote else 0))


if __name__ == '__main__':
    unittest.main()
