.PHONY: build test lint fmt fix clean all

all: fmt fix lint test build

build:
	go build -o bin/server ./cmd/server/main.go

test:
	go test -v ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...

fix:
	go fix ./...

clean:
	rm -rf bin/
