package gitlab

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
	c := NewClient(Config{Token: "test"}, newTestLogger())
	if c.Name() != integrations.PlatformGitLab {
		t.Errorf("expected gitlab, got %s", c.Name())
	}
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-test-token" {
			t.Errorf("expected PRIVATE-TOKEN header")
		}

		w.Header().Set("X-Oauth-Scopes", "api, read_user")
		w.Header().Set("RateLimit-Remaining", "100")
		json.NewEncoder(w).Encode(glUser{
			ID:       1,
			Username: "testuser",
			Name:     "Test User",
		})
	}))
	defer server.Close()

	c := NewClient(Config{Token: "glpat-test-token", BaseURL: server.URL}, newTestLogger())
	status, err := c.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Connected {
		t.Error("expected connected")
	}
	if status.User != "testuser" {
		t.Errorf("expected testuser, got %s", status.User)
	}
	if len(status.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(status.Scopes))
	}
	if !status.RateLimitOK {
		t.Error("expected rate limit OK")
	}
}

func TestTestConnection_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer server.Close()

	c := NewClient(Config{Token: "bad-token", BaseURL: server.URL}, newTestLogger())
	status, err := c.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Connected {
		t.Error("expected not connected")
	}
}

func TestListPipelines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]glPipeline{
			{ID: 100, Status: "success", Ref: "main", SHA: "abc123", WebURL: "https://gitlab.com/p/100"},
			{ID: 99, Status: "failed", Ref: "dev", SHA: "def456", WebURL: "https://gitlab.com/p/99"},
		})
	}))
	defer server.Close()

	c := NewClient(Config{Token: "test", BaseURL: server.URL}, newTestLogger())
	pipelines, err := c.ListPipelines(context.Background(), "myorg", "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipelines) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(pipelines))
	}
	if pipelines[0].ID != "100" {
		t.Errorf("expected pipeline ID 100, got %s", pipelines[0].ID)
	}
	if pipelines[0].Platform != integrations.PlatformGitLab {
		t.Errorf("expected gitlab platform")
	}
}

func TestGetPipelineRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(glPipelineDetail{
			ID:     42,
			Status: "running",
			Ref:    "main",
			SHA:    "abc123",
			WebURL: "https://gitlab.com/p/42",
		})
	}))
	defer server.Close()

	c := NewClient(Config{Token: "test", BaseURL: server.URL}, newTestLogger())
	run, err := c.GetPipelineRun(context.Background(), "myorg", "myrepo", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != integrations.StatusRunning {
		t.Errorf("expected running, got %s", run.Status)
	}
	if run.Branch != "main" {
		t.Errorf("expected main branch, got %s", run.Branch)
	}
}

func TestListPipelineRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]glPipelineDetail{
			{ID: 10, Status: "success", Ref: "main", SHA: "aaa"},
			{ID: 9, Status: "failed", Ref: "dev", SHA: "bbb"},
		})
	}))
	defer server.Close()

	c := NewClient(Config{Token: "test", BaseURL: server.URL}, newTestLogger())
	runs, err := c.ListPipelineRuns(context.Background(), "myorg", "myrepo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].Status != integrations.StatusSuccess {
		t.Errorf("expected success, got %s", runs[0].Status)
	}
	if runs[1].Status != integrations.StatusFailed {
		t.Errorf("expected failed, got %s", runs[1].Status)
	}
}

func TestTriggerPipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(glPipelineDetail{
			ID:     200,
			Status: "pending",
			Ref:    "main",
			SHA:    "newsha",
			WebURL: "https://gitlab.com/p/200",
		})
	}))
	defer server.Close()

	c := NewClient(Config{Token: "test", BaseURL: server.URL}, newTestLogger())
	run, err := c.TriggerPipeline(context.Background(), "myorg", "myrepo", "", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != integrations.StatusPending {
		t.Errorf("expected pending, got %s", run.Status)
	}
}

func TestMapGitLabStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected integrations.PipelineStatus
	}{
		{"pending", integrations.StatusPending},
		{"waiting_for_resource", integrations.StatusPending},
		{"preparing", integrations.StatusPending},
		{"running", integrations.StatusRunning},
		{"success", integrations.StatusSuccess},
		{"failed", integrations.StatusFailed},
		{"canceled", integrations.StatusCancelled},
		{"skipped", integrations.StatusCancelled},
		{"manual", integrations.StatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapGitLabStatus(tt.input)
			if got != tt.expected {
				t.Errorf("mapGitLabStatus(%q) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseTokenScopes(t *testing.T) {
	h := http.Header{}
	h.Set("X-Oauth-Scopes", "api, read_user, read_repository")
	scopes := parseTokenScopes(h)
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes, got %d", len(scopes))
	}

	// Empty header should default to "api"
	h2 := http.Header{}
	scopes2 := parseTokenScopes(h2)
	if len(scopes2) != 1 || scopes2[0] != "api" {
		t.Errorf("expected [api] for empty header, got %v", scopes2)
	}
}

func TestAPIErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(Config{Token: "test", BaseURL: server.URL}, newTestLogger())

	_, err := c.ListPipelines(context.Background(), "org", "repo")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	c := NewClient(Config{Token: "test"}, newTestLogger())
	if c.config.BaseURL != "https://gitlab.com/api/v4" {
		t.Errorf("expected default base URL, got %s", c.config.BaseURL)
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	c := NewClient(Config{Token: "test", BaseURL: "https://gitlab.example.com/api/v4/"}, newTestLogger())
	if c.config.BaseURL != "https://gitlab.example.com/api/v4" {
		t.Errorf("expected trailing slash removed, got %s", c.config.BaseURL)
	}
}
