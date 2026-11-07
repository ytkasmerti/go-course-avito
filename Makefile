.PHONY: generate migrate run test test-integration lint

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package api \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml
	go generate ./...

migrate:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

run:
	go run ./cmd/trip-service

test:
	CGO_ENABLED=1 go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

test-integration:
	CGO_ENABLED=1 go test -race -count=1 -tags=integration ./internal/repository/...

lint:
	go tool golangci-lint run