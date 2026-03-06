package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/integrations/bitbucket"
	"github.com/finsavvyai/pipewarden/internal/integrations/github"
	"github.com/finsavvyai/pipewarden/internal/integrations/gitlab"
	"github.com/finsavvyai/pipewarden/internal/logging"
)

func main() {
	logger, _ := logging.New(&config.LoggingConfig{Level: "info", JSON: false})
	defer logger.Sync()

	manager := integrations.NewManager(logger)

	// Each env var creates a named connection. Users can set multiple tokens
	// with numbered suffixes to test many connections at once.
	addGitHubConnections(manager, logger)
	addBitbucketConnections(manager, logger)
	addGitLabConnections(manager, logger)

	if manager.Count() == 0 {
		printUsage()
		os.Exit(1)
	}

	fmt.Println("PipeWarden Connection Tester")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Testing %d connection(s)...\n\n", manager.Count())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := manager.TestAllConnections(ctx)

	allOK := true
	for name, status := range results {
		if status.Connected {
			fmt.Printf("[PASS] %s (%s)\n", name, status.Platform)
			fmt.Printf("       User:      %s\n", status.User)
			fmt.Printf("       Scopes:    %v\n", status.Scopes)
			fmt.Printf("       RateLimit: %v\n", status.RateLimitOK)
			fmt.Printf("       Latency:   %v\n", status.Latency)
			fmt.Printf("       Message:   %s\n", status.Message)
		} else {
			fmt.Printf("[FAIL] %s (%s)\n", name, status.Platform)
			fmt.Printf("       Message:   %s\n", status.Message)
			fmt.Printf("       Latency:   %v\n", status.Latency)
			allOK = false
		}
		fmt.Println()
	}

	if allOK {
		fmt.Printf("All %d connection(s) successful!\n", len(results))
	} else {
		fmt.Println("Some connections failed.")
		os.Exit(1)
	}
}

// addGitHubConnections reads GITHUB_TOKEN and GITHUB_TOKEN_2..GITHUB_TOKEN_9
func addGitHubConnections(m *integrations.Manager, logger *logging.Logger) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		name := envOrDefault("GITHUB_NAME", "github-default")
		m.Add(name, github.NewClient(github.Config{
			Token:   token,
			BaseURL: os.Getenv("GITHUB_BASE_URL"),
		}, logger))
	}
	for i := 2; i <= 9; i++ {
		token := os.Getenv(fmt.Sprintf("GITHUB_TOKEN_%d", i))
		if token == "" {
			continue
		}
		name := envOrDefault(fmt.Sprintf("GITHUB_NAME_%d", i), fmt.Sprintf("github-%d", i))
		baseURL := os.Getenv(fmt.Sprintf("GITHUB_BASE_URL_%d", i))
		m.Add(name, github.NewClient(github.Config{Token: token, BaseURL: baseURL}, logger))
	}
}

// addBitbucketConnections reads BITBUCKET_USERNAME/BITBUCKET_APP_PASSWORD and _2.._9 variants
func addBitbucketConnections(m *integrations.Manager, logger *logging.Logger) {
	user := os.Getenv("BITBUCKET_USERNAME")
	pass := os.Getenv("BITBUCKET_APP_PASSWORD")
	if user != "" && pass != "" {
		name := envOrDefault("BITBUCKET_NAME", "bitbucket-default")
		m.Add(name, bitbucket.NewClient(bitbucket.Config{
			Username: user, AppPassword: pass,
			BaseURL: os.Getenv("BITBUCKET_BASE_URL"),
		}, logger))
	}
	for i := 2; i <= 9; i++ {
		user := os.Getenv(fmt.Sprintf("BITBUCKET_USERNAME_%d", i))
		pass := os.Getenv(fmt.Sprintf("BITBUCKET_APP_PASSWORD_%d", i))
		if user == "" || pass == "" {
			continue
		}
		name := envOrDefault(fmt.Sprintf("BITBUCKET_NAME_%d", i), fmt.Sprintf("bitbucket-%d", i))
		baseURL := os.Getenv(fmt.Sprintf("BITBUCKET_BASE_URL_%d", i))
		m.Add(name, bitbucket.NewClient(bitbucket.Config{
			Username: user, AppPassword: pass, BaseURL: baseURL,
		}, logger))
	}
}

// addGitLabConnections reads GITLAB_TOKEN and GITLAB_TOKEN_2..GITLAB_TOKEN_9
func addGitLabConnections(m *integrations.Manager, logger *logging.Logger) {
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		name := envOrDefault("GITLAB_NAME", "gitlab-default")
		m.Add(name, gitlab.NewClient(gitlab.Config{
			Token:   token,
			BaseURL: os.Getenv("GITLAB_BASE_URL"),
		}, logger))
	}
	for i := 2; i <= 9; i++ {
		token := os.Getenv(fmt.Sprintf("GITLAB_TOKEN_%d", i))
		if token == "" {
			continue
		}
		name := envOrDefault(fmt.Sprintf("GITLAB_NAME_%d", i), fmt.Sprintf("gitlab-%d", i))
		baseURL := os.Getenv(fmt.Sprintf("GITLAB_BASE_URL_%d", i))
		m.Add(name, gitlab.NewClient(gitlab.Config{Token: token, BaseURL: baseURL}, logger))
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printUsage() {
	fmt.Println("PipeWarden Connection Tester")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()
	fmt.Println("No credentials configured. Set environment variables:")
	fmt.Println()
	fmt.Println("  GitHub Actions (add _2, _3 ... _9 for more):")
	fmt.Println("    GITHUB_TOKEN=ghp_xxx  GITHUB_NAME=github-main")
	fmt.Println("    GITHUB_TOKEN_2=ghp_yyy  GITHUB_NAME_2=github-enterprise")
	fmt.Println()
	fmt.Println("  Bitbucket Pipelines:")
	fmt.Println("    BITBUCKET_USERNAME=user  BITBUCKET_APP_PASSWORD=pass")
	fmt.Println("    BITBUCKET_NAME=bitbucket-team")
	fmt.Println()
	fmt.Println("  GitLab CI/CD:")
	fmt.Println("    GITLAB_TOKEN=glpat-xxx  GITLAB_NAME=gitlab-cloud")
	fmt.Println("    GITLAB_TOKEN_2=glpat-yyy  GITLAB_NAME_2=gitlab-self-hosted")
	fmt.Println("    GITLAB_BASE_URL_2=https://gitlab.internal.com/api/v4")
	fmt.Println()
	fmt.Println("Example (3 connections):")
	fmt.Println("  GITHUB_TOKEN=ghp_abc GITHUB_NAME=gh-org-a \\")
	fmt.Println("  GITHUB_TOKEN_2=ghp_def GITHUB_NAME_2=gh-org-b \\")
	fmt.Println("  GITLAB_TOKEN=glpat-xyz GITLAB_NAME=gl-cloud \\")
	fmt.Println("  go run cmd/testconnections/main.go")
	fmt.Println()
}
