.PHONY: all up down logs go test clean

# Default: start all services
all: up

# Start all services
up:
	docker compose up --build -d

# Stop all services
down:
	docker compose down

# View logs from all services
logs:
	docker compose logs -f

# Start only Go client
go:
	docker compose up --build -d kafka go-client

# Run finite iterations (useful for CI)
test:
	MAX_ITERATIONS=10 docker compose up --build --abort-on-container-exit

# Clean up
clean:
	docker compose down -v --rmi local
