# HTTP и Kafka контракты Saga v1

HTTP проходит через существующий Gateway `:8080`. При `SAGA_ENABLED=false`
прежние API и события `order-created`/`payment-completed` работают как раньше.
`compose.saga.yaml` включает Saga во всех трёх сервисах и маршрутизирует запросы
в Order/Stock. Межсервисных HTTP-запросов в процессе Saga нет.

## HTTP

Swagger UI: `http://localhost:8080/swagger/index.html`,
`http://localhost:8083/swagger/index.html`. Сгенерированные JSON/YAML:
`gateway/docs/`, `order-service/docs/`. Полная спецификация Saga, включая
`/ready`: `docs/saga.openapi.yaml`. Схема `SagaOrder` расширяет прежний DTO
заказа; старый режим возвращает прежние поля. В режиме Saga:

```http
POST /api/v1/orders
Content-Type: application/json

{"product_id":2,"quantity":1}
```

Ответ `202 Accepted`:

```json
{"id":"order-...","product_id":2,"quantity":1,"amount_cents":9999000,"status":"PENDING","owner":"order-service","saga_id":"saga-...","saga_state":"STOCK_PENDING"}
```

`GET /api/v1/orders/{id}`: `200` с тем же DTO и актуальными `status`,
`saga_state`, необязательным `failure_reason`. Несуществующий заказ: `404`.
Невалидный запрос: `400`; техническая ошибка БД/записи Outbox: `503`.
`202` означает, что заказ и первая команда атомарно сохранены в локальной БД.
При недоступной Kafka уже запущенный Order продолжает принимать заказы;
relay отправит сохранённые команды после восстановления брокера.
Повтор POST создаёт новый заказ: HTTP
idempotency key в этой демонстрации не реализован.

Товары 1 и 2 стоят 9999000 копеек. Количество: 1..10. В Order есть маленький
локальный каталог цен; Stock владеет остатками. Цена не поступает от клиента.
Товар 1 изначально имеет 1000000 единиц для повторения демонстраций, товар 2 — 0.
`GET /api/v1/stock/1` и прежнее HTTP-резервирование остаются доступны.

Опциональное поле `demo_failure`: `payment` или `refund`. Оно принимается
только при `SAGA_DEMO_FAILURES=true` (в учебном Compose включено по умолчанию).
Payment проверяет этот флаг независимо. Значение `refund` влияет на возврат
в процессе с оплатой первой. В заказах нового процесса возврат не запускается.
Чтобы отключить демо-отказы:
`SAGA_DEMO_FAILURES=false docker compose -f compose.saga.yaml up -d`.
Это не платёжный API и не механизм удалённого управления production-сбоем.

`/health` означает работающий HTTP процесс. `/ready` трёх участников дополнительно
проверяет собственную БД; Compose предварительно ждёт Kafka и создания топиков.
Readiness не доказывает завершение назначения consumer group: E2E ждёт бизнес-результата.

## Маршрутизация Kafka

| Топик | Тип | Producer | Consumer group |
|---|---|---|---|
| `saga.payment.commands.v1` | `ChargePayment`, `RefundPayment` | Order | `saga-payment-v1` (Payment) |
| `saga.stock.commands.v1` | `ReserveStock`, `ReleaseStock` | Order | `saga-stock-v1` (Stock) |
| `saga.events.v1` | `PaymentCharged`, `PaymentFailed`, `PaymentRefunded`, `PaymentRefundFailed` | Payment | `saga-order-v1` (Order) |
| `saga.events.v1` | `StockReserved`, `StockReservationFailed`, `StockReleased` | Stock | `saga-order-v1` (Order) |

`ReleaseStock` и `StockReleased` используют те же идентификаторы и данные
операции, что и резервирование. `StockReleased` подтверждает восстановление
остатка. `RefundPayment` и события возврата применяются к Saga, которые
начались по прежней схеме до миграции.

Все топики имеют одну партицию и replication factor 1. Kafka key — `saga_id`.
Так сообщения одной Saga попадают в одну партицию. Глобального порядка между
разными топиками нет. Все команды адресованы конкретному участнику. События
сообщают факт и могут читаться другими группами, например аналитикой.
Другой экземпляр Order входит в ту же группу; аналитика должна использовать
свою группу, иначе она начнёт забирать часть сообщений у Order.

## Envelope и данные

Go DTO: `internal/sagacontract/message.go`.
Машиночитаемая схема: `docs/saga-messages.schema.json`.

```json
{
  "event_id":"saga-123:ReleaseStock",
  "saga_id":"saga-123",
  "order_id":"order-456",
  "message_type":"ReleaseStock",
  "schema_version":1,
  "created_at":"2026-11-13T10:00:00Z",
  "data":{"product_id":1,"quantity":1,"amount_cents":9999000,"demo_failure":"payment"}
}
```

`event_id` — ID сообщения, в том числе команды (единое имя упрощает envelope).
Для процесса с одним платежом/резервом/возвратом ID детерминирован:
`saga_id:message_type`. Новая бизнес-операция требует нового заказа/Saga.
`created_at` — UTC RFC3339, `schema_version` — строго 1. Новую несовместимую
версию нужно вводить явно с новым контрактом/топиком и миграцией consumers.

В каждой команде и событии передаётся неизменяемая тройка `product_id`,
`quantity`, `amount_cents`; это помогает проверить корреляцию и конфликт ключа.
`demo_failure` переносится по цепочке неизменным. Необязательное `reason`
появляется в отрицательных событиях: `demo_payment_declined`,
`insufficient_stock`, `demo_refund_declined`. Следующая команда не наследует
`reason`: причину отказа хранит Saga в Order.

PaymentCharged означает локально подтверждённое списание; PaymentFailed —
бизнес-отказ без списания; PaymentRefunded — подтверждённый возврат;
PaymentRefundFailed — бизнес-отказ возврата, деньги остаются списанными.
StockReserved означает сохранённый резерв; StockReservationFailed — отказ
без списания товара. Сбой SQL/сети не публикуется как бизнес-отказ.

## Подтверждение и повторы

`internal/bus.ConsumeReliable` вызывает `FetchMessage`, последовательный handler,
затем `CommitMessages` с `CommitInterval=0`. Handler фиксирует бизнес-эффект,
receipt и исходящее сообщение в одной локальной транзакции, возвращает `nil`.
Kafka offset подтверждается после этого commit; отдельный relay доставляет Outbox.
Ошибки обрабатывает устойчивая политика retry/DLQ, описанная ниже.
Прежний `Consume` в режиме Strangler сохраняет прежнее поведение.

Order блокирует строку Saga через `FOR UPDATE`, проверяет корреляцию и
`saga_receipts`. Уникальный `(saga_id,message_type)` отсекает также повтор факта
с новым ID. Первая обработка атомарно сохраняет состояние, историю, решение и Outbox.
Повтор не вызывает `Saga.Apply` и не создаёт вторую запись Outbox с тем же ID.

Payment блокирует платёж по `order_id`, хранит результат каждой операции
`(order_id,message_type)`. Stock блокирует товар и хранит результат резерва
по `order_id`. Результат снятия резерва хранится в `release_response`.
При снятии статус становится `RELEASED`. Бизнес-эффект и результат фиксируются одной локальной транзакцией.
Повтор использует сохранённое событие, включая прежние ID и timestamp.
Другие параметры с тем же ключом отклоняются. Счётчик `deliveries` растёт при
обработке дубля, а `charges`, `refunds`, количество товара и история не меняются.

Это at-least-once обработка с идемпотентными операциями. Настройки producer
`RequiredAcks=all` и повторная отправка не создают общей exactly-once гарантии
для Kafka и PostgreSQL. В single-broker стенде нет устойчивости к потере диска.

В `outbox_messages` каждой БД сообщение хранится до подтверждения Kafka.
Relay использует `FOR UPDATE SKIP LOCKED`, сохраняет порядок одного `saga_id`,
повторяет публикацию с backoff и продолжает после рестарта. Контекст OTel
сохраняется вместе с сообщением. Сбой после publish, до отметки `published_at`
может дать дубль; идемпотентность остаётся обязательной.

Для каждого исходного топика автоматически создаётся `<topic>.dlq`.
Постоянная ошибка контракта/перехода изолируется сразу. Техническая ошибка
обработчика повторяется до `SAGA_MAX_ATTEMPTS` (по умолчанию 3), с сохранением
счётчика в `consumer_failures`. Бизнес-отказ оплаты/склада/возврата — обычное
событие, он не попадает в DLQ. При недоступной БД исходный offset не
подтверждается: сначала необходимо сохранить локальный результат или карантин.

Карантин атомарно сохраняет `dead_letters` и Outbox для DLQ. После этого
consumer может подтвердить исходный offset и обработать следующее сообщение,
даже если брокер временно недоступен для отправки в DLQ. При redelivery уже
изолированного offset handler пропускается. DLQ envelope сохраняет исходные
байты в base64, key, headers, topic/partition/offset/group, ошибку и число попыток.
Контракт: `docs/dlq.schema.json`. Подробности и демонстрации: [RELIABILITY.md](RELIABILITY.md).

Первичные источники: [Kafka delivery semantics](https://kafka.apache.org/39/design/design/),
[kafka-go explicit commits](https://github.com/segmentio/kafka-go#explicit-commits).
