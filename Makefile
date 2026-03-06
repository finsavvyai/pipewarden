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

# Test real connections (set GITHUB_TOKEN, GITLAB_TOKEN, BITBUCKET_USERNAME/BITBUCKET_APP_PASSWORD)
test-connections:
	go run cmd/testconnections/main.go

# Run only integration tests against real APIs (requires credentials)
test-integration:
	go test -v -run TestReal ./internal/integrations/

# Clean build artifacts
clean:
	rm -rf bin/

# Generate mocks (requires mockgen)
mocks:
	go generate ./...