# Start from a small, efficient base image
FROM golang:1.18-alpine as qtap-builder

# Set the working directory inside the container
WORKDIR /app

# Copy the go.mod and go.sum files to download dependencies
ADD go.mod .
ADD go.sum .

# Download the dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the Go application
RUN go build -o qtap cmd/qtap/qtap.go

# Use deno alpine for the final image
FROM denoland/deno:alpine

# Copy the compiled binary from the builder stage
COPY --from=qtap-builder /app/qtap /usr/local/bin/qtap

# Set the entrypoint to run the binary when the container starts
ENTRYPOINT ["qtap"]
