FROM golang:1.26.2-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/analytics-service ./cmd/server

FROM istio/distroless

WORKDIR /app

COPY --from=builder /app/analytics-service .
COPY --from=builder /app/migrations ./migrations

EXPOSE 8083

ENTRYPOINT ["/app/analytics-service"]