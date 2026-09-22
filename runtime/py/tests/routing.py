"""Test fixtures share one explicitly owned dispatcher per endpoint."""
from nightseam.runtime import Dispatcher


def dispatcher(endpoint):
    held = getattr(endpoint, "_test_routes", None)
    if held is None:
        held = Dispatcher(endpoint)
        endpoint._test_routes = held
    return held


def select_endpoint(endpoint, path):
    return dispatcher(endpoint).select(path)
