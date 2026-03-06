package bitbucket

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
	client := NewClient(Config{Username: "user", AppPassword: "pass"}, newTestLogger())
	if client.Name() != integrations.PlatformBitbucket {
		t.Errorf("expected %s, got %s", integrations.PlatformBitbucket, client.Name())
	}
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "testuser" || pass != "app-pass" {
			t.Errorf("unexpected auth: %s/%s (ok=%v)", user, pass, ok)
		}
		json.NewEncoder(w).Encode(bbUser{
			Username:    "testuser",
			DisplayName: "Test User",
			UUID:        "{uuid-123}",
		})
	}))
	defer server.Close()

	client := NewClient(Config{
		Username:    "testuser",
		AppPassword: "app-pass",
		BaseURL:     server.URL,
	}, newTestLogger())

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
	if status.Platform != integrations.PlatformBitbucket {
		t.Errorf("expected platform bitbucket, got %s", status.Platform)
	}
}

func TestTestConnection_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid credentials"}}`))
	}))
	defer server.Close()

	client := NewClient(Config{
		Username:    "bad",
		AppPassword: "creds",
		BaseURL:     server.URL,
	}, newTestLogger())

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
		json.NewEncoder(w).Encode(bbPipelinesResponse{
			Size: 2,
			Values: []bbPipeline{
				{UUID: "{uuid-1}", BuildNumber: 10, State: bbState{Name: "COMPLETED", Result: bbResult{Name: "SUCCESSFUL"}}, Target: bbTarget{RefName: "main"}},
				{UUID: "{uuid-2}", BuildNumber: 11, State: bbState{Name: "IN_PROGRESS"}, Target: bbTarget{RefName: "feature"}},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{Username: "user", AppPassword: "pass", BaseURL: server.URL}, newTestLogger())
	pipelines, err := client.ListPipelines(context.Background(), "owner", "repo")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipelines) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(pipelines))
	}
	if pipelines[0].ID != "{uuid-1}" {
		t.Errorf("expected {uuid-1}, got %s", pipelines[0].ID)
	}
	if pipelines[0].Repository != "owner/repo" {
		t.Errorf("expected owner/repo, got %s", pipelines[0].Repository)
	}
}

func TestGetPipelineRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/owner/repo/pipelines/{uuid-1}" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(bbPipeline{
			UUID:        "{uuid-1}",
			BuildNumber: 42,
			State:       bbState{Name: "COMPLETED", Result: bbResult{Name: "SUCCESSFUL"}},
			Target: bbTarget{
				RefName: "main",
				Commit:  bbCommit{Hash: "deadbeef"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{Username: "user", AppPassword: "pass", BaseURL: server.URL}, newTestLogger())
	run, err := client.GetPipelineRun(context.Background(), "owner", "repo", "{uuid-1}")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.ID != "{uuid-1}" {
		t.Errorf("expected {uuid-1}, got %s", run.ID)
	}
	if run.Status != integrations.StatusSuccess {
		t.Errorf("expected success, got %s", run.Status)
	}
	if run.CommitSHA != "deadbeef" {
		t.Errorf("expected deadbeef, got %s", run.CommitSHA)
	}
}

func TestListPipelineRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(bbPipelinesResponse{
			Size: 1,
			Values: []bbPipeline{
				{UUID: "{uuid-1}", BuildNumber: 1, State: bbState{Name: "PENDING"}, Target: bbTarget{RefName: "dev"}},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{Username: "user", AppPassword: "pass", BaseURL: server.URL}, newTestLogger())
	runs, err := client.ListPipelineRuns(context.Background(), "owner", "repo", 5)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != integrations.StatusPending {
		t.Errorf("expected pending, got %s", runs[0].Status)
	}
}

func TestTriggerPipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(bbPipeline{
			UUID:        "{uuid-new}",
			BuildNumber: 99,
			State:       bbState{Name: "PENDING"},
			Target:      bbTarget{RefName: "main"},
		})
	}))
	defer server.Close()

	client := NewClient(Config{Username: "user", AppPassword: "pass", BaseURL: server.URL}, newTestLogger())
	run, err := client.TriggerPipeline(context.Background(), "owner", "repo", "", "main")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.ID != "{uuid-new}" {
		t.Errorf("expected {uuid-new}, got %s", run.ID)
	}
	if run.Status != integrations.StatusPending {
		t.Errorf("expected pending, got %s", run.Status)
	}
}

func TestMapBitbucketStatus(t *testing.T) {
	tests := []struct {
		state    bbState
		expected integrations.PipelineStatus
	}{
		{bbState{Name: "PENDING"}, integrations.StatusPending},
		{bbState{Name: "IN_PROGRESS"}, integrations.StatusRunning},
		{bbState{Name: "RUNNING"}, integrations.StatusRunning},
		{bbState{Name: "COMPLETED", Result: bbResult{Name: "SUCCESSFUL"}}, integrations.StatusSuccess},
		{bbState{Name: "COMPLETED", Result: bbResult{Name: "FAILED"}}, integrations.StatusFailed},
		{bbState{Name: "COMPLETED", Result: bbResult{Name: "ERROR"}}, integrations.StatusFailed},
		{bbState{Name: "COMPLETED", Result: bbResult{Name: "STOPPED"}}, integrations.StatusCancelled},
		{bbState{Name: "COMPLETED", Result: bbResult{Name: "OTHER"}}, integrations.StatusUnknown},
		{bbState{Name: "WEIRD"}, integrations.StatusUnknown},
	}

	for _, tt := range tests {
		result := mapBitbucketStatus(tt.state)
		if result != tt.expected {
			t.Errorf("mapBitbucketStatus(%v) = %s, want %s", tt.state, result, tt.expected)
		}
	}
}

func TestAPIErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(Config{Username: "user", AppPassword: "pass", BaseURL: server.URL}, newTestLogger())

	_, err := client.ListPipelines(context.Background(), "owner", "repo")
	if err == nil {
		t.Error("expected error for 404 response")
	}

	_, err = client.GetPipelineRun(context.Background(), "owner", "repo", "uuid")
	if err == nil {
		t.Error("expected error for 404 response")
	}

	_, err = client.ListPipelineRuns(context.Background(), "owner", "repo", 10)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	client := NewClient(Config{Username: "user", AppPassword: "pass"}, newTestLogger())
	if client.config.BaseURL != "https://api.bitbucket.org/2.0" {
		t.Errorf("expected default base URL, got %s", client.config.BaseURL)
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	client := NewClient(Config{Username: "user", AppPassword: "pass", BaseURL: "https://api.bitbucket.org/2.0/"}, newTestLogger())
	if client.config.BaseURL != "https://api.bitbucket.org/2.0" {
		t.Errorf("expected trimmed URL, got %s", client.config.BaseURL)
	}
}
