#!/usr/bin/env python3
"""E2E demonstration: actual HTTP, Kafka and each owner's PostgreSQL database."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ["docker", "compose", "-f", str(ROOT / "compose.saga.yaml")]
API = os.getenv("SAGA_API", "http://localhost:8080")
EXECUTOR = {"ChargePayment": "Payment", "ReserveStock": "Stock", "ReleaseStock": "Stock"}


def command(args, **kwargs):
    kwargs.setdefault("timeout", 90)
    return subprocess.run(args, cwd=ROOT, check=True, text=True, **kwargs)


def sql(database, query):
    result = command(COMPOSE + ["exec", "-T", "-e", f"PGPASSWORD={database}_demo",
                     "postgres", "psql", "-h", "127.0.0.1", "-U", database,
                     "-d", database, "-v", "ON_ERROR_STOP=1", "-At", "-c", query],
                     stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return result.stdout.strip()


def row(database, query):
    body = sql(database, f"SELECT row_to_json(result) FROM ({query}) result")
    return json.loads(body) if body else None


def http(path, body=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(API + path, data=data, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            expected = 200 if body is None else 202
            if response.status != expected:
                raise AssertionError(f"{path}: expected HTTP {expected}, got {response.status}")
            return json.load(response)
    except urllib.error.HTTPError as error:
        raise AssertionError(f"{path}: HTTP {error.code}: {error.read().decode()}") from error


def wait_for(description, read, matches, seconds=None):
    if seconds is None:
        seconds = float(os.getenv("SAGA_DEMO_TIMEOUT", "60"))
    deadline = time.monotonic() + seconds
    last = None
    while time.monotonic() < deadline:
        last = read()
        if matches(last):
            return last
        # Poll a condition with a deadline; elapsed time never implies success.
        time.sleep(0.15)
    raise AssertionError(f"timeout: {description}; last={last}")


def snapshot(order):
    order_id = order["id"]
    return {
        "order": row("orders", f"SELECT o.*,s.id AS saga_id,s.state AS saga_state FROM orders o JOIN sagas s ON s.order_id=o.id WHERE o.id='{order_id}'"),
        "payment": row("payments", f"SELECT state,charges,refunds,amount_cents FROM payments WHERE order_id='{order_id}'"),
        "stock": row("stock", f"SELECT status,quantity FROM stock_receipts WHERE order_id='{order_id}'"),
        "history": json.loads(sql("orders", f"SELECT COALESCE(json_agg(h ORDER BY seq),'[]') FROM (SELECT seq,state,message_type,next_command FROM saga_history WHERE saga_id='{order['saga_id']}') h")),
    }


def verify(order, scenario):
    state = "COMPLETED" if scenario == "happy" else "CANCELLED"
    wait_for(state, lambda: http("/api/v1/orders/" + order["id"]), lambda o: o["saga_state"] == state)
    result = snapshot(order)
    expected = {
        "happy": ("CONFIRMED", "CHARGED", 1, 0, "RESERVED", ["StockReserved", "PaymentCharged"]),
        "out-of-stock": ("CANCELLED", None, 0, 0, "REJECTED", ["StockReservationFailed"]),
        "payment-failed": ("CANCELLED", "DECLINED", 0, 0, "RELEASED", ["StockReserved", "PaymentFailed", "StockReleased"]),
    }
    status, payment, charges, refunds, stock, events = expected["payment-failed" if scenario == "compensation" else scenario]
    assert result["order"]["status"] == status, result
    assert result["order"]["saga_state"] == state, result
    if payment is None:
        assert result["payment"] is None, result
    else:
        assert result["payment"]["state"] == payment, result
        assert result["payment"]["charges"] == charges and result["payment"]["refunds"] == refunds, result
    assert result["stock"]["status"] == stock, result
    decisions = {
        "OrderCreated": ("PENDING", ""),
        "SagaStarted": ("STOCK_PENDING", "ReserveStock"),
        "StockReserved": ("PAYMENT_PENDING", "ChargePayment"),
        "StockReservationFailed": ("CANCELLED", ""),
        "PaymentCharged": ("COMPLETED", ""),
        "PaymentFailed": ("COMPENSATING", "ReleaseStock"),
        "StockReleased": ("CANCELLED", ""),
    }
    history = [(h["message_type"], h["state"], h["next_command"]) for h in result["history"]]
    assert history == [(event, *decisions[event]) for event in ["OrderCreated", "SagaStarted", *events]], history
    # Check physical inventory against ALL persisted successful reservations.
    inconsistent = sql("stock", "SELECT count(*) FROM products p WHERE p.available <> (CASE p.id WHEN 1 THEN 1000000 ELSE 0 END) - COALESCE((SELECT sum(quantity) FROM stock_receipts r WHERE r.product_id=p.id AND status='RESERVED'),0)")
    assert inconsistent == "0", "inventory does not match reservations"
    return result


def run_scenario(scenario):
    body = {"product_id": 2 if scenario == "out-of-stock" else 1, "quantity": 1}
    if scenario in ("payment-failed", "compensation"):
        body["demo_failure"] = "payment"
    order = http("/api/v1/orders", body)
    assert (order["status"], order["saga_state"]) == ("PENDING", "STOCK_PENDING"), order
    print(f"\n{scenario}: order_id={order['id']} saga_id={order['saga_id']}", flush=True)
    print(f"HTTP 202: status={order['status']} saga_state={order['saga_state']}", flush=True)
    result = verify(order, scenario)
    for step in result["history"]:
        command_type = step["next_command"]
        next_action = f" -> {command_type} ({EXECUTOR[command_type]})" if command_type else ""
        print(f"  {step['message_type']} -> {step['state']}{next_action}")
    print("PostgreSQL:", json.dumps(result, ensure_ascii=False), flush=True)
    print(f"SUCCESS {scenario}", flush=True)
    return order, result


def replay(database, table, order, kind, topic, use_response=False, new_id=False):
    column = "response" if use_response else "request"
    where = f"order_id='{order['id']}'"
    if table == "saga_receipts":
        where = f"saga_id='{order['saga_id']}'"
    if table != "stock_receipts":
        where += f" AND message_type='{kind}'"
    if kind == "ReleaseStock" and not use_response:
        message = json.loads(sql("orders", f"SELECT payload FROM outbox_messages WHERE id='{order['saga_id']}:ReleaseStock'"))
    else:
        if kind == "ReleaseStock":
            column = "release_response"
        message = json.loads(sql(database, f"SELECT {column} FROM {table} WHERE {where}"))
    if new_id:
        message["event_id"] += ":redelivery"
    # Publishing a response targets Order, whose receipt tracks that event type.
    target_db, target_table, target_where = database, table, where
    if use_response:
        target_db, target_table = "orders", "saga_receipts"
        target_where = f"saga_id='{order['saga_id']}' AND message_type='{message['message_type']}'"
    query = f"SELECT deliveries FROM {target_table} WHERE {target_where}"
    before = int(sql(target_db, query))
    command(COMPOSE + ["exec", "-T", "kafka", "/opt/kafka/bin/kafka-console-producer.sh",
                      "--bootstrap-server", "kafka:9092", "--topic", topic,
                      "--property", "parse.key=true", "--property", "key.separator=|"],
            input=message["saga_id"] + "|" + json.dumps(message) + "\n", stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    wait_for("duplicate consumed", lambda: int(sql(target_db, query)), lambda value: value > before)
    print(f"  повтор {message['message_type']}; event_id={message['event_id']}; deliveries>{before}", flush=True)


def duplicate():
    order, before = run_scenario("happy")
    replay("payments", "payment_receipts", order, "ChargePayment", "saga.events.v1", use_response=True)
    replay("payments", "payment_receipts", order, "ChargePayment", "saga.payment.commands.v1", new_id=True)
    replay("stock", "stock_receipts", order, "ReserveStock", "saga.stock.commands.v1", new_id=True)
    replay("stock", "stock_receipts", order, "ReserveStock", "saga.events.v1", use_response=True, new_id=True)
    after = verify(order, "happy")
    assert before == after, "duplicate changed business data or transition history"
    released, before = run_scenario("compensation")
    replay("stock", "stock_receipts", released, "ReleaseStock", "saga.stock.commands.v1", new_id=True)
    replay("stock", "stock_receipts", released, "ReserveStock", "saga.stock.commands.v1", new_id=True)
    replay("stock", "stock_receipts", released, "ReleaseStock", "saga.events.v1", use_response=True)
    assert before == verify(released, "compensation"), "duplicate changed reservation release"
    print("SUCCESS duplicate: at most one charge and one release; history unchanged", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario", choices=["happy", "out-of-stock", "payment-failed", "compensation", "duplicate", "all"])
    parser.add_argument("--no-start", action="store_true", help="Use already started Compose services")
    args = parser.parse_args()
    if not args.no_start:
        subprocess.run(COMPOSE + ["up", "-d", "--build", "--wait", "--wait-timeout", "240"], cwd=ROOT, check=True)
    if args.scenario == "all":
        for scenario in ("happy", "out-of-stock", "payment-failed"):
            run_scenario(scenario)
        duplicate()
    elif args.scenario == "duplicate":
        duplicate()
    else:
        run_scenario(args.scenario)


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print(f"FAILURE: {error}", file=sys.stderr)
        if getattr(error, "stderr", None):
            print(error.stderr, file=sys.stderr)
        sys.exit(1)
