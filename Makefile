.PHONY: build test test-short test-integration lint fmt fix clean ci loadtest

ci: fmt fix lint test build

build:
	go build -o bin/server ./cmd/server

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

# Concurrent-SSH-session load test against an already-running server. Every run
# registers fresh accounts, so PREFIX must change between runs on the same
# database or registration fails with "username taken".
# Override any flag: make loadtest SESSIONS=500 PREFIX=r2 HOLD=60s
loadtest:
	go run ./cmd/loadtest -addr $(or $(ADDR),127.0.0.1:6969) -sessions $(or $(SESSIONS),200) \
		-hold $(or $(HOLD),60s) -ramp $(or $(RAMP),10s) -journey $(or $(JOURNEY),menu) \
		-prefix $(or $(PREFIX),load)

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
