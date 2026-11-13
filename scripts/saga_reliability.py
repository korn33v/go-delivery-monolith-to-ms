#!/usr/bin/env python3
"""Real Kafka/PostgreSQL regression for persisted Outbox and DLQ recovery."""
import argparse
import base64
import json
import subprocess
import sys
import uuid

from saga_demo import COMPOSE, ROOT, command, http, row, run_scenario, sql, verify, wait_for


def publish(topic, key, body):
    command(COMPOSE + ["exec", "-T", "kafka", "/opt/kafka/bin/kafka-console-producer.sh",
                      "--bootstrap-server", "kafka:9092", "--topic", topic,
                      "--property", "parse.key=true", "--property", "key.separator=|"],
            input=key + "|" + body + "\n", stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def outbox_recovery():
    print("Outbox: stop Kafka, accept order, restart Order, restore Kafka", flush=True)
    command(COMPOSE + ["stop", "kafka"])
    try:
        order = http("/api/v1/orders", {"product_id": 2, "quantity": 1})
        message_id = order["saga_id"] + ":ChargePayment"
        queued = row("orders", f"SELECT id,published_at FROM outbox_messages WHERE id='{message_id}'")
        assert queued and queued["published_at"] is None, queued
        assert sql("payments", f"SELECT count(*) FROM payments WHERE order_id='{order['id']}'") == "0"
        assert http("/api/v1/orders/" + order["id"])["saga_state"] == "PAYMENT_PENDING"
        wait_for("failed relay attempt recorded",
                 lambda: int(sql("orders", f"SELECT attempts FROM outbox_messages WHERE id='{message_id}'")),
                 lambda attempts: attempts > 0)
        command(COMPOSE + ["stop", "order-service"])
        print(f"  HTTP 202; order_id={order['id']}; intent survives process stop", flush=True)
    finally:
        command(COMPOSE + ["start", "kafka"])
        command(COMPOSE + ["up", "-d", "--wait", "--wait-timeout", "120", "order-service"], timeout=180)
    result = verify(order, "compensation")
    assert result["payment"]["charges"] == 1 and result["payment"]["refunds"] == 1
    wait_for("initial command published after restart",
             lambda: sql("orders", f"SELECT published_at IS NOT NULL FROM outbox_messages WHERE id='{message_id}'"),
             lambda published: published == "t")
    print("SUCCESS outbox: stored while Kafka offline; delivered after restart; compensation completed", flush=True)


def assert_kafka_dlq(topic, envelope):
    offsets = command(COMPOSE + ["exec", "-T", "kafka", "/opt/kafka/bin/kafka-get-offsets.sh",
                               "--bootstrap-server", "kafka:9092", "--topic", topic, "--time", "-1"],
                      stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.strip()
    end_offset = int(offsets.splitlines()[0].rsplit(":", 1)[1])
    assert end_offset > 0, "DLQ topic is empty"
    result = command(COMPOSE + ["exec", "-T", "kafka", "/opt/kafka/bin/kafka-console-consumer.sh",
                               "--bootstrap-server", "kafka:9092", "--topic", topic,
                               "--partition", "0", "--offset", "0", "--max-messages", str(end_offset),
                               "--timeout-ms", "10000"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    found = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
    assert any(message == envelope for message in found), "durable DLQ envelope not found in Kafka"


def wait_dlq(database, source_topic, body, attempts):
    encoded = base64.b64encode(body.encode()).decode()
    envelope = wait_for("durable quarantine", lambda: row(database,
        f"SELECT envelope FROM dead_letters WHERE envelope->>'original_value'='{encoded}'"), bool)["envelope"]
    assert envelope["source_topic"] == source_topic and envelope["attempts"] == attempts, envelope
    assert base64.b64decode(envelope["original_value"]).decode() == body
    message_id = envelope["id"]
    wait_for("DLQ relay delivery", lambda: sql(database,
        f"SELECT published_at IS NOT NULL FROM outbox_messages WHERE id='{message_id}'"), lambda sent: sent == "t")
    assert_kafka_dlq(source_topic + ".dlq", envelope)
    return envelope


def dlq_regression():
    # Invalid JSON must not block any of the three consumer groups.
    for database, topic in (("orders", "saga.events.v1"), ("payments", "saga.payment.commands.v1"),
                            ("stock", "saga.stock.commands.v1")):
        marker = "dlq_" + uuid.uuid4().hex
        body = '{"marker":"' + marker + '",broken'
        publish(topic, marker, body)
        envelope = wait_dlq(database, topic, body, 1)
        assert envelope["reason"] == "permanent_error", envelope
        print(f"  {database}: invalid JSON quarantined; source_offset={envelope['source_offset']}", flush=True)

    # A temporary technical failure has a bounded, persisted retry budget.
    # This trigger affects only this test's unique order, never other purchases.
    marker = "retry_" + uuid.uuid4().hex
    trigger = "fail_" + marker
    sql("payments", f"""CREATE FUNCTION {trigger}() RETURNS trigger LANGUAGE plpgsql AS $$
      BEGIN IF NEW.order_id='{marker}' THEN RAISE EXCEPTION 'demo transient failure' USING ERRCODE='40001'; END IF; RETURN NEW; END $$;
      CREATE TRIGGER {trigger} BEFORE INSERT ON payments FOR EACH ROW EXECUTE FUNCTION {trigger}();""")
    try:
        body = json.dumps({"event_id": marker + ":ChargePayment", "saga_id": marker, "order_id": marker,
                           "message_type": "ChargePayment", "schema_version": 1,
                           "created_at": "2026-11-13T12:00:00Z",
                           "data": {"product_id": 1, "quantity": 1, "amount_cents": 100}})
        publish("saga.payment.commands.v1", marker, body)
        envelope = wait_dlq("payments", "saga.payment.commands.v1", body, 3)
        assert envelope["reason"] == "retries_exhausted", envelope
        assert sql("payments", f"SELECT count(*) FROM payments WHERE order_id='{marker}'") == "0"
    finally:
        sql("payments", f"DROP TRIGGER IF EXISTS {trigger} ON payments; DROP FUNCTION IF EXISTS {trigger}();")
    run_scenario("happy")
    print("SUCCESS dlq: original bytes retained; technical retries=3; subsequent order completed", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario", choices=["outbox", "dlq", "all"])
    parser.add_argument("--no-start", action="store_true")
    args = parser.parse_args()
    if not args.no_start:
        subprocess.run(COMPOSE + ["up", "-d", "--build", "--wait", "--wait-timeout", "240"], cwd=ROOT, check=True)
    if args.scenario in ("outbox", "all"):
        outbox_recovery()
    if args.scenario in ("dlq", "all"):
        dlq_regression()


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print(f"FAILURE: {error}", file=sys.stderr)
        if getattr(error, "stderr", None):
            print(error.stderr, file=sys.stderr)
        sys.exit(1)
