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

Для преподавателя доступны [сценарий занятия на 90 минут](SEMINAR_GUIDE.md),
[контракты HTTP и Kafka](docs/SAGA_CONTRACTS.md) и
[сценарии проверки Outbox и DLQ](docs/RELIABILITY.md).

## Учебные материалы и исходный стенд Strangler Fig

Следующий раздел относится к предыдущему семинару. Для практикума по Saga
используйте [запуск окружения выше](#запуск-окружения).

Учебная цель: заменить части монолита по одному маршруту, сохранив контракт
клиента. После каждого этапа покупка работает через тот же API Gateway.
Четыре лабораторных этапа рассчитаны примерно на 2–3 часа вместе с заданием.

## Что потребуется

- Docker Engine/Desktop с Docker Compose v2 и примерно 4 ГБ доступной памяти.
- Go 1.23 или новее для локальных Stock/Payment/Order, тестов и команд этапов.
- Для первого этапа достаточно Docker и браузера: Go и Node.js работают внутри
  build-контейнеров. Node.js 20.19+ или 22+ и npm на хосте нужны только для
  необязательной локальной разработки React.
- Браузер. `make` удобен, но все его команды ниже даны также напрямую.
- На Windows: Docker Desktop/WSL2; удобно запускать Go и npm из одной среды WSL.

Первый запуск требует интернета для образов, Go-модулей и npm-пакетов.
Swagger уже сгенерирован; устанавливать генератор для запуска не требуется.

## Решения для этой лабораторной

В отличие от общих требований других семинаров курса здесь используются Gin,
хранилища в памяти и постепенный локальный запуск извлекаемых сервисов.
PostgreSQL, Redis и gRPC здесь не нужны.

`docker-compose.yaml` запускает **семь компонентов**: Kafka в KRaft,
Prometheus, Grafana, Jaeger, Monolith, Gateway и React (статическая сборка
в Nginx). Stock, Payment и Order студент запускает в отдельных терминалах.
Для запуска первого этапа достаточно `docker compose up -d --build`.
Логи Gateway и Monolith можно независимо смотреть через `docker compose logs -f`.
Frontend собирается через Vite при создании образа; Nginx отдаёт каталог `dist`.

Один Go-модуль упрощает зависимости учебного репозитория. Это не общая база:
каждый процесс создаёт собственные `map` и `sync.Mutex`. Даже Monolith и Order
используют разные пакеты репозиториев. Общие пакеты содержат инфраструктуру и
форматы сообщений. HTTP-адаптеры вызывают use case; use case зависит от портов
domain; `main.go` вручную собирает зависимости. Контекст нужен для отмены и OTel,
в нём не скрываются базы или транзакции.

Структура одной копии:

```text
docker-compose.yaml
.env
go.mod / go.sum
Makefile
README.md
frontend/             Dockerfile, nginx.conf, .dockerignore, package.json, src/, public/
gateway/              Dockerfile, main.go, router/, docs/
monolith/             main.go, Dockerfile, internal/, docs/
stock-service/        main.go, internal/, docs/
payment-service/      main.go, internal/
order-service/        main.go, handler/order.go, internal/, docs/
internal/             config, contract, bus, observability, httpapi, server, stockclient
infra/                Prometheus и provisioned Grafana dashboard/data sources
cmd/lab/              безопасное изменение флагов по этапам
cmd/smoke/            проверка действующей системы и сквозного trace ID
```

## Карта портов и проверки

| Компонент | Адрес | Swagger / назначение |
|---|---|---|
| React | http://localhost:5173 | Пользовательская покупка |
| Gateway | http://localhost:8080 | http://localhost:8080/swagger/index.html |
| Monolith | http://localhost:8081 | http://localhost:8081/swagger/index.html |
| Stock | http://localhost:8082 | http://localhost:8082/swagger/index.html |
| Order | http://localhost:8083 | http://localhost:8083/swagger/index.html |
| Payment | http://localhost:8084 | Только технические `/health` и `/metrics` |
| Kafka | localhost:19092 | Адрес для локальных Go-процессов |
| Prometheus | http://localhost:9090 | `/targets`, запросы метрик |
| Grafana | http://localhost:3000 | Логин `admin`, пароль `admin` |
| Jaeger | http://localhost:16686 | UI трассировок |
| Jaeger OTLP HTTP | http://localhost:4318 | Приём трассировок, не UI |

У всех Go-компонентов есть `/health` и `/metrics`. `/health` означает, что HTTP
процесс запущен; это не проверка подключения Kafka consumer к группе.
Swagger JSON находится по `/swagger/doc.json`; статические копии JSON/YAML —
в `docs/`. `gin-swagger` обслуживает маршрут `/swagger/*any`.
У Payment нет пользовательского HTTP API, поэтому отдельного Swagger ему не нужно.

## Перед началом

Из папки распакованного архива выберите одну копию:

```bash
cd go-delivery-monolith-to-ms
test -f .env || cp env.example .env
```

Предварительно устанавливать Go- и npm-зависимости на хост не нужно:
Dockerfile Gateway/Monolith выполняет `go mod download`, а Dockerfile React —
`npm ci` и `npm run build`. Для этапов 2–4 установите Go на хосте.

Если переходите с предыдущей версии стенда, сначала остановите старые локальные
Gateway и React через Ctrl+C: контейнерам нужны те же порты 8080 и 5173.
Для повторения всех этапов с чистыми данными используйте раздел полного сброса;
он также требует остановить локальные Stock/Payment/Order.

Используйте именно `http://localhost:5173`, а не `127.0.0.1:5173`: CORS разрешает
значение `FRONTEND_ORIGIN` из `.env`. Frontend обращается только к
`http://localhost:8080`. В нём нет Vite proxy и прямых API-запросов к сервисам.
Ссылки в UI отдельно открывают Swagger и Jaeger в новой вкладке.

## Связь контейнеров и локальных сервисов

| Откуда | Куда | Адрес |
|---|---|---|
| Браузер (React выполняется здесь) | Gateway | `http://localhost:8080` |
| Gateway в Docker | Monolith в Docker | `http://monolith:8081` |
| Gateway / Monolith в Docker | Stock на хосте | `http://host.docker.internal:8082` |
| Gateway в Docker | Order на хосте | `http://host.docker.internal:8083` |
| Gateway / Monolith в Docker | Jaeger | `http://jaeger:4318` |
| Локальные Stock/Order/Payment | Kafka / Jaeger | `localhost:19092` / `localhost:4318` |
| Prometheus в Docker | Gateway | `gateway:8080` |

Compose переопределяет внутренние URL через `environment`, сохраняя в `.env`
host-адреса для локальных сервисов и `cmd/lab`. Frontend не получает Docker DNS
имена: JavaScript исполняется в браузере и продолжает обращаться на порт 8080.
Nginx обслуживает только React и `/health`; API-прокси в нём нет. CORS Gateway
по-прежнему разрешает `http://localhost:5173`.

После изменения React или Gateway пересоберите нужный образ:

```bash
docker compose up -d --build frontend
docker compose up -d --build gateway
```

Это не требуется при изменении четырёх флагов маршрутизации: оба Go-контейнера
читают примонтированный `.env` при каждом запросе. На первом этапе Compose ждёт
готовности Monolith перед Gateway и готовности Gateway перед Frontend.

## Необязательная локальная разработка UI / Gateway

Чтобы работать с Vite hot reload или локальным Go-процессом, сначала освободите
их порты: `docker compose stop frontend gateway`. Затем в отдельных терминалах:

```bash
cd gateway
go run main.go
```

```bash
cd frontend
npm ci
npm run dev
```

Для возврата к контейнерам остановите оба локальных процесса через Ctrl+C и
выполните `docker compose up -d --build`. В стандартном сценарии занятия
локально запускаются только Stock, Payment и Order.

## Флаги и условия переключения

| Этап | ROUTE_STOCK_TO_MICROSERVICE | ROUTE_ORDER_TO_MICROSERVICE | ASYNC_PAYMENT_ACTIVE | MONOLITH_STOCK_BRIDGE_ACTIVE |
|---|---|---|---|---|
| 1. Монолит | false | false | false | false |
| 2. Склад | true | false | false | true |
| 3. Оплата | true | false | true | true |
| 4. Заказы | true | true | true | true |

Меняйте этап командами `go run ./cmd/lab N` или `make stageN` из корня копии.
Флаги перечитываются из общего `.env` при каждом запросе. Они меняются без
перезапуска Gateway или Monolith и без потери их данных. Не редактируйте файл
одновременно с выполнением команды или с незавершённой покупкой.

Скрипт пишет `.env` на месте. Это сохраняет inode файла, примонтированного в
Monolith и Gateway. Некоторые редакторы заменяют файл через rename: после такой правки
контейнер может видеть старый файл. Для занятия пользуйтесь `cmd/lab`.
Остальные настройки (URL, порты, exporter, начальный остаток) читаются при старте.
Для них переменные среды процесса имеют приоритет над `.env`. При наличии
файла четыре флага маршрутизации берутся из него, чтобы Docker и локальные
процессы видели одни значения. `ENV_FILE` позволяет явно указать абсолютный путь.
Если физического файла нет, приложение может стартовать из переменных среды:
для этого должны быть заданы все четыре флага. В таком режиме автоматического
перечитывания host-файла нет; для лабораторного переключения используйте штатный
Compose с bind mount. Команда `cmd/lab` требует доступный для записи `.env`.

## Шаг 1. Поставить Gateway перед работающим монолитом

**User Story.** Как команда сопровождения, мы хотим направить клиента через
единый шлюз, чтобы затем менять исполнителя API без изменений клиента.

В терминале 1 из корня копии:

```bash
docker compose up -d --build
docker compose ps
```

В поставляемом `.env` уже выставлен этап 1. Команда запускает также Gateway
и React; не нужно дополнительно выполнять `go run main.go` и `npm run dev`.
После проверки `ps` Gateway, Monolith и Frontend должны стать `healthy`.
В отдельных терминалах при необходимости смотрите логи:

```bash
docker compose logs -f monolith
```

```bash
docker compose logs -f gateway
```

```bash
docker compose logs -f frontend
```

Дождитесь сообщения Monolith `listening`. Kafka становится healthy не мгновенно;
при первом запуске скачивание и сборка образов также могут занять несколько минут.
Топики `order-created` и `payment-completed` создаёт приложение после готовности
Kafka; отдельный контейнер для этого не нужен.

Откройте http://localhost:5173. Должны появиться товар «Ноутбук», цена 99 990 ₽,
остаток 50 (или меньше после покупок), этап 01 и исполнители «Монолит».
Нажмите «Оформить заказ». Ответ — 201, статус `PAID`, остаток уменьшается на 1.
Локальная оплата имитирует задержку 800 мс. Stock/Payment/Order ещё не запущены.
При этом Monolith уже имеет consumer результатов, но событие создания не публикует.

Откройте Swagger Gateway и Monolith. В Gateway нажмите `Try it out` для
`GET /api/v1/stock/1`. Убедитесь, что схема ответа совпадает у обоих компонентов.
Проверочный сценарий (необязательно, требует Go на хосте),
в дополнительном терминале из корня:

```bash
go run ./cmd/smoke -stage=1 -require-trace
```

Ожидается `SUCCESS`. Сценарий сам покупает один товар; он не сбрасывает данные.
Сохраните ID одного `mono-...` заказа: он понадобится на этапе 4.

**Что прочитать в коде:** `gateway/router/router.go` — ReverseProxy и выбор
маршрута; `monolith/internal/usecase/orders.go` — локальное списание и оплата;
`internal/observability/observability.go` — HTTP metrics, серверные OTel spans.

## Шаг 2. Выделить склад и переключить один endpoint

**User Story.** Как команда каталога, мы хотим отдельно обслуживать остатки,
сохранив оформление заказов в монолите на переходный период.

1. Временно прекратите покупки. Дождитесь окончания текущего заказа.
2. Только теперь запустите Stock в новом терминале:

```bash
cd stock-service
go run main.go
```

При запуске Stock один раз получает текущий остаток из Monolith по HTTP и
сохраняет снимок в собственной map. Это учебный перенос данных. После старта
он обслуживает свою базу и не читает Monolith на каждый запрос.
Покупки должны оставаться приостановленными до следующего шага: иначе между
снимком и включением моста возникнет окно несогласованности.

3. Из корня проекта:

```bash
go run ./cmd/lab 2
go run ./cmd/smoke -stage=2 -require-trace
```

Команда сравнит снимки и откажется переключать этап, если они различаются.
При несовпадении остановите покупки, перезапустите Stock и повторите команду.
UI покажет этап 02. `GET /api/v1/stock/1` пойдёт в Stock, `POST /api/v1/orders`
останется в Monolith. Откройте Swagger Stock и проверьте GET.

Почему остатки продолжают меняться: Monolith по условию задания всегда
списывает товар локально, а временный HTTP-мост повторяет резерв в Stock.
Stock проверяет остаток и списывает товар под одним mutex. `order_id` — ключ
идемпотентности: повтор того же резерва не уменьшает остаток повторно.
Сама кнопка заказа не имеет HTTP idempotency key; новый POST — новая покупка.

Просто направить чтение на новую независимую map недостаточно: она быстро
разойдётся с записью монолита. Мост и начальный снимок делают это ограничение
видимым. При ошибках двух записей нужна сверка; распределённой транзакции здесь нет.

**Что прочитать:** `stock-service/main.go` — начальный перенос;
`stock-service/internal/repository/memory.go` — атомарность и идемпотентность;
ветка `mode.Bridge` в сценарии Monolith — временная архитектурная связь.

## Шаг 3. Выделить оплату в асинхронный сервис

**User Story.** Как команда платежей, мы хотим независимо обрабатывать оплату
через очередь, чтобы медленная оплата не удерживала HTTP-запрос создания заказа.

Запустите Payment в новом терминале:

```bash
cd payment-service
go run main.go
```

Затем из корня:

```bash
go run ./cmd/lab 3
go run ./cmd/smoke -stage=3 -require-trace
```

Оформите заказ через UI. Monolith по-прежнему списывает товар локально и через
мост, но вместо локальной функции публикует `order-created`. Ответ — 202 и
`NEW`. Payment читает событие, имитирует оплату и публикует `payment-completed`.
Consumer Monolith меняет статус в своей map на `PAID`. React опрашивает статус
через Gateway, поэтому пользователь видит окончание процесса.

Отключите Payment через Ctrl+C, оформите ещё один заказ и убедитесь, что он
остаётся `NEW`. Запустите Payment снова: сообщение будет прочитано из Kafka.
Покупки не нужно повторять вручную. Не перезапускайте Monolith в этом упражнении.

`internal/bus/kafka.go` явно инжектирует W3C `traceparent`/`tracestate` в заголовки
Kafka и извлекает их перед созданием consumer span. Producer span и все consumer
spans остаются частью исходной HTTP-трассы, включая обратное событие оплаты.
Просто передать `context.Context` в `WriteMessages` для этого недостаточно.

Consumer использует `FetchMessage` → обработка → `CommitMessages`.
При ошибке повторно обрабатывается то же сообщение; более поздний offset
не подтверждается вместо него. Payment кеширует обработанный заказ в своей map
и может повторно отправить результат без повторной имитации списания денег.
Это защита в пределах жизни процесса, не exactly-once гарантия.

**Что прочитать:** `payment-service/internal/usecase/pay.go`, адаптер Kafka,
`monolith/internal/delivery/order.go: PaymentResult` и `internal/bus/kafka.go`.

## Swagger и ручная проверка HTTP

Все четыре UI доступны по адресам из таблицы портов после запуска соответствующего
компонента. Проверяйте реальные пути `/api/v1/...`: в Swagger нет лишнего префикса
`basePath`. Запросы `Try it out` выполняются на том же хосте, где открыт Swagger.

```bash
curl -i http://localhost:8080/api/v1/stock/1
curl -i -X POST http://localhost:8080/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"product_id":1,"quantity":1}'
# Подставьте ID из ответа:
curl -i http://localhost:8080/api/v1/orders/mono-REPLACE_WITH_REAL_ID
```

Результаты: 201 при локальной оплате; 202 при асинхронной; 400 при неверном JSON
или количестве; 404 при неизвестном товаре/заказе; 409 при нехватке товара;
502 при недоступном upstream; 503 при неопределённой внешней операции;
501 при незавершённой студенческой реализации.
Заголовок `X-Backend` показывает владельца ответа, `X-Trace-ID` — ID трассы.
После покупки в UI кнопка временно блокируется до окончательного статуса.

## Grafana и Prometheus

1. Откройте http://localhost:9090/targets. Не запущенные ещё локальные сервисы
   ожидаемо будут `DOWN`. Запущенные должны быть `UP` через несколько секунд.
2. Откройте http://localhost:3000, войдите `admin` / `admin`. При предложении
   сменить пароль можно выбрать Skip для локального стенда.
3. В Dashboards откройте **GoDelivery · Strangler Fig**. Data sources Prometheus
   и Jaeger и dashboard создаются автоматически через provisioning.
4. Сделайте несколько покупок и посмотрите RPS, p95, HTTP 5xx, счётчики Kafka
   по теме/направлению и business events. Для rate-запросов дайте Prometheus
   собрать хотя бы два scrape; небольшая выборка p95 имеет учебный характер.

Полезные PromQL:

```promql
sum by (service) (rate(http_requests_total[1m]))
histogram_quantile(0.95, sum by (le, service) (rate(http_request_duration_seconds_bucket[1m])))
sum by (service, topic, direction, outcome) (kafka_messages_total)
sum by (service, event) (business_events_total)
```

Запросы `/metrics` не попадают в HTTP-счётчики, чтобы scrape сам не генерировал
учебный трафик. `/health` учитывается: так виден и технический HTTP-интерфейс
Payment. `route` — шаблон Gin с `:id`, а не каждый реальный ID;
это ограничивает кардинальность. Payment тоже имеет технические HTTP-метрики;
его рабочая нагрузка отражается прежде всего в Kafka и business counters.
Все приложения пишут JSON-логи в свой терминал; HTTP/Kafka логи содержат trace ID.

## Jaeger и проверка сквозного пути

После покупки нажмите в UI «Посмотреть трассу в Jaeger» или откройте
http://localhost:16686, выберите service `gateway`, затем Find Traces.
В течение 1–2 секунд batch exporter допишет асинхронные spans в ту же трассу.

| Этап | Ожидаемые сервисы внутри одной трассы покупки |
|---|---|
| 1 | gateway, monolith; span `payment.local` |
| 2 | gateway, monolith, stock-service |
| 3 | gateway, monolith, stock-service, payment-service |
| 4 | gateway, order-service, stock-service, payment-service |

На этапах 3–4 найдите spans `order-created publish`, `order-created process`,
`payment-completed publish`, `payment-completed process`. Общий trace ID
доказывает перенос контекста через оба Kafka-сообщения, а не просто наличие
отдельных трасс у разных приложений. HTTP-поллинг статуса создаёт отдельные
трассы; исходную покупку ищите по ID из ответа POST.
`cmd/smoke -require-trace` автоматически ждёт появления всех нужных сервисов
в **одной** трассе через Jaeger Query API и возвращает ошибку, если контекст потерян.

## Тесты, сборка, остановка и чистый повтор

```bash
make test          # go test -race ./...; требует C compiler для race detector
make acceptance    # проверка студенческого use case (до решения должна падать)
make build         # Docker build Monolith, Gateway и React
make build-local   # необязательно: Go build и сборка React на хосте
make up            # docker compose up -d --build
make logs          # логи Monolith, Gateway и React
make swagger       # заново генерирует четыре комплекта docs из комментариев
make smoke4        # требует работающую систему этапа 4 и Jaeger
```

Без C compiler можно выполнить `go test ./...` и
`go test -tags=acceptance ./order-service/...`; гонки в этом случае не проверяются.
Конкурентный тест склада запускает 60 резервирований при остатке 7 и проверяет,
что успешны ровно 7 и остаток не отрицательный. Другие тесты проверяют повторный
резерв, переключение маршрутов, чтение старых ID, CORS, сохранение OTel headers,
выбор локальной/Kafka оплаты и отсутствие повторной оплаты при сбое публикации.

Для полной очистки сначала нажмите Ctrl+C **во всех локальных Stock/Payment/Order**,
затем из корня выбранной копии:

```bash
docker compose down -v
go run ./cmd/lab -reset 1
docker compose up -d --build
```

Gateway и React снова запустятся вместе с Compose. Далее повторите лабораторные
этапы. `make reset`
выполняет первые две команды, но не может остановить процессы в ваших терминалах.
Сброс удаляет учебную историю Kafka. У Kafka нет выделенного постоянного volume:
её журнал живёт в файловой системе контейнера и удаляется с ним. Для обычной
остановки достаточно Ctrl+C локальных процессов и `docker compose down`.

## Ошибка `.env not found` при запуске
+
+`env_file` передаёт переменные окружения контейнеру; физический `.env` для
+перечитывания флагов подключён отдельно через bind mount. Загрузчик теперь
+поддерживает и окружение без физического файла. В Dockerfile Monolith/Gateway
+явно указан `ENV_FILE=/app/.env`.
+
+Если файл потерялся при распаковке, в корне выбранной копии создайте его из
+не скрытого шаблона `env.example`. Уже существующий файл сохраняется:
+
+```bash
+test -f .env || cp env.example .env
+chmod 644 .env
+docker compose build --no-cache monolith gateway
+docker compose up -d
+docker compose logs --tail=100 monolith gateway
+```
+
+Проверьте, что `.env` — обычный файл, а не каталог. В bind mount отключено
+автоматическое создание отсутствующего пути. Права 644 позволяют пользователю
+`app` внутри контейнера читать учебную конфигурацию. Приложения не запускаются
+от root. `dependency monolith failed to start` означает, что зависимый Gateway
+не дождался запуска Monolith; первичная ошибка находится в логах Monolith.
+Пересоздание Monolith очищает его in-memory состояние; новый проход занятия
+начинайте с чистого этапа 1 по инструкции сброса, если уже проходили миграцию.
+
+## Разбор типичных проблем

- **Kafka connection refused:** дождитесь healthy и `listening` Monolith;
  локальные сервисы используют `localhost:19092`, Docker — `kafka:9092`.
  Kafka возвращает адреса из `advertised.listeners`: одного проброса порта мало.
- **Gateway/Monolith не достигают локального сервиса:** Stock и Order должны
  слушать `:8082` / `:8083` на всех интерфейсах. Контейнеры обращаются к ним
  через `host.docker.internal`; на Linux задан `host-gateway`. Проверьте firewall/WSL,
  не ограничивайте bind этих сервисов адресом 127.0.0.1.
- **CORS в браузере:** адрес UI должен быть `http://localhost:5173`.
  Не открывайте `index.html` через `file://`.
- **Порт занят:** остановите старую копию стенда. Не запускайте локальные
  Gateway/React или второй Monolith, если их контейнеры уже работают в Compose.
- **Stock snapshot differs:** не покупайте во время перехода; перезапустите Stock
  до включения этапа 2. Нельзя перезапускать только один сервис, теряя его map,
  и считать, что остальная система автоматически восстановит данные.
- **Статус NEW не меняется:** проверьте Payment и consumer владельца заказа,
  затем события и trace ID в их терминалах. `/health` сам не доказывает готовность группы.
- **Нет трассы:** проверьте OTLP порт 4318, `OTEL_ENABLED=true`, дайте exporter
  несколько секунд. UI Jaeger работает на другом порту — 16686.

## Границы учебной модели

Ниже описан исходный режим **Strangler Fig**, `SAGA_ENABLED=false`.
В режиме Saga данные постоянны, а доставка использует Outbox и DLQ;
его гарантии и ограничения описаны в [RELIABILITY.md](docs/RELIABILITY.md).

Состояние приложений теряется при завершении процесса. Кеш обработанных
платежей не переживает рестарт, сообщения могут доставляться повторно.
Ошибочное сообщение намеренно блокирует consumer с повтором и логированием;
для промышленной реализации нужна политика DLQ/poison messages.

Резерв склада, сохранение заказа и публикация в Kafka не являются одной
транзакцией. `PAYMENT_UNKNOWN`/`STOCK_UNKNOWN` показывают неопределённость,
а не притворяются успешным откатом. При ошибке ответа резервирования новый
Order Service может не знать о принятом складе резерве: требуется сверка по ID
и, в реальной системе, постоянное хранение намерения/идемпотентный повтор.
При публикации с неизвестным исходом нельзя автоматически возвращать товар:
Kafka уже могла принять событие, а оплата — завершиться.

Следующее развитие: PostgreSQL Database-per-Service, transactional outbox,
устойчивая дедупликация платежей, saga компенсации, HTTP idempotency key,
безопасный перенос истории и обратная миграция для отката. В этом стенде нет
настоящего платёжного провайдера, пользовательских аккаунтов и авторизации.
Инфраструктурные порты Compose опубликованы на loopback; локальные Go-сервисы
слушают все интерфейсы, чтобы контейнер мог обращаться к складу.

## Вопросы для защиты

1. Что именно здесь делает Gateway частью Strangler Fig? Почему общий контракт
   клиента важнее одновременного переписывания всех внутренних компонентов?
2. Почему после переключения GET остатков нужен перенос данных и временный мост?
3. Почему чтение остатка перед созданием заказа не защищает от overselling?
4. Где в коде проходят границы владения данными и Composition Root?
5. Что означает 202 и как frontend узнаёт, что асинхронная оплата закончилась?
6. Что потеряется, если заменить `c.Request.Context()` на `context.Background()`?
7. Почему `ReadMessage` с автоматическим commit неудобен для этого обработчика?
8. Какая авария остаётся между резервом, map заказа и Kafka? Как её решит outbox,
   и какую отдельную проблему всё ещё нужно будет решать с резервом склада?
9. Почему map платежей не даёт exactly-once после перезапуска?
10. Почему после извлечения заказов нельзя просто вернуть все флаги в false?

## Первичные материалы

- [Лекции КПО](https://kert0n.github.io/KPO/lectures/01)
- [Strangler Fig Application, Martin Fowler](https://martinfowler.com/bliki/StranglerFigApplication.html)
- [Kafka 3.9 Docker](https://kafka.apache.org/39/getting-started/docker/)
- [kafka-go: producer, reader и explicit commits](https://github.com/segmentio/kafka-go)
- [OpenTelemetry: propagation](https://opentelemetry.io/docs/concepts/context-propagation/)
- [gin-swagger](https://github.com/swaggo/gin-swagger)
- [Jaeger all-in-one и OTLP](https://www.jaegertracing.io/docs/1.76/getting-started/)
