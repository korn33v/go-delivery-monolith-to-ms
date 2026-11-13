#!/usr/bin/env python3
"""Author Archify candidates from committed code and the running SQL schemas."""
import argparse
from functools import lru_cache
import json
from pathlib import Path
import subprocess
from urllib.parse import urlsplit, urlunsplit

from saga_demo import ROOT, sql

REVISION = ""
TRANSLATIONS = json.loads((ROOT / "docs/sources/05-order-service.json").read_text())["meta"]["translations"]
TRANSLATIONS.update(json.loads((ROOT / "docs/sources/saga-ru.json").read_text()))


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


@lru_cache(None)
def committed(path):
    return git("show", REVISION + ":" + path).splitlines()


def source(path, start=None, end=None):
    lines = committed(path)
    first = 0 if start is None else next(i for i, line in enumerate(lines) if start in line)
    last = len(lines)
    if end is not None:
        last = next(i for i in range(first + 1, len(lines)) if end in lines[i])
    elif start and start.startswith("func "):
        last = next((i for i in range(first + 1, len(lines)) if lines[i].startswith("func ")), len(lines))
    return {"path": path, "line": first + 1, "end_line": max(first + 1, last)}


def meta(name, title, quality="showcase"):
    origin = git("remote", "get-url", "origin")
    if origin.startswith(("http://", "https://")):
        parsed = urlsplit(origin)
        origin = urlunsplit((parsed.scheme, parsed.netloc.rsplit("@", 1)[-1], parsed.path, "", ""))
    return {"title": title, "output": "docs/diagrams/" + name + ".html", "quality_profile": quality,
            "locale": "ru", "translations": TRANSLATIONS,
            "repository": {"url": origin, "revision": REVISION, "link_mode": "local-only"}}


def component(identifier, label, detail, kind, x, y, sources, width=220):
    return {"id": identifier, "label": label, "sublabel": detail, "type": kind,
            "pos": [x, y], "size": [width, 88], "sources": sources}


def edge(start, end, label, variant="default"):
    return {"id": start + "-" + end, "from": start, "to": end, "label": label, "variant": variant}


def containers():
    compose = "compose.saga.yaml"
    nodes = [
        component("browser", "Браузер", "React вызывает API :8080", "external", 40, 40,
                  [source("frontend/src/App.jsx", "const API", "export default")], 190),
        component("frontend", "Frontend / Nginx", "Vite build · :5173", "frontend", 290, 40,
                  [source(compose, "  frontend:", "volumes:"), source("frontend/nginx.conf")]),
        component("gateway", "API Gateway", "Gin ReverseProxy · :8080", "backend", 290, 230,
                  [source(compose, "  gateway:", "  frontend:"), source("gateway/router/router.go", "func (g *Gateway) orders"), source("gateway/router/router.go", "func (g *Gateway) StockRead")]),
        component("order", "Order Service", "Orchestrator + relay · :8083", "backend", 580, 230,
                  [source("order-service/main.go", "func runSaga"), source("order-service/internal/usecase/saga.go", "func (s *Orchestrator) Handle"), source("order-service/internal/usecase/saga.go", "func (s *Orchestrator) Create")], 240),
        component("stock", "Stock Service", "Резерв + relay · :8082", "backend", 580, 500,
                  [source("stock-service/main.go", "func runSaga"), source("stock-service/internal/usecase/saga.go", "func (s *SagaService) Handle")], 240),
        component("payment", "Payment Service", "Оплата/возврат + relay · :8084", "backend", 580, 770,
                  [source("payment-service/main.go", "func runSaga"), source("payment-service/internal/usecase/saga.go", "func (s *SagaService) Handle")], 240),
        component("kafka", "Kafka 3.9.1", "3 исходных топика + 3 DLQ", "messagebus", 290, 500,
                  [source(compose, "  kafka:", "  kafka-init:"), source("internal/sagaruntime/runtime.go", "func Messaging")]),
        component("postgres", "PostgreSQL 17", "3 БД · отдельные роли · :5432", "database", 930, 500,
                  [source(compose, "  postgres:", "  order-service:"), source("infra/saga/init.sql")], 240),
        component("init", "kafka-init", "Однократное создание топиков", "backend", 290, 770,
                  [source(compose, "  kafka-init:", "  postgres:")]),
    ]
    return {"schema_version": 1, "diagram_type": "architecture",
            "meta": meta("saga-containers", "GoDelivery: контейнеры Saga и границы данных"),
            "components": nodes,
            "boundaries": [{"kind": "region", "label": "Docker Compose: godelivery-saga", "wraps": [n["id"] for n in nodes if n["id"] != "browser"], "pad": 24}],
            "connections": [edge("browser", "frontend", "GET /"), edge("browser", "gateway", "HTTP API", "emphasis"),
                            edge("gateway", "order", "orders", "emphasis"), edge("gateway", "stock", "stock read"),
                            edge("order", "kafka", "produce / consume", "dashed"), edge("stock", "kafka", "produce / consume", "dashed"),
                            edge("payment", "kafka", "produce / consume", "dashed"), edge("init", "kafka", "create topics"),
                            edge("order", "postgres", "orders SQL"), edge("payment", "postgres", "payments SQL"), edge("stock", "postgres", "stock SQL")],
            "cards": [{"dot": "cyan", "title": "Контейнеры и процессы", "items": ["Consumer и Outbox relay работают внутри каждого Go-процесса. Отдельных сервисов Outbox/DLQ нет.", "React исполняется в браузере: Nginx отдаёт файлы и не проксирует API."]},
                      {"dot": "emerald", "title": "Владение данными", "items": ["Один PostgreSQL контейнер экономит ресурсы; orders, payments и stock — разные БД и роли.", "Каждая БД содержит собственные Outbox, failures и DLQ. Межбазовых SQL-запросов и FK нет."]},
                      {"dot": "amber", "title": "Что означают связи", "items": ["produce / consume — клиентское соединение сервиса с Kafka; порядок команд и событий раскрыт на Sequence.", "kafka-init завершается после создания топиков; старый стенд Strangler запускается отдельным Compose."]}]}


def components():
    return {"schema_version": 1, "diagram_type": "architecture",
            "meta": meta("saga-components", "Order Service: компоненты оркестрации и доставки"),
            "components": [
                component("http", "HTTP adapter", "SagaHandler.Create / Get", "backend", 40, 40, [source("order-service/handler/saga.go", "func (h *SagaHandler) Create"), source("order-service/handler/saga.go", "func (h *SagaHandler) Get")]),
                component("orchestrator", "Orchestrator", "Создать / обработать событие", "backend", 410, 40, [source("order-service/internal/usecase/saga.go", "func (s *Orchestrator) Create"), source("order-service/internal/usecase/saga.go", "func (s *Orchestrator) Handle")], 240),
                component("domain", "Saga domain", "Start / Apply → следующая команда", "backend", 820, 40, [source("order-service/internal/domain/saga.go")], 260),
                component("consumer", "Kafka delivery", "Handler + retry/DLQ policy", "backend", 40, 320, [source("internal/bus/kafka.go", "func handleDelivery"), source("internal/bus/kafka.go", "func consume"), source("order-service/handler/saga.go", "func (h *SagaHandler) Event")]),
                component("stores", "SQL repositories", "SagaStore + Outbox + DLQ Store", "backend", 410, 320, [source("order-service/internal/repository/saga.go", "func (r *SagaStore) Save"), source("internal/outbox/store.go", "func (s *Store) EnqueueJSON"), source("internal/deadletter/store.go", "func (s *Store) OnFailure")], 260),
                component("db", "orders DB", "Заказ + receipts + Outbox + DLQ", "database", 820, 320, [source("order-service/internal/repository/saga.sql"), source("internal/outbox/schema.sql"), source("internal/deadletter/schema.sql")], 260),
                component("relay", "Outbox relay", "Retry, порядок key, published_at", "backend", 410, 620, [source("internal/outbox/relay.go", "func (s *Store) DispatchOne"), source("internal/outbox/relay.go", "func (s *Store) Run")], 260),
                component("kafka", "Kafka", "Команды, события и DLQ", "messagebus", 40, 620, [source("internal/bus/kafka.go", "func (p *Producer) Publish"), source("internal/sagaruntime/runtime.go", "func Messaging")]),
            ],
            "connections": [edge("http", "orchestrator", "Create / Get", "emphasis"), edge("consumer", "orchestrator", "Handle event", "emphasis"),
                            edge("orchestrator", "domain", "Start / Apply"), edge("orchestrator", "stores", "Save + Enqueue"),
                            edge("consumer", "stores", "on failure: DLQ"), edge("stores", "db", "local SQL tx"),
                            edge("relay", "stores", "DispatchOne"), edge("relay", "kafka", "Publish JSON", "dashed"), edge("kafka", "consumer", "FetchMessage", "dashed")],
            "cards": [{"dot": "cyan", "title": "Уровень компонентов", "items": ["Схема раскрывает один контейнер Order; Kafka и orders DB — внешние для его Go-процесса.", "SQL repositories объединяет реальные пакеты SagaStore, Outbox и DLQ Store; универсального workflow engine нет."]},
                      {"dot": "emerald", "title": "Граница commit", "items": ["Orchestrator явно передаёт *sql.Tx и сохраняет переход, receipt и Outbox атомарно.", "Consumer подтверждает offset после SQL commit или устойчивого карантина. Relay публикует независимо от HTTP/consumer."]},
                      {"dot": "amber", "title": "Повтор и отказ", "items": ["Permanent error → DLQ сразу; технический сбой handler → 3 попытки в PostgreSQL.", "Сбой publish → backoff relay; publish до commit published_at → возможный дубль, защищённый receipts."]}]}


def sequence():
    participants = [
        {"id": "client", "type": "external", "label": "Client / Gateway", "sources": [source("order-service/handler/saga.go", "func (h *SagaHandler) Create")]},
        {"id": "order", "type": "backend", "label": "Order", "sublabel": "Orchestrator + Outbox", "sources": [source("order-service/internal/usecase/saga.go", "func (s *Orchestrator) Create"), source("order-service/internal/usecase/saga.go", "func (s *Orchestrator) Handle"), source("internal/outbox/relay.go", "func (s *Store) DispatchOne")]},
        {"id": "kafka", "type": "messagebus", "label": "Kafka", "sources": [source("internal/bus/kafka.go", "func consume")]},
        {"id": "payment", "type": "backend", "label": "Payment", "sublabel": "Своя БД + Outbox", "sources": [source("payment-service/internal/usecase/saga.go", "func (s *SagaService) Handle"), source("payment-service/internal/domain/saga.go", "func (p *SagaPayment) Refund")]},
        {"id": "stock", "type": "backend", "label": "Stock", "sublabel": "Своя БД + Outbox", "sources": [source("stock-service/internal/usecase/saga.go", "func (s *SagaService) Handle"), source("stock-service/internal/domain/saga.go", "func ReserveAvailable")]},
    ]
    messages = []
    y = 160
    def message(start, end, label, note=None, variant="dashed"):
        nonlocal y
        item = {"id": "step-" + str(len(messages) + 1), "from": start, "to": end, "label": label, "y": y, "variant": variant}
        if note:
            item["note"] = note
        messages.append(item)
        y += 76 if note else 36
    message("client", "order", "POST /orders: product_id=2", "COMMIT: PENDING + ChargePayment в Outbox", "emphasis")
    message("order", "client", "202 PENDING", variant="return")
    message("order", "kafka", "ChargePayment", "Relay; published_at отмечается после broker ack")
    message("kafka", "payment", "ChargePayment", "COMMIT: CHARGED, charges=1 + PaymentCharged в Outbox")
    message("payment", "kafka", "PaymentCharged")
    message("kafka", "order", "PaymentCharged", "COMMIT: STOCK_PENDING + ReserveStock в Outbox")
    message("order", "kafka", "ReserveStock")
    message("kafka", "stock", "ReserveStock", "available=0; COMMIT: REJECTED + StockReservationFailed в Outbox")
    message("stock", "kafka", "StockReservationFailed")
    message("kafka", "order", "StockReservationFailed", "COMMIT: COMPENSATING + RefundPayment в Outbox")
    message("order", "kafka", "RefundPayment")
    message("kafka", "payment", "RefundPayment", "COMMIT: REFUNDED, refunds=1 + PaymentRefunded в Outbox")
    message("payment", "kafka", "PaymentRefunded")
    message("kafka", "order", "PaymentRefunded", "COMMIT: Saga CANCELLED, заказ CANCELLED")
    message("client", "order", "GET /api/v1/orders/{id}", variant="emphasis")
    message("order", "client", "CANCELLED / CANCELLED", variant="return")
    return {"schema_version": 1, "diagram_type": "sequence", "meta": meta("saga-sequence", "Компенсация: оплата → отказ склада → возврат"),
            "participants": participants, "messages": messages,
            "cards": [{"dot": "emerald", "title": "Локальные транзакции", "items": ["Ноты показывают SQL commit внутри владельца; межбазовых вызовов нет. Каждый сервис включает consumer и relay.", "Каждое исходящее сообщение отправляет relay из ранее закоммиченного Outbox; Kafka offset входящего сообщения фиксируется после локального commit."]},
                      {"dot": "amber", "title": "Что произойдёт при отказе", "items": ["RefundPayment ещё не означает возврат. Только PaymentRefunded позволяет отменить заказ.", "PaymentRefundFailed оставляет COMPENSATION_FAILED. Ошибка обработчика идёт в retry/DLQ, а не изображается успешной компенсацией."]}]}


def erd(database):
    column_rows = json.loads(sql(database, """SELECT json_agg(json_build_object('table',c.relname,'name',a.attname,
      'type',format_type(a.atttypid,a.atttypmod),'comment',col_description(c.oid,a.attnum)) ORDER BY c.relname,a.attnum)
      FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid
      WHERE n.nspname='public' AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped"""))
    constraints = json.loads(sql(database, """SELECT json_agg(json_build_object('table',c.relname,'kind',co.contype,
      'columns',(SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(co.conkey) WITH ORDINALITY k(num,ord) JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.num),
      'target',parent.relname,'references',(SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(co.confkey) WITH ORDINALITY k(num,ord) JOIN pg_attribute a ON a.attrelid=co.confrelid AND a.attnum=k.num)) ORDER BY c.relname,co.conname)
      FROM pg_constraint co JOIN pg_class c ON c.oid=co.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace
      LEFT JOIN pg_class parent ON parent.oid=co.confrelid WHERE n.nspname='public' AND co.contype IN ('p','u','f')"""))
    business = {"orders": ["orders", "sagas", "saga_history", "saga_receipts"],
                "payments": ["payments", "payment_receipts"], "stock": ["products", "stock_receipts"]}[database]
    core_width = 2 if database == "orders" else 1
    placements = {name: (i // core_width, i % core_width, database) for i, name in enumerate(business)}
    placements.update({"outbox_messages": (0, core_width, "Outbox"), "schema_migrations": (1, core_width, "Миграции"),
                       "consumer_failures": (0, core_width + 1, "DLQ"), "dead_letters": (1, core_width + 1, "DLQ")})
    assert {c["table"] for c in column_rows} == set(placements), "schema differs from documented migrations"
    entities = []
    roles = {"p": "pk", "u": "uk", "f": "fk"}
    for name, (row_number, col_number, tag) in placements.items():
        path = ("internal/outbox/schema.sql" if name == "outbox_messages" else
                "internal/deadletter/schema.sql" if name in ("consumer_failures", "dead_letters") else
                "internal/postgres/postgres.go" if name == "schema_migrations" else
                {"orders": "order-service", "payments": "payment-service", "stock": "stock-service"}[database] + "/internal/repository/saga.sql")
        attributes = []
        for column in (c for c in column_rows if c["table"] == name):
            assert not column["comment"], "preserve new SQL column comment in diagram before generating"
            attribute = {"name": column["name"], "type": column["type"]}
            keys = set()
            for constraint in (c for c in constraints if c["table"] == name and column["name"] in c["columns"]):
                keys.add(roles[constraint["kind"]])
                if constraint["kind"] == "f":
                    index = constraint["columns"].index(column["name"])
                    attribute["references"] = constraint["target"] + "." + constraint["references"][index]
            if keys:
                attribute["key"] = sorted(keys) if len(keys) > 1 else next(iter(keys))
            attributes.append(attribute)
        entities.append({"id": name, "label": name, "tag": tag, "row": row_number, "col": col_number,
                         "attributes": attributes, "sources": [source(path)]})
        if name == "dead_letters":
            entities[-1]["width"] = 330
    relationships = []
    for constraint in (c for c in constraints if c["kind"] == "f"):
        unique = any(c["kind"] in ("p", "u") and c["table"] == constraint["table"] and set(c["columns"]) == set(constraint["columns"]) for c in constraints)
        relationships.append({"id": constraint["table"] + "-" + constraint["target"], "from": constraint["table"], "to": constraint["target"],
                              "fromCardinality": "one" if unique else "many", "toCardinality": "one", "fromOptional": True})
    return {"schema_version": 1, "diagram_type": "erd", "meta": meta("saga-erd-" + database, "PostgreSQL " + database + ": полная физическая схема", "standard"),
            "layout": {"mode": "grid", "entityW": 320, "gapX": 90, "gapY": 65}, "entities": entities, "relationships": relationships,
            "cards": [{"dot": "cyan", "title": "Одна схема — одна БД", "items": ["Показаны все физические столбцы public-схемы " + database + ", включая Outbox, failures, DLQ и миграции.", "В каждой из трёх БД собственные экземпляры таблиц доставки. Идентификаторы других сервисов — обычные поля, не межбазовые FK."]},
                      {"dot": "emerald", "title": "Ключи и атомарность", "items": ["PK / FK / UK отмечены у каждого столбца. Для составных ограничений роли указаны у всех участвующих столбцов.", "dead_letters связан составным FK с consumer_failures; Outbox не имеет FK к бизнес-таблицам и сохраняется с ними явной локальной транзакцией."]}]}


def main():
    global REVISION
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--revision", default="HEAD")
    args = parser.parse_args()
    REVISION = git("rev-parse", args.revision)
    candidates = [containers(), components(), sequence()] + [erd(database) for database in ("orders", "payments", "stock")]
    for candidate in candidates:
        name = Path(candidate["meta"]["output"]).stem
        target = ROOT / "docs/sources" / (name + ".json")
        target.write_text(json.dumps(candidate, ensure_ascii=False, indent=2) + "\n")
        print(target.relative_to(ROOT))


if __name__ == "__main__":
    main()
