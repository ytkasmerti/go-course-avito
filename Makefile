.PHONY: generate migrate run test

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
	CGO_ENABLED=1 go test -race -cover ./...

test-integration:
	CGO_ENABLED=1 go test -race -cover -tags=integration ./internal/repository/...