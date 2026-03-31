.PHONY: build test run migrate-up migrate-down docker-up docker-down clean

build:
	go build -o bin/pos ./cmd/pos
	go build -o bin/server ./cmd/server
	go build -o bin/admin ./cmd/admin

test:
	go test -v -race -count=1 ./...

run:
	go run ./cmd/server

migrate-up:
	migrate -path migrations -database "$$(grep DATABASE_URL .env 2>/dev/null | cut -d= -f2 || echo 'postgres://postgres:postgres@localhost:5432/minidb?sslmode=disable')" up

migrate-down:
	migrate -path migrations -database "$$(grep DATABASE_URL .env 2>/dev/null | cut -d= -f2 || echo 'postgres://postgres:postgres@localhost:5432/minidb?sslmode=disable')" down

docker-up:
	docker compose up -d

docker-down:
	docker compose down -v

clean:
	rm -rf bin/
	go clean -cache
