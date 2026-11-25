# Практикум · Оркестрация заказа за 60 минут

Реализуйте `Saga.Start` и `Saga.Apply` в Order Service. Метод возвращает имя следующей команды и ошибку. Usecase сам сохраняет заказ, состояние Saga и команду в PostgreSQL. Payment и Stock уже читают команды из Kafka и публикуют результаты. [Заготовка](order-service/internal/domain/saga.go) · [Доменные тесты](order-service/internal/domain/saga_test.go) · [Usecase Order](order-service/internal/usecase/saga.go)

**До занятия** подготовьте Go 1.23+, Docker с Compose v2, Python 3.9+ и Make. Выполняйте команды в корне ветки `feat_saga_orchestration_seminar`. Остановите прежний стенд Strangler, если он занимает порты. Выполните `make saga-up` заранее, чтобы загрузка образов и инициализация Kafka и PostgreSQL прошли до начала практикума.

### 0–5 минут · Воспроизведите RED

```bash
go build ./...
make saga-acceptance
```

Сборка должна пройти. В выводе тестов Order найдите `TODO Saga.Start` и `TODO Saga.Apply`. На заготовке `make saga-demo-happy` возвращает HTTP 503 с `TODO Saga.Start`. Ошибка откатывает создание заказа до отправки первой команды.

### 5–15 минут · Начните оплату

`Start` вызывается при создании заказа. Из состояния Saga `PENDING` начните ожидание оплаты, верните команду `ChargePayment` и оставьте статус заказа `PENDING`. Повторный старт из любого другого состояния должен вернуть ошибку, для которой `errors.Is(err, ErrTransition)` истинно. Сохраните все поля Saga при отказе.

```bash
go test ./order-service/internal/domain -run '^TestSaga(StartsPayment|RejectsRestart)$'
```

Переходите дальше, когда оба теста проходят.

### 15–30 минут · Проведите успешный заказ

`Apply` получает тип события и причину отказа. Успешное событие приходит с пустой причиной. Обработайте две строки таблицы.

| Текущий шаг | Событие | Следующий шаг | Статус заказа | Следующая команда |
|---|---|---|---|---|
| PAYMENT_PENDING | PaymentCharged | STOCK_PENDING | PENDING | ReserveStock |
| STOCK_PENDING | StockReserved | COMPLETED | CONFIRMED | пустая строка |

```bash
go test ./order-service/internal/domain -run '^TestSagaBusinessOutcomes$/^happy$'
make saga-demo-happy
```

Скрипт пересобирает сервисы и заказывает одну единицу товара 1. Найдите `HTTP 202`, скопируйте `order_id` и `saga_id`. После ответа склада ожидаются заказ `CONFIRMED`, Saga `COMPLETED`, одно списание, ноль возвратов и резерв одной единицы. Скрипт сам проверяет три базы и выводит `SUCCESS happy`. Ниже даны запросы для ручной проверки этого заказа.

### 30–50 минут · Обработайте отказы

| Текущий шаг | Событие | Следующий шаг | Статус заказа | Что вернуть или сохранить |
|---|---|---|---|---|
| PAYMENT_PENDING | PaymentFailed | CANCELLED | CANCELLED | Сохраните причину отказа оплаты. Команда пуста. |
| STOCK_PENDING | StockReservationFailed | COMPENSATING | PENDING | Сохраните причину отказа склада. Верните RefundPayment. |
| COMPENSATING | PaymentRefunded | CANCELLED | CANCELLED | Сохраните прежнюю причину отмены. Команда пуста. |
| COMPENSATING | PaymentRefundFailed | COMPENSATION_FAILED | PENDING | Запишите причину отказа возврата. Команда пуста. |

Для любого недопустимого сочетания шага и события верните `ErrTransition` или ошибку, оборачивающую её. Все поля, включая прежнюю причину, должны сохраниться. Дубликат из Kafka usecase распознаёт до вызова `Apply`. Проверьте свои переходы командой `make saga-acceptance`. Теперь ожидается `ok` для всех трёх доменных пакетов.

### 50–60 минут · Покажите компенсацию и сдайте работу

```bash
make saga-demo-compensation
```

У товара 2 нулевой остаток. Найдите в истории `StockReservationFailed → COMPENSATING → RefundPayment`, затем `PaymentRefunded → CANCELLED`. Для сдачи покажите прошедшие `make saga-acceptance`, `SUCCESS happy` и `SUCCESS compensation`. На примере одного заказа объясните, почему для отмены после списания требуется подтверждение Payment. В базе Payment должны быть одно списание и один возврат.

Дополнительные прогоны доступны через `make saga-demo-payment-failed`, `make saga-demo-refund-failed` и `make saga-demo-duplicate`. При таймауте посмотрите последнее состояние через `curl -s "http://localhost:8080/api/v1/orders/$ORDER_ID"` и историю по командам ниже. После исправления повторите команду demo для нового заказа. По окончании остановите стенд командой `make saga-down`.

## SQL для одного заказа

Подставьте оба идентификатора из **одного** запуска `make saga-demo-happy` или `make saga-demo-compensation`. Выполняйте блоки в одном терминале из корня проекта.

```bash
ORDER_ID='вставьте order_id'
SAGA_ID='вставьте saga_id'
docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=orders_demo postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U orders -d orders -v oid="$ORDER_ID" -v sid="$SAGA_ID" <<'SQL'
SELECT o.status, s.state, s.reason
FROM orders o JOIN sagas s ON s.order_id = o.id
WHERE o.id = :'oid' AND s.id = :'sid';

SELECT message_type, state, next_command
FROM saga_history WHERE saga_id = :'sid' ORDER BY seq;
SQL
```

```bash
docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=payments_demo postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U payments -d payments -v oid="$ORDER_ID" <<'SQL'
SELECT state, charges, refunds FROM payments WHERE order_id = :'oid';
SQL

docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=stock_demo postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U stock -d stock -v oid="$ORDER_ID" <<'SQL'
SELECT status, product_id, quantity FROM stock_receipts WHERE order_id = :'oid';
SQL
```

| Прогон | Order · статус / шаг / причина | Payment · состояние / списания / возвраты | Stock · результат / товар / количество |
|---|---|---|---|
| Happy Path | CONFIRMED / COMPLETED / пусто | CHARGED / 1 / 0 | RESERVED / 1 / 1 |
| Компенсация | CANCELLED / CANCELLED / insufficient_stock | REFUNDED / 1 / 1 | REJECTED / 2 / 1 |

В истории Happy Path должны быть четыре строки в порядке `OrderCreated`, `SagaStarted`, `PaymentCharged`, `StockReserved`. Последняя строка содержит пустую следующую команду.
