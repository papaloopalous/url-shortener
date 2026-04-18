BINARY       := gateway-service
DOCKER_IMAGE := papaloopalous/gateway-service
DOCKER_TAG   ?= latest
PROTO_SRC    := proto/auth/auth.proto

.PHONY: build run test test-integration test-coverage lint fmt docker-build clean proto

build:
	go build -o bin/$(BINARY) ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./... -race -count=1 -timeout=60s

test-integration:
	go test ./... -tags=integration -race -count=1 -timeout=300s

test-coverage:
	go test ./... -race -coverprofile=coverage.txt -covermode=atomic
	go tool cover -html=coverage.txt -o coverage.html
	@go tool cover -func=coverage.txt | grep total

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .

docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

clean:
	rm -rf bin/ coverage.txt coverage.html

proto:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_SRC)