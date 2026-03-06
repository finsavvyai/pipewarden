package integrations_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/integrations/bitbucket"
	"github.com/finsavvyai/pipewarden/internal/integrations/github"
	"github.com/finsavvyai/pipewarden/internal/integrations/gitlab"
	"github.com/finsavvyai/pipewarden/internal/logging"
)

func newLogger() *logging.Logger {
	cfg := &config.LoggingConfig{Level: "debug", JSON: false}
	logger, _ := logging.New(cfg)
	return logger
}

// TestRealGitHubConnection tests against the real GitHub API.
// Run with: GITHUB_TOKEN=ghp_xxx go test -v -run TestRealGitHubConnection ./internal/integrations/
func TestRealGitHubConnection(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping real GitHub connection test")
	}

	logger := newLogger()
	client := github.NewClient(github.Config{
		Token: token,
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test connection
	status, err := client.TestConnection(ctx)
	if err != nil {
		t.Fatalf("GitHub connection failed: %v", err)
	}

	t.Logf("Connected: %v", status.Connected)
	t.Logf("User:      %s", status.User)
	t.Logf("Scopes:    %v", status.Scopes)
	t.Logf("RateLimit: %v", status.RateLimitOK)
	t.Logf("Latency:   %v", status.Latency)
	t.Logf("Message:   %s", status.Message)

	if !status.Connected {
		t.Fatalf("expected connected, got message: %s", status.Message)
	}

	// If a test repo is configured, test pipeline operations
	owner := os.Getenv("GITHUB_TEST_OWNER")
	repo := os.Getenv("GITHUB_TEST_REPO")
	if owner != "" && repo != "" {
		t.Run("ListPipelines", func(t *testing.T) {
			pipelines, err := client.ListPipelines(ctx, owner, repo)
			if err != nil {
				t.Fatalf("ListPipelines failed: %v", err)
			}
			t.Logf("Found %d workflows", len(pipelines))
			for _, p := range pipelines {
				t.Logf("  - %s (ID: %s) %s", p.Name, p.ID, p.URL)
			}
		})

		t.Run("ListPipelineRuns", func(t *testing.T) {
			runs, err := client.ListPipelineRuns(ctx, owner, repo, 5)
			if err != nil {
				t.Fatalf("ListPipelineRuns failed: %v", err)
			}
			t.Logf("Found %d recent runs", len(runs))
			for _, r := range runs {
				t.Logf("  - Run %s: status=%s branch=%s sha=%s", r.ID, r.Status, r.Branch, r.CommitSHA[:min(7, len(r.CommitSHA))])
			}
		})
	}
}

// TestRealBitbucketConnection tests against the real Bitbucket API.
// Run with: BITBUCKET_USERNAME=xxx BITBUCKET_APP_PASSWORD=xxx go test -v -run TestRealBitbucketConnection ./internal/integrations/
func TestRealBitbucketConnection(t *testing.T) {
	username := os.Getenv("BITBUCKET_USERNAME")
	appPassword := os.Getenv("BITBUCKET_APP_PASSWORD")
	if username == "" || appPassword == "" {
		t.Skip("BITBUCKET_USERNAME/BITBUCKET_APP_PASSWORD not set, skipping real Bitbucket connection test")
	}

	logger := newLogger()
	client := bitbucket.NewClient(bitbucket.Config{
		Username:    username,
		AppPassword: appPassword,
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := client.TestConnection(ctx)
	if err != nil {
		t.Fatalf("Bitbucket connection failed: %v", err)
	}

	t.Logf("Connected: %v", status.Connected)
	t.Logf("User:      %s", status.User)
	t.Logf("Latency:   %v", status.Latency)
	t.Logf("Message:   %s", status.Message)

	if !status.Connected {
		t.Fatalf("expected connected, got message: %s", status.Message)
	}

	owner := os.Getenv("BITBUCKET_TEST_WORKSPACE")
	repo := os.Getenv("BITBUCKET_TEST_REPO")
	if owner != "" && repo != "" {
		t.Run("ListPipelines", func(t *testing.T) {
			pipelines, err := client.ListPipelines(ctx, owner, repo)
			if err != nil {
				t.Fatalf("ListPipelines failed: %v", err)
			}
			t.Logf("Found %d pipelines", len(pipelines))
			for _, p := range pipelines {
				t.Logf("  - %s (ID: %s) %s", p.Name, p.ID, p.URL)
			}
		})

		t.Run("ListPipelineRuns", func(t *testing.T) {
			runs, err := client.ListPipelineRuns(ctx, owner, repo, 5)
			if err != nil {
				t.Fatalf("ListPipelineRuns failed: %v", err)
			}
			t.Logf("Found %d recent runs", len(runs))
			for _, r := range runs {
				t.Logf("  - Run %s: status=%s branch=%s", r.ID, r.Status, r.Branch)
			}
		})
	}
}

// TestRealGitLabConnection tests against the real GitLab API.
// Run with: GITLAB_TOKEN=glpat-xxx go test -v -run TestRealGitLabConnection ./internal/integrations/
func TestRealGitLabConnection(t *testing.T) {
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		t.Skip("GITLAB_TOKEN not set, skipping real GitLab connection test")
	}

	baseURL := os.Getenv("GITLAB_BASE_URL")

	logger := newLogger()
	client := gitlab.NewClient(gitlab.Config{
		Token:   token,
		BaseURL: baseURL,
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := client.TestConnection(ctx)
	if err != nil {
		t.Fatalf("GitLab connection failed: %v", err)
	}

	t.Logf("Connected: %v", status.Connected)
	t.Logf("User:      %s", status.User)
	t.Logf("Scopes:    %v", status.Scopes)
	t.Logf("RateLimit: %v", status.RateLimitOK)
	t.Logf("Latency:   %v", status.Latency)
	t.Logf("Message:   %s", status.Message)

	if !status.Connected {
		t.Fatalf("expected connected, got message: %s", status.Message)
	}

	owner := os.Getenv("GITLAB_TEST_NAMESPACE")
	repo := os.Getenv("GITLAB_TEST_PROJECT")
	if owner != "" && repo != "" {
		t.Run("ListPipelines", func(t *testing.T) {
			pipelines, err := client.ListPipelines(ctx, owner, repo)
			if err != nil {
				t.Fatalf("ListPipelines failed: %v", err)
			}
			t.Logf("Found %d pipelines", len(pipelines))
			for _, p := range pipelines {
				t.Logf("  - %s (ID: %s) %s", p.Name, p.ID, p.URL)
			}
		})

		t.Run("ListPipelineRuns", func(t *testing.T) {
			runs, err := client.ListPipelineRuns(ctx, owner, repo, 5)
			if err != nil {
				t.Fatalf("ListPipelineRuns failed: %v", err)
			}
			t.Logf("Found %d recent runs", len(runs))
			for _, r := range runs {
				t.Logf("  - Run %s: status=%s branch=%s", r.ID, r.Status, r.Branch)
			}
		})
	}
}

// TestRealAllConnections tests all configured providers concurrently via the Manager.
// Run with all tokens set to test full orchestration.
func TestRealAllConnections(t *testing.T) {
	ghToken := os.Getenv("GITHUB_TOKEN")
	bbUser := os.Getenv("BITBUCKET_USERNAME")
	bbPass := os.Getenv("BITBUCKET_APP_PASSWORD")
	glToken := os.Getenv("GITLAB_TOKEN")

	if ghToken == "" && (bbUser == "" || bbPass == "") && glToken == "" {
		t.Skip("No integration credentials set, skipping full connection test")
	}

	logger := newLogger()
	manager := integrations.NewManager(logger)

	if ghToken != "" {
		manager.Register(github.NewClient(github.Config{Token: ghToken}, logger))
	}
	if bbUser != "" && bbPass != "" {
		manager.Register(bitbucket.NewClient(bitbucket.Config{
			Username: bbUser, AppPassword: bbPass,
		}, logger))
	}
	if glToken != "" {
		glBaseURL := os.Getenv("GITLAB_BASE_URL")
		manager.Register(gitlab.NewClient(gitlab.Config{
			Token: glToken, BaseURL: glBaseURL,
		}, logger))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := manager.TestAllConnections(ctx)

	t.Logf("Tested %d platform(s):", len(results))
	allOK := true
	for platform, status := range results {
		symbol := "PASS"
		if !status.Connected {
			symbol = "FAIL"
			allOK = false
		}
		t.Logf("  [%s] %s: connected=%v user=%s latency=%v message=%q",
			symbol, platform, status.Connected, status.User, status.Latency, status.Message)
	}

	if !allOK {
		t.Error("One or more connections failed")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
