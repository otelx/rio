"""Ordinary aiohttp application: all telemetry comes from RIO's injected agent."""

import logging

from aiohttp import ClientSession, ClientTimeout, web


logger = logging.getLogger("rio.fixture")
logger.setLevel(logging.INFO)
logger.addHandler(logging.StreamHandler())
# Keep propagation to the root logger: the injected agent owns its OTLP handler.


async def health(request):
    return web.json_response({"status": "ok", "language": "python"})


async def work(request):
    marker = request.match_info["marker"]
    logger.info("rio-fixture %s", marker)
    headers = {}
    for name, value in request.raw_headers:
        headers.setdefault(name.decode("ascii").lower(), []).append(
            value.decode("latin-1")
        )
    result = {"marker": marker, "language": "python", "headers": headers}
    # Bounded ordinary application activity for automatically collected metrics.
    result["checksum"] = sum(bytearray(4096))
    if downstream := request.query.get("next"):
        async with ClientSession(timeout=ClientTimeout(total=10)) as client:
            async with client.get(downstream) as response:
                response.raise_for_status()
                result["downstream"] = await response.json()
    return web.json_response(result)


app = web.Application()
app.add_routes([web.get("/health", health), web.get("/work/{marker}", work)])

if __name__ == "__main__":
    web.run_app(app, host="0.0.0.0", port=8080, access_log=None)
