FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /gateway-service ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /gateway-service /gateway-service

EXPOSE 8080

ENTRYPOINT ["/gateway-service"]