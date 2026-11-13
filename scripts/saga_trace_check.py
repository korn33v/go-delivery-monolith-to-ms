#!/usr/bin/env python3
"""Verify the UI trace header and the complete Saga trace in Jaeger."""
import json
import re
import time
import urllib.error
import urllib.request

request = urllib.request.Request("http://localhost:8080/api/v1/orders",
    data=json.dumps({"product_id": 2, "quantity": 1}).encode(),
    headers={"Content-Type": "application/json"})
with urllib.request.urlopen(request, timeout=10) as response:
    order = json.load(response)
    values = response.headers.get_all("X-Trace-ID")
assert len(values or []) == 1 and re.fullmatch(r"[0-9a-f]{32}", values[0]), values
trace_id = values[0]
expected = {"gateway", "order-service", "payment-service", "stock-service"}
deadline = time.monotonic() + 60
found = set()
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen("http://localhost:16686/api/traces/" + trace_id, timeout=5) as response:
            result = json.load(response)
        traces = result.get("data") or []
        if traces:
            found = {p["serviceName"] for p in traces[0]["processes"].values()}
        with urllib.request.urlopen("http://localhost:8080/api/v1/orders/" + order["id"], timeout=5) as response:
            status = json.load(response)
        if expected <= found and status["saga_state"] == "CANCELLED":
            print("SUCCESS Jaeger: single trace ID; compensation spans across", ", ".join(sorted(found)))
            print("http://localhost:16686/trace/" + trace_id)
            break
    except (urllib.error.URLError, TimeoutError):
        pass
    time.sleep(0.25)
else:
    raise SystemExit(f"FAILURE Jaeger: trace={trace_id}, services={found}")
