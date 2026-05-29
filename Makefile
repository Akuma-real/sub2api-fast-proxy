.PHONY: test build run

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/sub2api-fast-proxy ./cmd/sub2api-fast-proxy

run:
	go run ./cmd/sub2api-fast-proxy
