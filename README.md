# PipeWarden

<div align="center">
  <img src="https://via.placeholder.com/200x200" alt="PipeWarden Logo" width="200" height="200"/>
  <h3>Security Guardian for CI/CD Pipelines</h3>
  <p>Break free from security vulnerabilities without slowing down development</p>
</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/finsavvyai/pipewarden)](https://goreportcard.com/report/github.com/finsavvyai/pipewarden)
[![GoDoc](https://godoc.org/github.com/finsavvyai/pipewarden?status.svg)](https://godoc.org/github.com/finsavvyai/pipewarden)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

---

## Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Architecture](#architecture)
- [Getting Started](#getting-started)
- [Web Dashboard](#web-dashboard)
- [Integrations](#integrations)
- [API Reference](#api-reference)
- [Configuration](#configuration)
- [Development](#development)
- [Testing](#testing)
- [Deployment](#deployment)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

PipeWarden is a DevSecOps Pipeline Orchestrator that acts as a security guardian for your CI/CD pipelines. It monitors, scans, and enforces security policies across multiple CI/CD platforms from a single dashboard.

### Why PipeWarden?

- **Multi-Connection** - Connect unlimited accounts per platform (multiple GitHub orgs, GitLab instances, Bitbucket workspaces)
- **Web Dashboard** - Add, remove, and test connections from a browser UI with real-time status
- **DB Persistence** - SQLite storage keeps connections across restarts
- **Universal Integration** - GitHub Actions, GitLab CI/CD, and Bitbucket Pipelines with a pluggable provider interface
- **Policy-Driven Security** - Define security rules once and apply them everywhere
- **AI Analysis** - AI-powered vulnerability context and remediation suggestions

---

## Key Features

### Connection Manager

Manage all your CI/CD platform connections from the web dashboard:

- Add unlimited connections per platform (e.g. 3 GitHub orgs + 2 GitLab instances)
- Test individual connections or all at once with one click
- Live status indicators (connected, failed, testing)
- Platform-specific credential fields (tokens, app passwords)
- Persistent storage in SQLite - survives restarts

### Supported Platforms

| Platform | Auth Method | Features |
|----------|-------------|----------|
| **GitHub Actions** | Personal Access Token | Workflows, runs, dispatch triggers, OAuth scope detection |
| **GitLab CI/CD** | Access Token (PRIVATE-TOKEN) | Pipelines, runs, triggers, self-hosted instances |
| **Bitbucket Pipelines** | App Password (Basic Auth) | Pipelines, runs, triggers |

### Security Policy Engine

- Create and manage security policies with an intuitive UI
- Define custom rules for vulnerability thresholds and compliance requirements
- Template library for common security requirements

### Vulnerability Management

- Detect and track security vulnerabilities across platforms
- Normalize and deduplicate findings from multiple scanners
- Prioritize issues based on severity and context
- Actionable remediation guidance

### Compliance Automation

- Map security controls to compliance frameworks: SOC 2, ISO 27001, PCI DSS, HIPAA, GDPR
- Automate evidence collection
- Generate compliance reports

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    Web Dashboard                         │
│              (Embedded SPA at :8080)                     │
└───────────────────────┬──────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────────┐
│                   PipeWarden API                         │
│            REST endpoints + Connection Manager           │
└──────┬──────────┬──────────┬─────────────────────────────┘
       │          │          │
       ▼          ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────┐    ┌─────────────┐
│  GitHub  │ │  GitLab  │ │Bitbucket │    │   SQLite    │
│ Actions  │ │  CI/CD   │ │Pipelines │    │  (Storage)  │
└──────────┘ └──────────┘ └──────────┘    └─────────────┘

Provider Interface:
  TestConnection()  - Verify credentials and connectivity
  ListPipelines()   - List pipeline/workflow definitions
  GetPipelineRun()  - Get details of a specific run
  ListPipelineRuns()- List recent runs
  TriggerPipeline() - Start a new pipeline run
```

### Key Components

- **Integration Manager** - Thread-safe registry with concurrent connection testing
- **Provider Interface** - Pluggable contract for adding new CI/CD platforms
- **Storage Layer** - SQLite with WAL mode, auto-migration on first run
- **Web Dashboard** - Embedded SPA served from Go binary (no separate build step)

---

## Getting Started

### Prerequisites

- Go 1.21+
- Git
- GCC (for SQLite CGO compilation)

### Quick Start

```bash
# Clone
git clone https://github.com/finsavvyai/pipewarden.git
cd pipewarden

# Build and run
make build
./bin/pipewarden

# Or run directly
make run
```

Open http://localhost:8080 to access the dashboard.

### Add Your First Connection

1. Open the dashboard at http://localhost:8080
2. Click **"+ Add Connection"**
3. Select a platform (GitHub, GitLab, or Bitbucket)
4. Enter a name and credentials
5. Click **"Add Connection"**
6. Click **"Test"** to verify connectivity

---

## Web Dashboard

The dashboard is embedded in the Go binary and served at the root URL.

### Features

- **Stats Overview** - Total connections + per-platform counts
- **Connection Cards** - Name, platform, status with colored indicators
- **Add Modal** - Platform-specific credential forms
- **Test Individual** - Test a single connection on demand
- **Test All** - Concurrent testing of every connection
- **Remove** - Delete from both memory and database

### Status Indicators

| Indicator | Meaning |
|-----------|---------|
| Green dot | Connected successfully |
| Red dot | Connection failed |
| Yellow dot (pulsing) | Currently testing |
| Gray dot | Not yet tested |

---

## Integrations

### GitHub Actions

```bash
# Via dashboard: Platform = GitHub, Token = ghp_xxx
# Supports GitHub Enterprise: set Base URL to https://github.example.com/api/v3
```

Features: workflow listing, run management, dispatch triggers, OAuth scope detection, rate limit monitoring.

### GitLab CI/CD

```bash
# Via dashboard: Platform = GitLab, Token = glpat-xxx
# Self-hosted: set Base URL to https://gitlab.example.com/api/v4
```

Features: pipeline CRUD, trigger by ref, token scope parsing, rate limit detection.

### Bitbucket Pipelines

```bash
# Via dashboard: Platform = Bitbucket, Username + App Password
```

Features: pipeline listing, run management, branch-based triggers.

### Adding a New Platform

Implement the `Provider` interface in `internal/integrations/`:

```go
type Provider interface {
    Name() Platform
    TestConnection(ctx context.Context) (*ConnectionStatus, error)
    ListPipelines(ctx context.Context, owner, repo string) ([]Pipeline, error)
    GetPipelineRun(ctx context.Context, owner, repo, runID string) (*PipelineRun, error)
    ListPipelineRuns(ctx context.Context, owner, repo string, limit int) ([]PipelineRun, error)
    TriggerPipeline(ctx context.Context, owner, repo, workflow, branch string) (*PipelineRun, error)
}
```

---

## API Reference

All connections are managed via REST API. The dashboard uses these same endpoints.

### Connections

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/connections` | List all connections |
| `POST` | `/api/v1/connections` | Add a new connection |
| `GET` | `/api/v1/connections/{name}` | Get connection details |
| `DELETE` | `/api/v1/connections/{name}` | Remove a connection |
| `POST` | `/api/v1/connections/{name}/test` | Test a single connection |
| `POST` | `/api/v1/connections/test` | Test all connections |

### Examples

```bash
# Add a GitHub connection
curl -X POST http://localhost:8080/api/v1/connections \
  -H "Content-Type: application/json" \
  -d '{"name":"github-main","platform":"github","token":"ghp_xxx"}'

# Add a GitLab connection (self-hosted)
curl -X POST http://localhost:8080/api/v1/connections \
  -H "Content-Type: application/json" \
  -d '{"name":"gitlab-internal","platform":"gitlab","token":"glpat-xxx","base_url":"https://gitlab.example.com/api/v4"}'

# Add a Bitbucket connection
curl -X POST http://localhost:8080/api/v1/connections \
  -H "Content-Type: application/json" \
  -d '{"name":"bb-team","platform":"bitbucket","username":"user","app_password":"xxx"}'

# Test all connections
curl -X POST http://localhost:8080/api/v1/connections/test

# Test a specific connection
curl -X POST http://localhost:8080/api/v1/connections/github-main/test

# List all connections
curl http://localhost:8080/api/v1/connections

# Remove a connection
curl -X DELETE http://localhost:8080/api/v1/connections/github-main
```

### Other Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/` | Web dashboard |

---

## Configuration

### Config File

```yaml
environment: development
server:
  port: 8080
  readTimeout: 5s
  writeTimeout: 10s
  idleTimeout: 120s
database:
  path: pipewarden.db    # SQLite file path
auth:
  jwtSecret: "your-secret-key-here"
  tokenDuration: 24h
logging:
  level: debug            # debug, info, warn, error
  json: false             # true for structured JSON logs
```

### Environment Variables

All config values can be set via environment variables with the `PIPEWARDEN_` prefix:

```bash
PIPEWARDEN_SERVER_PORT=8080
PIPEWARDEN_DATABASE_PATH=pipewarden.db
PIPEWARDEN_LOGGING_LEVEL=info
PIPEWARDEN_LOGGING_JSON=true
```

### Command Line Flags

```bash
./bin/pipewarden --config configs/development/config.yml --db /data/pipewarden.db
```

---

## Development

### Project Structure

```
pipewarden/
├── cmd/
│   ├── pipewarden/          # Main application entry point
│   └── testconnections/     # CLI connection tester
├── configs/
│   └── development/
│       └── config.yml       # Dev configuration
├── internal/
│   ├── config/              # Configuration management (Viper)
│   ├── errors/              # Custom error types
│   ├── integrations/        # Provider interface + manager
│   │   ├── github/          # GitHub Actions client
│   │   ├── gitlab/          # GitLab CI/CD client
│   │   └── bitbucket/       # Bitbucket Pipelines client
│   ├── logging/             # Structured logging (Zap)
│   ├── storage/             # SQLite persistence layer
│   └── web/
│       └── static/          # Embedded dashboard HTML/CSS/JS
├── Makefile
├── go.mod
└── go.sum
```

### Makefile Targets

```bash
make build             # Build binary to bin/pipewarden
make run               # Run the application
make test              # Run all tests
make test-connections   # Test real API connections (requires tokens)
make test-integration   # Run integration tests against real APIs
make lint              # Run golangci-lint
make clean             # Remove build artifacts
make mocks             # Generate mocks
```

---

## Testing

### Unit Tests (67 tests)

```bash
make test
```

| Package | Tests | Description |
|---------|-------|-------------|
| `integrations` | 21 | Manager: CRUD, multi-connection, concurrent testing |
| `integrations/github` | 12 | GitHub client: auth, CRUD, status mapping, scopes |
| `integrations/gitlab` | 12 | GitLab client: auth, CRUD, status mapping, scopes |
| `integrations/bitbucket` | 11 | Bitbucket client: auth, CRUD, status mapping |
| `storage` | 12 | SQLite: CRUD, persistence, duplicates, file creation |

### Integration Tests (Real APIs)

```bash
# Test GitHub connection
GITHUB_TOKEN=ghp_xxx make test-integration

# Test all platforms
GITHUB_TOKEN=ghp_xxx GITLAB_TOKEN=glpat-xxx \
  BITBUCKET_USERNAME=user BITBUCKET_APP_PASSWORD=pass \
  make test-integration

# With pipeline listing
GITHUB_TOKEN=ghp_xxx GITHUB_TEST_OWNER=myorg GITHUB_TEST_REPO=myrepo \
  make test-integration
```

### CLI Connection Tester

```bash
# Test multiple connections
GITHUB_TOKEN=ghp_abc GITHUB_NAME=gh-org-a \
  GITHUB_TOKEN_2=ghp_def GITHUB_NAME_2=gh-enterprise \
  GITLAB_TOKEN=glpat-xyz GITLAB_NAME=gl-cloud \
  make test-connections
```

---

## Deployment

### Docker

```bash
docker build -t pipewarden .
docker run -p 8080:8080 -v /data:/data pipewarden --db /data/pipewarden.db
```

### Production Recommendations

- Set `PIPEWARDEN_LOGGING_JSON=true` for structured log output
- Mount a persistent volume for the SQLite database
- Use `PIPEWARDEN_AUTH_JWTSECRET` with a strong secret
- Set `PIPEWARDEN_ENVIRONMENT=production`

---

## Roadmap

- **Q2 2025**: Jenkins and Azure DevOps integrations
- **Q3 2025**: Security scanner integrations (Trivy, Snyk, SonarQube)
- **Q4 2025**: Policy engine with automated enforcement
- **Q1 2026**: AI-powered vulnerability analysis and remediation
- **Q2 2026**: Compliance reporting and audit trails

---

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details.

### Adding a New CI/CD Platform

1. Create a new package under `internal/integrations/yourplatform/`
2. Implement the `Provider` interface
3. Add the platform constant to `integration.go`
4. Register the provider in `cmd/pipewarden/main.go`
5. Write tests using `net/http/httptest` mock servers

---

## License

PipeWarden is released under the MIT License. See [LICENSE](LICENSE) for details.

---

<div align="center">
  <p>PipeWarden - Break free from security vulnerabilities</p>
  <p>Made with care by <a href="https://pipewarden.io">FinSavvy AI</a></p>
</div>
