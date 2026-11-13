# Transactional Outbox, retry и DLQ

Эти механизмы включены только в `SAGA_ENABLED=true`. Режим Strangler Fig
с map-хранилищами и прежним Kafka consumer сохранён. Внешних сервисов не добавлено:
в каждом процессе Order/Payment/Stock работают consumer и relay, а таблицы
доставки находятся в его собственной PostgreSQL БД.

## Локальная транзакция

Usecase явно передаёт один `*sql.Tx` в бизнес-репозиторий и `Outbox.Enqueue`:

```text
BEGIN
  изменить заказ / платёж / склад
  сохранить receipt и решение
  INSERT outbox_messages (стабильный message id, topic, key, JSON, trace context)
COMMIT
```

Ошибка любой записи откатывает всё. `202` возвращается после commit заказа
и первой команды, публикация больше не находится на пути HTTP-запроса.
Handler входящего Kafka сообщения возвращает успех после локального commit;
только затем consumer фиксирует исходный offset.

`internal/outbox/relay.go` выбирает готовую запись через `FOR UPDATE SKIP LOCKED`,
отправляет её существующим Kafka producer и после broker acknowledgement
отмечает `published_at`. Блокируется только строка Outbox, бизнес-транзакция
не удерживается во время сетевого вызова. Публикация ограничена пятью секундами;
локальная транзакция relay имеет отдельный более длинный таймаут.

Неотправленный предшественник блокирует последующие сообщения того же key
(`saga_id`), но не другие Saga. Ошибки публикации сохраняются в `attempts`,
`last_error`, `next_attempt_at`. Backoff: 0.5, 1, 2, 4, 8 секунд, затем 8 секунд.
Relay повторяет доставку без ограничения числа попыток и восстанавливается
из БД после рестарта. Он не отправляет временный сбой Kafka в DLQ.

Если процесс упал после успешного publish, но до commit `published_at`, сообщение
будет отправлено повторно. Стабильный `event_id`, receipts и ограничения БД
защищают бизнес-эффекты. Это at-least-once, не exactly-once. `trace_context`
восстанавливается перед отправкой, поэтому асинхронная публикация сохраняет
OTel parent context, не пряча SQL-транзакцию в context.

## Ошибки потребителя и DLQ

`internal/messageerror` помечает постоянные ошибки, сохраняя `errors.Is/As`:
неверный JSON/контракт/версия, другой тип команды, конфликт ключа и недопустимый
переход. Они изолируются после первой попытки. Отказ оплаты, недостаток товара
и отказ возврата — ожидаемые бизнес-результаты, для них публикуются обычные события.

Техническая ошибка обработчика повторяется с backoff. Счётчик по
`consumer_group + topic + partition + offset` хранится в `consumer_failures`
и переживает перезапуск. После `SAGA_MAX_ATTEMPTS` попыток (по умолчанию 3)
транзакция сохраняет `dead_letters`, исходящее сообщение DLQ в Outbox и статус
`QUARANTINED`. Только после её commit разрешён commit исходного offset.
Успешный повтор переводит прежнюю ошибку в `RESOLVED`.

Если БД недоступна и нельзя сохранить даже карантин, offset удерживается.
Карантин не теряется при недоступной Kafka: relay отправит его позднее.
После сбоя между commit карантина и commit Kafka offset повторная доставка
проверяет `IsQuarantined` и не вызывает бизнес-обработчик снова.

| Исходный топик | DLQ |
|---|---|
| `saga.payment.commands.v1` | `saga.payment.commands.v1.dlq` |
| `saga.stock.commands.v1` | `saga.stock.commands.v1.dlq` |
| `saga.events.v1` | `saga.events.v1.dlq` |

Топики создаются автоматически. Envelope v1 содержит исходные key/value/headers
в base64 (включая невалидный UTF-8), исходные topic/partition/offset/time,
consumer group, service, error, reason, attempts и failed_at.
ID карантина детерминирован по группе и исходному offset; одинаковая запись
не создаёт вторую запись БД/Outbox. Возможны физические дубли в самом DLQ после
неопределённого исхода publish, поэтому его подписчики тоже должны дедуплицировать.

## Демонстрации и регрессия

```bash
make saga-up
make saga-demo-outbox
make saga-demo-dlq
make saga-e2e
make saga-integration
make regression-strangler
```

`saga-demo-outbox` останавливает Kafka, создаёт заказ с HTTP 202, проверяет
сохранённую первую команду и неуспешную попытку relay. Затем останавливает Order,
восстанавливает Kafka/Order и проверяет полную компенсацию. Восстановление
контейнеров находится в `finally`; существующие volumes не удаляются.

`saga-demo-dlq` отправляет невалидный JSON каждому участнику, проверяет
локальный карантин, доставку в настоящий Kafka DLQ и сохранение исходных байтов.
Технический отказ вызывается временным PostgreSQL trigger только для уникального
тестового `order_id`; после трёх попыток проверяются DLQ и отсутствие списания.
Trigger удаляется в `finally`. Следующий нормальный заказ подтверждается:
испорченное сообщение не блокирует партицию. Политика по умолчанию — три попытки;
при ручном изменении `SAGA_MAX_ATTEMPTS` адаптируйте ожидание этого сценария.

`saga-integration` проверяет бизнес-транзакции, rollback Outbox, восстановление
relay, порядок сообщений одного key, неоднозначный publish и DLQ. Каждый тест
создаёт собственную временную SQL-схему: работающие relay не могут прочитать
тестовые сообщения. Схемы удаляются после проверки.

`regression-strangler` временно останавливает Saga, копирует исходники в
временный каталог с отдельным `.env`, запускает все четыре прежних этапа
с настоящими Kafka и Jaeger. Используются существующие `cmd/lab` и `cmd/smoke
-require-trace`. После завершения временные контейнеры удаляются, Saga
восстанавливается с прежними volumes. Пользовательский `.env` не изменяется.
Журнал — `dist/strangler-regression.log`, исключённый из Git.

## Наблюдение и разбор

Например, очередь Order:

```bash
docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=orders_demo postgres psql -h 127.0.0.1 -U orders -d orders -c 'SELECT id,topic,attempts,published_at,last_error FROM outbox_messages ORDER BY seq;'
docker compose -f compose.saga.yaml exec -T -e PGPASSWORD=payments_demo postgres psql -h 127.0.0.1 -U payments -d payments -c 'SELECT * FROM consumer_failures; SELECT id,envelope FROM dead_letters;'
docker compose -f compose.saga.yaml exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server kafka:9092 --topic saga.payment.commands.v1.dlq --from-beginning
```

Для ручного повтора сначала исправляют причину и сверяют состояние Saga/участника.
Из envelope декодируют `original_value`, `original_key`, `original_headers` и
публикуют в `source_topic`. Новый Kafka offset не имеет статуса карантина старого
offset; уже выполненные операции защищены прежним `event_id` и бизнес-ключами.
Автоматического replay нет, чтобы не повторять неисправимое сообщение бесконечно.

## Миграции и ограничения

`postgres.Open` последовательно применяет версии: 1 — существующая схема сервиса,
2 — Outbox, 3 — failures/DLQ. Миграции добавочные и сериализуются advisory lock;
не требуется удалять БД, Kafka или заказы для обновления стенда. Уже потерянные
намерения старой версии без Outbox автоматически не восстанавливаются.

Остались учебные ограничения: один брокер, без production retention/архивации
Outbox/DLQ, без автоматической сверки зависших Saga и HTTP idempotency key.
После карантина важного результата Saga может требовать ручного разбора;
DLQ не доказывает успешную компенсацию и не меняет статус заказа сама по себе.
Начальный запуск сервиса по-прежнему ждёт готовности Kafka; при последующем
сбое брокера уже работающий Order может продолжать сохранять заказы.
