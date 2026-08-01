.PHONY: build test test-short test-integration lint fmt fix clean ci

ci: fmt fix lint test build


build:
	go build -o bin/server ./cmd/server/main.go

test:
	go test -race ./...

test-short:
	go test -race -short ./...

test-integration:
	go test -race -tags=integration ./... -count=1 -timeout 15m

lint:
	golangci-lint run

fmt:
	go fmt ./...

fix:
	go fix ./...

clean:
	rm -rf bin/

# Database Migrations
install-tools:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

migrate-create:
	@read -p "Enter migration name: " name; \
	migrate create -ext sql -dir internal/db/migrations -seq $$name

migrate-up:
	migrate -path internal/db/migrations -database "$$DB_DSN" up

migrate-down:
	migrate -path internal/db/migrations -database "$$DB_DSN" down -all
