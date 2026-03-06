package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/logging"
)

func newTestLogger() *logging.Logger {
	cfg := &config.LoggingConfig{Level: "info", JSON: true}
	logger, _ := logging.New(cfg)
	return logger
}

func TestName(t *testing.T) {
	client := NewClient(Config{Token: "test"}, newTestLogger())
	if client.Name() != integrations.PlatformGitHub {
		t.Errorf("expected %s, got %s", integrations.PlatformGitHub, client.Name())
	}
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-OAuth-Scopes", "repo, workflow")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(ghUser{Login: "testuser", ID: 1})
	}))
	defer server.Close()

	client := NewClient(Config{Token: "test-token", BaseURL: server.URL}, newTestLogger())
	status, err := client.TestConnection(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Connected {
		t.Error("expected connected to be true")
	}
	if status.User != "testuser" {
		t.Errorf("expected user testuser, got %s", status.User)
	}
	if status.Platform != integrations.PlatformGitHub {
		t.Errorf("expected platform github, got %s", status.Platform)
	}
	if !status.RateLimitOK {
		t.Error("expected rate limit OK")
	}
	if len(status.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(status.Scopes))
	}
}

func TestTestConnection_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	client := NewClient(Config{Token: "bad-token", BaseURL: server.URL}, newTestLogger())
	status, err := client.TestConnection(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Connected {
		t.Error("expected connected to be false")
	}
}

func TestListPipelines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/actions/workflows" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ghWorkflowsResponse{
			TotalCount: 2,
			Workflows: []ghWorkflow{
				{ID: 1, Name: "CI", State: "active", HTMLURL: "https://github.com/owner/repo/actions/workflows/ci.yml"},
				{ID: 2, Name: "Deploy", State: "active", HTMLURL: "https://github.com/owner/repo/actions/workflows/deploy.yml"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{Token: "token", BaseURL: server.URL}, newTestLogger())
	pipelines, err := client.ListPipelines(context.Background(), "owner", "repo")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipelines) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(pipelines))
	}
	if pipelines[0].Name != "CI" {
		t.Errorf("expected CI, got %s", pipelines[0].Name)
	}
	if pipelines[0].Repository != "owner/repo" {
		t.Errorf("expected owner/repo, got %s", pipelines[0].Repository)
	}
}

func TestGetPipelineRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/actions/runs/123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ghWorkflowRun{
			ID:         123,
			WorkflowID: 1,
			Status:     "completed",
			Conclusion: "success",
			HeadBranch: "main",
			HeadSHA:    "abc123",
			HTMLURL:    "https://github.com/owner/repo/actions/runs/123",
		})
	}))
	defer server.Close()

	client := NewClient(Config{Token: "token", BaseURL: server.URL}, newTestLogger())
	run, err := client.GetPipelineRun(context.Background(), "owner", "repo", "123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.ID != "123" {
		t.Errorf("expected ID 123, got %s", run.ID)
	}
	if run.Status != integrations.StatusSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}
	if run.Branch != "main" {
		t.Errorf("expected main, got %s", run.Branch)
	}
}

func TestListPipelineRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ghRunsResponse{
			TotalCount: 1,
			WorkflowRuns: []ghWorkflowRun{
				{ID: 100, WorkflowID: 1, Status: "in_progress", HeadBranch: "feature"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{Token: "token", BaseURL: server.URL}, newTestLogger())
	runs, err := client.ListPipelineRuns(context.Background(), "owner", "repo", 10)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != integrations.StatusRunning {
		t.Errorf("expected running, got %s", runs[0].Status)
	}
}

func TestTriggerPipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/actions/workflows/ci.yml/dispatches" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(Config{Token: "token", BaseURL: server.URL}, newTestLogger())
	run, err := client.TriggerPipeline(context.Background(), "owner", "repo", "ci.yml", "main")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != integrations.StatusPending {
		t.Errorf("expected pending, got %s", run.Status)
	}
}

func TestMapGitHubStatus(t *testing.T) {
	tests := []struct {
		status     string
		conclusion string
		expected   integrations.PipelineStatus
	}{
		{"queued", "", integrations.StatusPending},
		{"waiting", "", integrations.StatusPending},
		{"pending", "", integrations.StatusPending},
		{"in_progress", "", integrations.StatusRunning},
		{"completed", "success", integrations.StatusSuccess},
		{"completed", "failure", integrations.StatusFailed},
		{"completed", "cancelled", integrations.StatusCancelled},
		{"completed", "skipped", integrations.StatusUnknown},
		{"something_else", "", integrations.StatusUnknown},
	}

	for _, tt := range tests {
		result := mapGitHubStatus(tt.status, tt.conclusion)
		if result != tt.expected {
			t.Errorf("mapGitHubStatus(%q, %q) = %s, want %s", tt.status, tt.conclusion, result, tt.expected)
		}
	}
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"repo", 1},
		{"repo, workflow, admin:org", 3},
	}

	for _, tt := range tests {
		result := parseScopes(tt.input)
		if len(result) != tt.expected {
			t.Errorf("parseScopes(%q) returned %d scopes, want %d", tt.input, len(result), tt.expected)
		}
	}
}

func TestAPIErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(Config{Token: "token", BaseURL: server.URL}, newTestLogger())

	_, err := client.ListPipelines(context.Background(), "owner", "repo")
	if err == nil {
		t.Error("expected error for 404 response")
	}

	_, err = client.GetPipelineRun(context.Background(), "owner", "repo", "999")
	if err == nil {
		t.Error("expected error for 404 response")
	}

	_, err = client.ListPipelineRuns(context.Background(), "owner", "repo", 10)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	client := NewClient(Config{Token: "token"}, newTestLogger())
	if client.config.BaseURL != "https://api.github.com" {
		t.Errorf("expected default base URL, got %s", client.config.BaseURL)
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	client := NewClient(Config{Token: "token", BaseURL: "https://api.github.com/"}, newTestLogger())
	if client.config.BaseURL != "https://api.github.com" {
		t.Errorf("expected trimmed URL, got %s", client.config.BaseURL)
	}
}
