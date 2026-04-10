FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

RUN go build -o /bin/auth-service ./cmd/server

FROM istio/distroless AS runtime

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/auth-service /auth-service
COPY --from=builder /app/migrations /migrations

EXPOSE 8081 9091

ENTRYPOINT ["/auth-service"]