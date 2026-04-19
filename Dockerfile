FROM golang:1.26.2-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /gateway-service ./cmd/server

FROM istio/distroless

COPY --from=builder /gateway-service /gateway-service

EXPOSE 8080

ENTRYPOINT ["/gateway-service"]