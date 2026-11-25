# GoDelivery · Практикум по Orchestrated Saga

**60 минут** · Ветка `feat_saga_orchestration_seminar`

Стартовая версия оформляет заказы по схеме «оплата → резервирование». Задача
практикума — изменить работающую Saga так, чтобы склад резервировал товар до
оплаты и снимал резерв при отказе оплаты.

[Запуск](#запуск-окружения) · [User Story](#user-story) ·
[Критерии приёмки](#критерии-приёмки) · [План работы](#план-работы) ·
[Проверка PostgreSQL](#sql-для-одного-заказа)

## Запуск окружения

Понадобятся Go 1.23+, Docker Engine или Desktop с Compose v2, Python 3.9+ и Make.
Запустите Docker и выделите ему около 4 ГБ памяти. Для первой сборки потребуется
загрузить образы и зависимости. Выполните её до начала практикума. Если работает
стенд Strangler Fig, остановите его контейнеры и локальные сервисы, чтобы
освободить порты.

Запустите стенд из корня репозитория.

```bash
git switch feat_saga_orchestration_seminar
make saga-up
docker compose -f compose.saga.yaml ps -a
```

`make saga-up` собирает сервисы и ждёт готовности контейнеров. Compose создаст
топики Kafka, базы PostgreSQL и тестовые товары. Настройки для запуска уже
заданы в [compose.saga.yaml](compose.saga.yaml). Локальный `.env` не требуется.
Контейнер `kafka-init` должен завершиться с кодом `0`.

| Компонент | Адрес |
|---|---|
| Интерфейс покупателя | [http://localhost:5173](http://localhost:5173) |
| API Gateway и Swagger | [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html) |
| Jaeger | [http://localhost:16686](http://localhost:16686) |
| PostgreSQL · базы `orders`, `payments` и `stock` | `localhost:15432` |
| Kafka | `localhost:19092` |

Если запуск прервался, проверьте состояние Docker, свободные порты и логи.
После работы остановите стенд через `make saga-down`. Данные сохранятся в томах
Docker. Команда `make saga-reset` удаляет данные Kafka и PostgreSQL этого стенда.

```bash
docker compose -f compose.saga.yaml logs --tail=100
```

## User Story

### Бизнес-постановка

Сейчас GoDelivery списывает деньги до резервирования. Если склад затем сообщает,
что товара недостаточно, система возвращает деньги и отменяет заказ после
подтверждения возврата. Покупатель видит списание за товар, который магазин
не может доставить.

> **Как покупатель GoDelivery, я хочу, чтобы нужное количество товара
> резервировалось до оплаты, чтобы деньги списывались только за товар, который
> магазин может мне выделить.**

Заказ в этой версии содержит один товар и его количество. При достаточном
остатке GoDelivery резервирует товар, проводит оплату и подтверждает заказ.
При нехватке товара GoDelivery отменяет заказ без оплаты. При отказе оплаты
после успешного резервирования Stock снимает резерв, а Order ждёт подтверждения
снятия перед окончательной отменой.

## Критерии приёмки

| Критерий | Дано | Когда | Тогда |
|---|---|---|---|
| **AC-01 · Успешный заказ** | Товара достаточно, оплата проходит | Покупатель оформляет заказ | Stock уменьшает доступный остаток на заказанное количество. Только после `StockReserved` Order отправляет команду оплаты. После успешной оплаты заказ получает `CONFIRMED`. Резерв сохраняется, компенсация не запускается. |
| **AC-02 · Товара недостаточно** | Доступный остаток меньше запрошенного | Склад отклоняет резервирование | Заказ получает `CANCELLED`. Остаток сохраняется, команда оплаты не отправляется, списания и возврата нет. |
| **AC-03 · Оплата отклонена** | Резерв уже создан | Payment отклоняет оплату | Order отправляет `ReleaseStock`. Stock снимает резерв и восстанавливает доступный остаток. Списания и возврата нет. После подтверждения снятия заказ получает `CANCELLED`. |
| **AC-04 · Снятие ещё выполняется** | Оплата отклонена, Order запросил снятие | Подтверждение `StockReleased` ещё не получено | Заказ остаётся `PENDING`, Saga — `COMPENSATING`. Отмена завершается только после `StockReleased`. |

## План работы

### 0–10 минут · Воспроизведите исходный процесс и RED

```bash
go build ./...
make saga-acceptance
python3 scripts/saga_demo.py happy --no-start
```

Ожидаются успешная сборка, проходящие тесты исходной Saga и `SUCCESS happy`.
В истории `ChargePayment` стоит перед `ReserveStock`. Чтобы увидеть исходный
возврат денег при отсутствии товара, выполните
`python3 scripts/saga_demo.py compensation --no-start`.

На том же стенде запустите проверки нового требования.

```bash
make saga-assignment
make saga-assignment-stock
```

Обе команды должны показать RED. В Order первая команда пока `ChargePayment`,
при отказе склада запускается `RefundPayment`, а при отказе оплаты заказ
отменяется сразу. В Stock тест успешно создаёт резерв и получает
`stock expects ReserveStock` при попытке выполнить `ReleaseStock`.

### 10–25 минут · Изучите код и измените порядок шагов

| Где читать | Что найти |
|---|---|
| [Машина состояний Order](order-service/internal/domain/saga.go) | `Saga.Start` выбирает первую команду, `Saga.Apply` определяет переход после события. |
| [Оркестратор Order](order-service/internal/usecase/saga.go) и [репозиторий](order-service/internal/repository/saga.go) | `Create` и `Handle` сохраняют шаг Saga и команду в локальной транзакции. Повторное событие распознаётся до вызова `Apply`. |
| [Обработчик Stock](stock-service/internal/usecase/saga.go), [правило резерва](stock-service/internal/domain/saga.go) и [репозиторий](stock-service/internal/repository/saga.go) | Как блокируется товар, уменьшается остаток, сохраняется результат и ставится событие в Outbox. |
| [Payment](payment-service/internal/usecase/saga.go) и [правила оплаты и возврата](payment-service/internal/domain/saga.go) | Как бизнес-отказ превращается в событие. Возврат денег — пример отдельной компенсирующей операции. |
| [Контракт Kafka](internal/sagacontract/message.go) | Куда маршрутизируются команды. Как `order_id` и `saga_id` связывают сообщения с заказом. |

Пройдите по требованиям AC-01 и AC-02. Для каждого события определите, какой
сервис его публикует, какой шаг ждёт оркестратор и какую команду отправляет дальше.
Измените существующие переходы в `Start` и `Apply`, переиспользуя `STOCK_PENDING`
и `PAYMENT_PENDING`. Подтверждайте заказ после успешной оплаты и отменяйте при
отказе в резервировании без обращения к Payment. Сохраните обработку
недопустимых переходов.

```bash
go test -tags=saga_assignment ./order-service/internal/domain \
  -run 'TestAssignment(ReserveBeforePayment|NoPaymentWhenStockRejects)'
```

Оба теста должны пройти. Остальные ожидания найдёте в
[тестах нового бизнес-процесса](order-service/internal/domain/saga_assignment_test.go).

### 25–45 минут · Добавьте снятие резерва

1. В Order обработайте отказ оплаты после резервирования. Переведите Saga в
   `COMPENSATING`, сохраните причину отказа и отправьте `ReleaseStock`. После
   `StockReleased` завершите отмену. Команда и событие уже описаны в
   [контракте](internal/sagacontract/message.go).
2. В [обработчике Stock](stock-service/internal/usecase/saga.go) добавьте операцию
   для `ReleaseStock`. Найдите запись резерва по `order_id` и сверьте `saga_id`,
   товар и количество. Используйте существующую блокировку товара.
3. В локальной транзакции восстановите остаток по активному резерву, сохраните
   статус `RELEASED` и поставьте `StockReleased` в Outbox. Для повторной доставки
   используйте сохранённый результат, чтобы не увеличивать остаток второй раз.
   [Миграция](stock-service/internal/repository/saga_release.sql) уже добавляет
   `release_response`. Поля `request` и `response` хранят исходный результат
   резервирования. Проверку SQL и события задаёт
   [интеграционный тест Stock](stock-service/internal/usecase/saga_assignment_test.go).

Order координирует шаги, Stock владеет остатками, Payment владеет оплатой. Каждый
сервис меняет свою БД. Оставьте взаимодействие между участниками в Kafka.

```bash
make saga-assignment
make saga-assignment-stock
```

Ожидается GREEN. Исправляйте реализацию, сохраняя существующие тесты.

### 45–60 минут · Проверьте и сдайте

```bash
make saga-assignment-e2e
```

Команда пересобирает стенд и проводит реальные заказы через Gateway, Kafka и
PostgreSQL. Должны появиться `SUCCESS assignment happy`,
`SUCCESS assignment out-of-stock` и `SUCCESS assignment payment-declined`.
[Код сквозной проверки](scripts/saga_assignment.py)

Также воспроизведите Happy Path вручную. Запишите доступный остаток товара 1
из `GET /api/v1/stock/1`. Оформите заказ в интерфейсе или выполните
`POST /api/v1/orders` в Swagger с телом
`{"product_id": 1, "quantity": 1}`. Сохраните `id` заказа и `saga_id` из ответа
`202`. Проверьте `GET /api/v1/orders/{id}` в Swagger до получения `CONFIRMED` и
`COMPLETED`. До завершения асинхронных шагов заказ может оставаться `PENDING`.
Для ручного отказа оплаты добавьте поле `"demo_failure": "payment"`. Товар 2
предназначен для проверки отсутствия остатка. В выводе `make saga-logs`
найдите записи с вашим `saga_id` и проследите команды и события Order, Stock и Payment.

Для сдачи покажите результаты трёх команд `saga-assignment*` и данные одного
ручного заказа. В истории Happy Path покажите участок `StockReserved →
ChargePayment → PaymentCharged`. Для отказа оплаты объясните, почему
`PaymentFailed` запускает снятие резерва, а окончательную отмену разрешает
`StockReleased`.

`make saga-acceptance` и `make saga-demo-*` проверяют исходную последовательность.
Тесты новой последовательности запускаются с Go-тегом `saga_assignment`.

Если осталось время, добавьте тест повторной доставки `ReleaseStock`. Проверьте,
что повтор возвращает тот же результат и не увеличивает остаток.

## SQL для одного заказа

Подставьте `order_id` и `saga_id` из ответа API или строки, напечатанной
сквозным тестом. В HTTP-ответе идентификатор заказа называется `id`.
Выполняйте команды в одном терминале.

```bash
ORDER_ID='вставьте order_id'
SAGA_ID='вставьте saga_id'

docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=orders_demo postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U orders -d orders \
  -v oid="$ORDER_ID" -v sid="$SAGA_ID" <<'SQL'
SELECT o.status, s.state, s.reason
FROM orders o JOIN sagas s ON s.order_id = o.id
WHERE o.id = :'oid' AND s.id = :'sid';

SELECT message_type, state, next_command
FROM saga_history WHERE saga_id = :'sid' ORDER BY seq;
SQL

docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=payments_demo postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U payments -d payments -v oid="$ORDER_ID" <<'SQL'
SELECT state, charges, refunds FROM payments WHERE order_id = :'oid';
SQL

docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=stock_demo postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U stock -d stock -v oid="$ORDER_ID" <<'SQL'
SELECT r.status, r.product_id, r.quantity, p.available
FROM stock_receipts r JOIN products p ON p.id = r.product_id
WHERE r.order_id = :'oid';
SQL
```

| Новый процесс | Заказ / Saga | Payment | Stock |
|---|---|---|---|
| Happy Path | `CONFIRMED / COMPLETED` | `CHARGED`, 1 списание, 0 возвратов | `RESERVED`, остаток уменьшен на количество заказа |
| Товара нет | `CANCELLED / CANCELLED` | строки нет | `REJECTED`, остаток сохраняется |
| Оплата отклонена | `CANCELLED / CANCELLED` после `StockReleased` | `DECLINED`, 0 списаний и возвратов | `RELEASED`, остаток восстановлен |

`products.available` показывает текущий общий остаток. Для ручной проверки
разницы запишите его до заказа и сравните после завершения Saga. В истории
`saga_history` каждая строка связывает событие с переходом и следующей командой.
Это позволяет установить порядок шагов при асинхронной доставке.

