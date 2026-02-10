.PHONY: all up down logs ruby go java node test clean certs certs-clean up-tls test-tls

# Default: start all services (non-TLS)
all: up

# Generate TLS certificates
certs:
	@./scripts/generate-certs.sh

# Clean certificates
certs-clean:
	@rm -f certs/*.crt certs/*.key certs/*.pem

# Start all services (non-TLS)
up:
	docker compose up --build -d

# Start all services with TLS
up-tls: certs
	MYSQL_TLS_ENABLED=true docker compose up --build -d

# Stop all services
down:
	docker compose down

# View logs from all services
logs:
	docker compose logs -f

# Start only Ruby client
ruby:
	docker compose up --build -d mysql ruby-client

# Start only Go client
go:
	docker compose up --build -d mysql go-client

# Start only Java client
java:
	docker compose up --build -d mysql java-client

# Start only Node client
node:
	docker compose up --build -d mysql node-client

# Run finite iterations (useful for CI) - non-TLS
test:
	MAX_ITERATIONS=10 docker compose up --build --abort-on-container-exit

# Run finite iterations with TLS
test-tls: certs
	MYSQL_TLS_ENABLED=true MAX_ITERATIONS=10 docker compose up --build --abort-on-container-exit

# Clean up
clean: certs-clean
	docker compose down -v --rmi local
