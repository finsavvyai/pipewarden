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

## 📋 Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Architecture](#architecture)
- [Getting Started](#getting-started)
- [Configuration](#configuration)
- [Development](#development)
- [Deployment](#deployment)
- [API Documentation](#api-documentation)
- [Integrations](#integrations)
- [FAQ](#faq)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

---

## 🔍 Overview

PipeWarden is a comprehensive DevSecOps Pipeline Orchestrator that acts as a security guardian for your CI/CD pipelines. It continuously monitors, scans, and enforces security policies throughout your development lifecycle across multiple CI/CD platforms.

With PipeWarden, security becomes an integral part of your development process, not a bottleneck. It helps you identify vulnerabilities early, enforce security policies automatically, and provide clear remediation guidance - all while integrating seamlessly with your existing development workflows.

### Why PipeWarden?

- **Universal Integration** - Works with multiple CI/CD platforms including GitLab, GitHub Actions, Jenkins, and more
- **Policy-Driven Security** - Define security rules once and apply them everywhere
- **Intelligent Analysis** - AI-powered vulnerability context and remediation suggestions
- **Developer-Friendly** - Clear feedback and guidance without disrupting workflows
- **Compliance Automation** - Map security controls directly to compliance frameworks

---

## ✨ Key Features

### 🛡️ Security Policy Engine

- Create and manage security policies with an intuitive UI
- Define custom rules for vulnerability thresholds, compliance requirements
- Apply policies at different stages of development
- Version control your security policies
- Template library for common security requirements

### 🔄 CI/CD Integration

- Connect to multiple CI/CD platforms:
  - GitLab CI/CD
  - GitHub Actions
  - Jenkins
  - Azure DevOps
  - CircleCI
  - AWS CodePipeline
- Monitor pipeline activities in real-time
- Scan code, containers, and infrastructure
- Enforce security gates based on policy

### 🔎 Vulnerability Management

- Detect and track security vulnerabilities
- Normalize and deduplicate findings from multiple scanners
- Prioritize issues based on severity and context
- Track remediation progress
- Provide actionable remediation guidance

### 👥 Multi-tenant Architecture

- Support multiple organizations/teams
- Isolate data and policies between tenants
- Role-based access control
- Customizable per-tenant configurations
- Cross-tenant analytics for enterprise users

### 🧠 AI-powered Analysis

- Intelligent context analysis for vulnerabilities
- Remediation suggestions with code examples
- Risk prediction based on historical data
- Automated severity adjustment based on context
- Natural language explanations of security issues

### 📊 Compliance Automation

- Map security controls to compliance frameworks:
  - SOC 2
  - ISO 27001
  - PCI DSS
  - HIPAA
  - GDPR
  - Custom frameworks
- Automate evidence collection
- Generate compliance reports
- Track compliance status in real-time

---

## 🏗️ Architecture

PipeWarden follows a modern, scalable architecture:

```
┌───────────────────┐     ┌──────────────────┐     ┌───────────────────┐
│                   │     │                  │     │                   │
│  CI/CD Platforms  │◄────┤  PipeWarden API  │◄────┤  Web Dashboard   │
│                   │     │                  │     │                   │
└───────────────────┘     └──────────────────┘     └───────────────────┘
         ▲                        ▲
         │                        │
         ▼                        ▼
┌───────────────────┐     ┌──────────────────┐     ┌───────────────────┐
│                   │     │                  │     │                   │
│  Scanner Service  │◄────┤  Policy Engine   │◄────┤  AI Analysis      │
│                   │     │                  │     │                   │
└───────────────────┘     └──────────────────┘     └───────────────────┘
                                   ▲
                                   │
                                   ▼
                          ┌──────────────────┐
                          │                  │
                          │  Event System    │
                          │                  │
                          └──────────────────┘
```

PipeWarden uses a modular, microservices-based architecture with the following components:

- **API Gateway**: Central entry point for all external interactions
- **Web Dashboard**: React-based UI for management and monitoring
- **Policy Engine**: Core component for security policy evaluation
- **Scanner Service**: Integration with various security scanning tools
- **Event System**: Asynchronous event processing for scalability
- **AI Analysis**: Integration with AWS Bedrock for intelligent security analysis

---

## 🚀 Getting Started

### Prerequisites

- Go 1.21+
- Docker and docker-compose
- Git
- PostgreSQL (for local development)

### Quick Start

1. **Clone the repository**

```bash
git clone https://github.com/finsavvyai/pipewarden.git
cd pipewarden
```

2. **Build and run with Docker**

```bash
docker-compose up -d
```

3. **Or build and run locally**

```bash
make build
./bin/pipewarden
```

4. **Visit the dashboard**

Open your browser and navigate to http://localhost:8080

### Initial Setup

After starting PipeWarden for the first time:

1. Create an admin account using the setup wizard
2. Configure your first integration with a CI/CD platform
3. Set up your initial security policies or import templates

---

## ⚙️ Configuration

PipeWarden can be configured through environment variables, configuration files, or command-line flags.

### Environment Variables

Key environment variables include:

```
PIPEWARDEN_ENVIRONMENT=development
PIPEWARDEN_SERVER_PORT=8080
PIPEWARDEN_DATABASE_HOST=localhost
PIPEWARDEN_DATABASE_PORT=5432
PIPEWARDEN_DATABASE_NAME=pipewarden
PIPEWARDEN_LOGGING_LEVEL=info
```

### Configuration File

Example `config.yaml`:

```yaml
environment: development
server:
  port: 8080
  readTimeout: 5s
  writeTimeout: 10s
  idleTimeout: 120s
database:
  host: localhost
  port: 5432
  username: postgres
  password: postgres
  name: pipewarden
  sslMode: disable
auth:
  jwtSecret: "your-secret-key-here"
  tokenDuration: 24h
logging:
  level: debug
  json: false
```

See [Configuration Guide](docs/configuration.md) for a complete reference.

---

## 💻 Development

### Project Structure

```
├── api/            # API definitions and handlers
├── cmd/            # Application entry points
├── configs/        # Configuration files
├── docs/           # Documentation
├── internal/       # Private application code
│   ├── auth/       # Authentication and authorization
│   ├── config/     # Configuration management
│   ├── database/   # Database access
│   ├── errors/     # Error handling
│   ├── logging/    # Logging framework
│   ├── policy/     # Policy engine
│   ├── scanner/    # Scanner integration
│   └── server/     # HTTP server
├── pkg/            # Public packages
│   ├── models/     # Data models
│   └── utils/      # Utilities
├── scripts/        # Utility scripts
├── tests/          # Integration and E2E tests
└── web/            # Frontend assets
```

### Development Workflow

1. **Set up your local environment**

```bash
# Install dependencies
make setup

# Generate mocks
make mocks
```

2. **Run the application in development mode**

```bash
make run
```

3. **Run tests**

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage
```

4. **Lint code**

```bash
make lint
```

### Debugging

PipeWarden supports various debugging options:

- Set `PIPEWARDEN_LOGGING_LEVEL=debug` for verbose logging
- Use the `/debug/pprof` endpoint in development mode
- Enable tracing with `PIPEWARDEN_TRACING_ENABLED=true`

---

## 📦 Deployment

### Docker Deployment

PipeWarden can be deployed using Docker:

```bash
docker run -p 8080:8080 -e PIPEWARDEN_DATABASE_HOST=your-db-host finsavvyai/pipewarden
```

### Kubernetes Deployment

See [Kubernetes Deployment Guide](docs/kubernetes.md) for details on deploying to Kubernetes.

### AWS Marketplace

PipeWarden is available on AWS Marketplace for easy deployment on AWS:

1. Subscribe to PipeWarden on AWS Marketplace
2. Launch using CloudFormation templates
3. Configure using the setup wizard

---

## 📚 API Documentation

PipeWarden provides a RESTful API for integration with other systems.

API documentation is available at `/api/docs` when running the application, or see [API Documentation](docs/api.md).

### Example API Usage

```bash
# Authenticate and get token
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"password"}'

# Use token to get policies
curl -X GET http://localhost:8080/api/v1/policies \
  -H "Authorization: Bearer YOUR_TOKEN"
```

---

## 🔌 Integrations

PipeWarden integrates with various CI/CD platforms and security tools:

### CI/CD Platforms

- GitLab CI/CD
- GitHub Actions
- Jenkins
- Azure DevOps
- CircleCI
- AWS CodePipeline

### Security Scanners

- Trivy
- SonarQube
- OWASP ZAP
- Snyk
- Checkmarx
- Veracode

### Notification Systems

- Email
- Slack
- Microsoft Teams
- Webhooks

See [Integration Guide](docs/integrations.md) for setup instructions.

---

## ❓ FAQ

### How does PipeWarden compare to other security tools?

PipeWarden differentiates itself by focusing on cross-platform pipeline orchestration rather than being a scanner itself. It integrates with your existing security tools while providing centralized policy management and enforcement.

### Can I use PipeWarden with my existing CI/CD setup?

Yes, PipeWarden is designed to integrate with various CI/CD platforms without requiring you to change your existing workflows.

### How does licensing work?

PipeWarden uses a subscription model based on the number of pipelines you want to monitor. See our [pricing page](https://pipewarden.io/pricing) for details.

### Is my data secure?

Yes, PipeWarden uses tenant isolation to ensure that your data remains private. We do not store your source code, only metadata about security findings.

---

## 🗺️ Roadmap

Our planned features include:

- **Q2 2025**: Add support for additional CI/CD platforms
- **Q3 2025**: Enhanced compliance reporting with more frameworks
- **Q4 2025**: Advanced AI capabilities for proactive security insights
- **Q1 2026**: Extended API and SDK for custom integrations

---

## 👨‍💻 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details on how to contribute.

### Development Environment

See the [Development](#development) section for details on setting up your development environment.

### Code of Conduct

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before contributing.

---

## 📄 License

PipeWarden is released under the MIT License. See [LICENSE](LICENSE) for details.

---

<div align="center">
  <p>PipeWarden - Break free from security vulnerabilities</p>
  <p>Made with ❤️ by FinSavvy AI</p>
</div>
