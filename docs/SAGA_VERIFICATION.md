# Регрессия Saga, Outbox, DLQ и Strangler Fig

Проверено 13 ноября 2026 года на macOS arm64, Go 1.27.1 и Docker Engine 29.2.1.
Контейнерная сборка использует Go 1.25.0, Kafka 3.9.1, PostgreSQL 17 и
production-сборку React/Vite в Nginx. В проекте один полностью рабочий вариант.

| Проверка | Результат |
|---|---|
| `gofmt` изменённых Go-файлов, `git diff --check` | PASS |
| `go test ./...`, `go test -race ./...` | PASS |
| `go vet ./...`, `go build ./...` | PASS |
| `go test -tags=acceptance ./order-service/...` (предыдущий семинар) | PASS |
| `make saga-acceptance` (машина состояний и участники) | PASS |
| `make saga-integration` с PostgreSQL | PASS |
| Docker Compose build + healthchecks всех компонентов | PASS |
| Happy path: CONFIRMED, CHARGED, RESERVED, charges=1 | PASS |
| Payment failure: CANCELLED, DECLINED, charges=0, нет резерва | PASS |
| Stock failure: CANCELLED, REFUNDED, charges=1, refunds=1 | PASS |
| Refund failure: COMPENSATION_FAILED, PENDING, refunds=0 | PASS |
| Повтор событий и команд через Kafka, включая новые event_id | PASS |
| Дубли не меняют историю, остаток, число списаний/возвратов | PASS |
| Rollback бизнес-операции при ошибке записи Outbox (Order/Payment/Stock) | PASS |
| Восстановление Outbox после остановки Kafka и рестарта Order | PASS |
| Publish до commit published_at: повтор того же сообщения после сбоя | PASS |
| Порядок сообщений одного Saga key, продолжение других Saga | PASS |
| Невалидный JSON → DLQ каждого из трёх участников | PASS |
| Исходные байты, key/headers и source metadata сохранены в DLQ | PASS |
| Техническая ошибка handler: три устойчивые попытки → DLQ | PASS |
| Уже изолированный offset не запускает handler при redelivery | PASS |
| Ошибка сохранения карантина не разрешает подтверждение offset | PASS |
| Заказ после плохих сообщений завершается успешно | PASS |
| Восемь конкурентных команд оплаты | PASS, одно списание |
| Перезапуск трёх участников и дальнейшая работа демонстраций | PASS |
| Роль `orders` подключается к БД `payments` | Ожидаемый отказ CONNECT |
| Две упаковки одинаковых исходников | Одинаковые SHA-256 |
| ZIP не содержит `.git`, `.env`, бинарников и временных файлов | PASS |
| Чистый запуск распакованного единого ZIP без `.env`/Git | PASS |
| Все Kafka/PostgreSQL E2E из этого ZIP после удаления учебных volumes | PASS |
| Живые Strangler этапы 1, 2, 3, 4 с проверкой остатков и одной Jaeger-трассы | PASS |
| Шесть Archify HTML: validation, delivery, strict check, Chrome browser-check | PASS |
| Gateway возвращает один X-Trace-ID при наличии trace headers у upstream | PASS |
| `make saga-trace-check`: новая трасса компенсации доступна в Jaeger и содержит четыре сервиса | PASS |

Интеграционные тесты используют изолированные SQL-схемы и fake producer для
воспроизведения сбоя публикации. E2E использует реальные Kafka producer,
consumers, PostgreSQL и HTTP Gateway. Скрипт поллит результаты и счётчики
`deliveries`; истечение времени никогда не засчитывается как успех.

Проверка чистого дистрибутива:

```bash
make saga-zip
# Распаковать dist/godelivery-saga.zip в отдельный каталог и перейти в него.
make saga-reset  # удаляет только учебные Kafka/PostgreSQL данные Saga
make saga-up
make saga-e2e
```

Ожидаются `SUCCESS happy`, `SUCCESS payment-failed`, `SUCCESS compensation`,
`SUCCESS refund-failed`, `SUCCESS duplicate`, `SUCCESS outbox`, `SUCCESS dlq`.
Текущий ZIP дополнительно запускался в отдельном Compose-проекте с новыми
Kafka/PostgreSQL volumes, без `.env` и `.git`; исходный стенд затем восстановлен.
Перезапуск и E2E выполнялись
также повторно без очистки. Входные данные определены в скрипте, ID уникальны.

`make regression-strangler` использует временную копию проекта и отдельный
`.env`; все четыре существующих `cmd/smoke -require-trace` прошли.
Между этапами монолит не пересоздаётся и не теряет map-состояние.
Пользовательский `.env` и Saga volumes сохранены. Журналы контейнерных прогонов:
`dist/strangler-regression.log`, `dist/saga-package-regression.log`; вся папка
`dist/` исключена из Git.

Диаграммы привязаны к реализации commit `6a9d152`. Сводка проверенных SHA-256
HTML, JSON и ссылок на passing receipts: [saga-diagrams.json](verification/saga-diagrams.json).
Контейнерная схема дополнительно просмотрена по снимку Chrome 1440×900;
остальные проверены автоматически. В ней сохранено одно разрешённое пересечение
маршрутов; автоматические проверки читаемости и геометрии пройдены.

Ограничения проверок: React собран контейнером; отдельной автоматизации кликов
в React нет. At-least-once остаётся: сбой после publish, до локальной отметки
даёт повтор, а бизнес-эффекты защищены receipts. Детали: [RELIABILITY.md](RELIABILITY.md).

Исправление ссылки Jaeger проверено дополнительно через реальные HTTP endpoints:
Saga Compose запускает Jaeger и экспортирует OTel на `http://jaeger:4318`.
Gateway удаляет upstream-заголовок `X-Trace-ID`, сохраняя собственный одиночный ID.
`go test -race ./gateway/...`, `go vet ./gateway/...` и `go test ./...` прошли.
