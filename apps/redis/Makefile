.PHONY: all up down logs ruby go java node test clean

# Default: start all services
all: up

# Start all services
up:
	docker-compose up --build -d

# Stop all services
down:
	docker-compose down

# View logs from all services
logs:
	docker-compose logs -f

# Start only Ruby client
ruby:
	docker-compose up --build -d redis ruby-client

# Start only Go client
go:
	docker-compose up --build -d redis go-client

# Start only Java client
java:
	docker-compose up --build -d redis java-client

# Start only Node client
node:
	docker-compose up --build -d redis node-client

# Run finite iterations (useful for CI)
test:
	MAX_ITERATIONS=10 docker-compose up --build --abort-on-container-exit

# Clean up
clean:
	docker-compose down -v --rmi local
