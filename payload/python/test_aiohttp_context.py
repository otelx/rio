"""Context-lifecycle regression; run with the fixture's pinned aiohttp installed.

These unit checks do not create/export telemetry. The admission propagation
scenario separately verifies automatic server/client spans and received headers.
"""
import asyncio
from types import SimpleNamespace
import unittest

from multidict import CIMultiDict, CIMultiDictProxy
from opentelemetry import baggage, context, propagate
from opentelemetry.baggage.propagation import W3CBaggagePropagator
from opentelemetry.instrumentation.aiohttp_server import _rio_request_context
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator


class AiohttpContextTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.original = propagate.get_global_textmap()
        self.token = context.attach(context.Context())
        propagate.set_global_textmap(CompositePropagator([
            TraceContextTextMapPropagator(), W3CBaggagePropagator(),
        ]))

    async def asyncTearDown(self):
        propagate.set_global_textmap(self.original)
        context.detach(self.token)

    def request(self, marker=None):
        headers = {} if marker is None else {'baggage': 'rio_marker=' + marker}
        return SimpleNamespace(headers=CIMultiDictProxy(CIMultiDict(headers)))

    async def test_request_scope_restores_previous_context(self):
        before = context.get_current()
        with _rio_request_context(self.request('first')):
            self.assertEqual(baggage.get_baggage('rio_marker'), 'first')
        self.assertIs(context.get_current(), before)
        with _rio_request_context(self.request()):
            self.assertIsNone(baggage.get_baggage('rio_marker'))

    async def test_exception_restores_previous_context(self):
        before = context.get_current()
        with self.assertRaisesRegex(RuntimeError, 'application failed'):
            with _rio_request_context(self.request('failure')):
                self.assertEqual(baggage.get_baggage('rio_marker'), 'failure')
                raise RuntimeError('application failed')
        self.assertIs(context.get_current(), before)
        self.assertIsNone(baggage.get_baggage('rio_marker'))

    async def test_overlapping_tasks_keep_distinct_baggage(self):
        async def handle(marker):
            with _rio_request_context(self.request(marker)):
                await asyncio.sleep(.01)
                self.assertEqual(baggage.get_baggage('rio_marker'), marker)
            self.assertIsNone(baggage.get_baggage('rio_marker'))
        await asyncio.gather(*(handle(str(i)) for i in range(8)))
        self.assertIsNone(baggage.get_baggage('rio_marker'))

    async def test_disabled_baggage_propagator_stays_disabled(self):
        propagate.set_global_textmap(TraceContextTextMapPropagator())
        with _rio_request_context(self.request('must-not-propagate')):
            self.assertIsNone(baggage.get_baggage('rio_marker'))


if __name__ == '__main__':
    unittest.main()
