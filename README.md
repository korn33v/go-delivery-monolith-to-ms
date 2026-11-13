# GoDelivery · Strangler Fig

Выберите `start/` для занятия или `solution/` для готового решения.
Внутри каждой папки находятся полный проект, `.env`, Compose и подробный
README с четырьмя лабораторными этапами. Не запускайте обе копии одновременно.

Быстрый запуск: откройте `solution/README.md` и начните с шага 1.
Первый этап запускается из выбранной копии командой `docker compose up -d --build`.
В Compose находятся Kafka, Prometheus, Grafana, Jaeger, Monolith, Gateway и React.
Откройте http://localhost:5173. Stock, Payment и Order запускаются локально
в отдельных терминалах по ходу лабораторных этапов.

Отличия заготовки: TODO в `order-service/handler/order.go` и
`order-service/internal/usecase/orders.go`. Этапы 1–3 уже работают.
Приёмочные тесты задания: `go test -race -tags=acceptance ./order-service/...`.
