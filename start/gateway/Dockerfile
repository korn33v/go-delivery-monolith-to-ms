FROM golang:1.25.0-alpine AS builder
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY gateway ./gateway
RUN CGO_ENABLED=0 go build -trimpath -o /gateway ./gateway

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
WORKDIR /app
ENV ENV_FILE=/app/.env
COPY --from=builder /gateway /app/gateway
USER app
EXPOSE 8080
ENTRYPOINT ["/app/gateway"]
