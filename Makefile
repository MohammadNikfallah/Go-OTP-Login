# Project variables
BINARY_NAME=Go-OTP-Login
PKG=./cmd/api
DOCS=./docs

.PHONY: all
all: build

.PHONY: build
build:
	@echo ">> Building Go binary..."
	go build -o $(BINARY_NAME) $(PKG)

.PHONY: run
run:
	@echo ">> Running Go app..."
	go run $(PKG)

.PHONY: clean
clean:
	@echo ">> Cleaning build..."
	go clean
	-del $(BINARY_NAME) 2> NUL || true

.PHONY: swagger
swagger:
	@echo ">> Generating Swagger docs..."
	swag init -g $(PKG)/main.go -o $(DOCS)

.PHONY: up
up:
	@echo ">> Starting Docker services..."
	docker compose up -d

.PHONY: down
down:
	@echo ">> Stopping Docker services..."
	docker compose down

.PHONY: migrate-up
migrate-up:
	@echo ">> Running DB migrations..."
	migrate -path ./migrations -database "postgres://postgres:1234@localhost:5433/optlogin?sslmode=disable" up

.PHONY: migrate-down
migrate-down:
	@echo ">> Rolling back DB migrations..."
	migrate -path ./migrations -database "postgres://postgres:1234@localhost:5433/optlogin?sslmode=disable" down
