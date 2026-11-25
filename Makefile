.PHONY: up logs build-local gateway-local frontend-local infra down reset gateway stock payment order frontend test acceptance build swagger stage1 stage2 stage3 stage4 smoke1 smoke2 smoke3 smoke4
up infra:
	docker compose up -d --build
down:
	docker compose down
reset:
	docker compose down -v
	go run ./cmd/lab -reset 1
gateway:
	docker compose up -d --build gateway
gateway-local:
	cd gateway && go run main.go
stock:
	cd stock-service && go run main.go
payment:
	cd payment-service && go run main.go
order:
	cd order-service && go run main.go
frontend:
	docker compose up -d --build frontend
frontend-local:
	cd frontend && npm run dev
logs:
	docker compose logs -f monolith gateway frontend
test:
	go test -race ./...
acceptance:
	go test -race -tags=acceptance ./order-service/...
build:
	docker compose build
build-local:
	go build ./...
	cd frontend && npm ci --no-audit --no-fund && npm run build
swagger:
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g main.go -d gateway,internal/contract,internal/config --parseInternal -o gateway/docs
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g main.go -d monolith,internal/contract --parseInternal -o monolith/docs
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g main.go -d stock-service,internal/contract --parseInternal -o stock-service/docs
	go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g main.go -d order-service,internal/contract --parseInternal -o order-service/docs
stage1:
	go run ./cmd/lab 1
stage2:
	go run ./cmd/lab 2
stage3:
	go run ./cmd/lab 3
stage4:
	go run ./cmd/lab 4
smoke1:
	go run ./cmd/smoke -stage=1 -require-trace
smoke2:
	go run ./cmd/smoke -stage=2 -require-trace
smoke3:
	go run ./cmd/smoke -stage=3 -require-trace
smoke4:
	go run ./cmd/smoke -stage=4 -require-trace

.PHONY: saga-up saga-down saga-reset saga-logs saga-demo-happy saga-demo-payment-failed saga-demo-compensation saga-demo-duplicate saga-demo-out-of-stock saga-e2e saga-acceptance saga-integration saga-zip
saga-up:
	docker compose -f compose.saga.yaml up -d --build --wait --wait-timeout 240
saga-down:
	docker compose -f compose.saga.yaml down
# Explicitly destructive: deletes ONLY this Saga project's Kafka and SQL data.
saga-reset:
	docker compose -f compose.saga.yaml down -v
saga-logs:
	docker compose -f compose.saga.yaml logs -f order-service payment-service stock-service
saga-demo-happy:
	python3 scripts/saga_demo.py happy
saga-demo-payment-failed:
	python3 scripts/saga_demo.py payment-failed
saga-demo-compensation:
	python3 scripts/saga_demo.py compensation
saga-demo-duplicate:
	python3 scripts/saga_demo.py duplicate
saga-demo-out-of-stock:
	python3 scripts/saga_demo.py out-of-stock
saga-e2e:
	python3 scripts/saga_demo.py all
	python3 scripts/saga_reliability.py all --no-start
	python3 scripts/saga_trace_check.py

.PHONY: saga-trace-check
saga-trace-check:
	python3 scripts/saga_trace_check.py
saga-acceptance:
	go test -tags=saga_assignment ./order-service/internal/domain ./payment-service/internal/domain ./stock-service/internal/domain
saga-integration:
	go test -race -tags=saga_integration,saga_assignment ./internal/outbox ./internal/deadletter ./order-service/internal/usecase ./payment-service/internal/usecase ./stock-service/internal/usecase
saga-zip:
	python3 scripts/package_saga.py

.PHONY: saga-assignment saga-assignment-stock saga-assignment-e2e
saga-assignment:
	go test -tags=saga_assignment ./...
saga-assignment-stock:
	go test -tags=saga_assignment,saga_integration ./stock-service/internal/usecase -run '^TestAssignmentReleaseRestoresInventory$$'
saga-assignment-e2e: saga-up
	python3 scripts/saga_assignment.py all

.PHONY: saga-demo-outbox saga-demo-dlq saga-reliability
saga-demo-outbox:
	python3 scripts/saga_reliability.py outbox
saga-demo-dlq:
	python3 scripts/saga_reliability.py dlq
saga-reliability:
	python3 scripts/saga_reliability.py all

.PHONY: regression-strangler
regression-strangler:
	python3 scripts/strangler_regression.py

.PHONY: saga-diagrams
saga-diagrams:
	python3 scripts/generate_saga_diagrams.py
	python3 scripts/render_saga_diagrams.py
