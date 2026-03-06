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
	registered := 0

	// GitHub
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		manager.Register(github.NewClient(github.Config{
			Token:   token,
			BaseURL: os.Getenv("GITHUB_BASE_URL"),
		}, logger))
		registered++
	}

	// Bitbucket
	bbUser := os.Getenv("BITBUCKET_USERNAME")
	bbPass := os.Getenv("BITBUCKET_APP_PASSWORD")
	if bbUser != "" && bbPass != "" {
		manager.Register(bitbucket.NewClient(bitbucket.Config{
			Username:    bbUser,
			AppPassword: bbPass,
			BaseURL:     os.Getenv("BITBUCKET_BASE_URL"),
		}, logger))
		registered++
	}

	// GitLab
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		manager.Register(gitlab.NewClient(gitlab.Config{
			Token:   token,
			BaseURL: os.Getenv("GITLAB_BASE_URL"),
		}, logger))
		registered++
	}

	if registered == 0 {
		fmt.Println("PipeWarden Connection Tester")
		fmt.Println(strings.Repeat("=", 40))
		fmt.Println()
		fmt.Println("No credentials configured. Set environment variables:")
		fmt.Println()
		fmt.Println("  GitHub Actions:")
		fmt.Println("    GITHUB_TOKEN=ghp_xxx")
		fmt.Println("    GITHUB_BASE_URL=https://api.github.com  (optional)")
		fmt.Println()
		fmt.Println("  Bitbucket Pipelines:")
		fmt.Println("    BITBUCKET_USERNAME=your-username")
		fmt.Println("    BITBUCKET_APP_PASSWORD=your-app-password")
		fmt.Println("    BITBUCKET_BASE_URL=...  (optional)")
		fmt.Println()
		fmt.Println("  GitLab CI/CD:")
		fmt.Println("    GITLAB_TOKEN=glpat-xxx")
		fmt.Println("    GITLAB_BASE_URL=https://gitlab.com/api/v4  (optional)")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  GITHUB_TOKEN=ghp_abc GITLAB_TOKEN=glpat-xyz go run cmd/testconnections/main.go")
		os.Exit(1)
	}

	fmt.Println("PipeWarden Connection Tester")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("Testing %d platform(s)...\n\n", registered)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := manager.TestAllConnections(ctx)

	allOK := true
	for platform, status := range results {
		if status.Connected {
			fmt.Printf("[PASS] %s\n", platform)
			fmt.Printf("       User:      %s\n", status.User)
			fmt.Printf("       Scopes:    %v\n", status.Scopes)
			fmt.Printf("       RateLimit: %v\n", status.RateLimitOK)
			fmt.Printf("       Latency:   %v\n", status.Latency)
			fmt.Printf("       Message:   %s\n", status.Message)
		} else {
			fmt.Printf("[FAIL] %s\n", platform)
			fmt.Printf("       Message:   %s\n", status.Message)
			fmt.Printf("       Latency:   %v\n", status.Latency)
			allOK = false
		}
		fmt.Println()
	}

	if allOK {
		fmt.Println("All connections successful!")
	} else {
		fmt.Println("Some connections failed.")
		os.Exit(1)
	}
}
