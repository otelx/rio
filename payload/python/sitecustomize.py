"""Load the upstream zero-code entry point from the injected package directory."""
from opentelemetry.instrumentation.auto_instrumentation import initialize

initialize()
