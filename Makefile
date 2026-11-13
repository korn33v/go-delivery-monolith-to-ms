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
