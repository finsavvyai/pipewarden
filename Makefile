.PHONY: build run test lint clean

# Build the application
build:
	go build -o bin/pipewarden cmd/pipewarden/main.go

# Run the application
run:
	go run cmd/pipewarden/main.go

# Run tests
test:
	go test -v ./...

# Run linting
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf bin/

# Generate mocks (requires mockgen)
mocks:
	go generate ./...