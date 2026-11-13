# Проверка демонстрации Saga

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
| Восстановление решения из БД после ошибки publish и нового экземпляра usecase | PASS |
| Восемь конкурентных команд оплаты | PASS, одно списание |
| Перезапуск трёх участников и дальнейшая работа демонстраций | PASS |
| Роль `orders` подключается к БД `payments` | Ожидаемый отказ CONNECT |
| Две упаковки одинаковых исходников | Одинаковые SHA-256 |
| ZIP не содержит `.git`, `.env`, бинарников и временных файлов | PASS |
| Чистый запуск распакованного единого ZIP без `.env`/Git | PASS |
| Все Kafka/PostgreSQL E2E из этого ZIP после удаления учебных volumes | PASS |

Интеграционные тесты используют PostgreSQL и fake producer для точного
воспроизведения ошибки публикации. E2E использует реальные Kafka producer,
consumers, PostgreSQL и HTTP Gateway. Скрипт поллит результаты и счётчики
`deliveries`; истечение времени никогда не засчитывается как успех.

Проверка чистого дистрибутива:

```bash
make saga-zip
# Распаковать dist/godelivery-saga.zip в отдельный каталог и перейти в него.
make saga-reset  # удаляет только учебные Kafka/PostgreSQL данные Saga
make saga-up
python3 scripts/saga_demo.py all --no-start
```

Ожидаются `SUCCESS happy`, `SUCCESS payment-failed`, `SUCCESS compensation`,
`SUCCESS refund-failed`, `SUCCESS duplicate`. Перезапуск и E2E выполнялись
также повторно без очистки. Входные данные определены в скрипте, ID уникальны.

Ограничения проверок: прежние сценарии Strangler покрыты существующими
автоматическими тестами; полный живой проход четырёх этапов миграции с Jaeger
в этой сессии не запускался. React собран контейнером; отдельной автоматизации
кликов в браузере нет. Окно DB commit → Kafka publish сознательно сохранено
для следующей темы Outbox; тестирование не превращает его в exactly-once.
