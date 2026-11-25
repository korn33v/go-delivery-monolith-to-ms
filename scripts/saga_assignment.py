#!/usr/bin/env python3
"""Acceptance checks for the stock-first User Story on the running Saga stack."""
import argparse
import subprocess
import sys

from saga_demo import http, snapshot, sql, wait_for


def available(product):
    return int(sql("stock", f"SELECT available FROM products WHERE id={product}"))


def check(scenario):
    product = 2 if scenario == "out-of-stock" else 1
    before = available(product)
    body = {"product_id": product, "quantity": 1}
    if scenario == "payment-declined":
        body["demo_failure"] = "payment"
    order = http("/api/v1/orders", body)
    print(f"{scenario}: order_id={order['id']} saga_id={order['saga_id']}", flush=True)
    assert (order["status"], order["saga_state"]) == ("PENDING", "STOCK_PENDING"), (
        f"expected PENDING/STOCK_PENDING at HTTP 202, got {order['status']}/{order['saga_state']}"
    )
    status = "CONFIRMED" if scenario == "happy" else "CANCELLED"
    wait_for(status, lambda: http("/api/v1/orders/" + order["id"]), lambda item: item["status"] == status)
    result = snapshot(order)
    steps = [(h["message_type"], h["state"], h["next_command"]) for h in result["history"]]
    initial = [("OrderCreated", "PENDING", ""), ("SagaStarted", "STOCK_PENDING", "ReserveStock")]
    reserved = ("StockReserved", "PAYMENT_PENDING", "ChargePayment")
    if scenario == "happy":
        assert steps == initial + [reserved, ("PaymentCharged", "COMPLETED", "")], steps
        assert result["stock"] == {"status": "RESERVED", "quantity": 1}, result
        assert (result["payment"]["state"], result["payment"]["charges"], result["payment"]["refunds"]) == ("CHARGED", 1, 0), result
        assert available(product) == before - 1, result
    elif scenario == "out-of-stock":
        assert steps == initial + [("StockReservationFailed", "CANCELLED", "")], steps
        assert result["stock"] == {"status": "REJECTED", "quantity": 1}, result
        assert result["payment"] is None and available(product) == before, result
    else:
        assert steps == initial + [reserved, ("PaymentFailed", "COMPENSATING", "ReleaseStock"),
                                   ("StockReleased", "CANCELLED", "")], steps
        assert result["stock"] == {"status": "RELEASED", "quantity": 1}, result
        assert (result["payment"]["state"], result["payment"]["charges"], result["payment"]["refunds"]) == ("DECLINED", 0, 0), result
        assert available(product) == before, result
    print(f"SUCCESS assignment {scenario}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario", choices=["happy", "out-of-stock", "payment-declined", "all"])
    scenario = parser.parse_args().scenario
    scenarios = ("happy", "out-of-stock", "payment-declined") if scenario == "all" else (scenario,)
    for item in scenarios:
        check(item)


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError, KeyError, TypeError, ValueError, subprocess.SubprocessError) as error:
        sys.exit(f"FAILURE: {error}")
