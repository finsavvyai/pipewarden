package integrations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/logging"
)

func newTestLogger() *logging.Logger {
	cfg := &config.LoggingConfig{Level: "info", JSON: true}
	logger, _ := logging.New(cfg)
	return logger
}

// mockProvider implements Provider for testing.
type mockProvider struct {
	platform      Platform
	connectStatus *ConnectionStatus
	connectErr    error
	pipelines     []Pipeline
	runs          []PipelineRun
}

func (m *mockProvider) Name() Platform { return m.platform }
func (m *mockProvider) TestConnection(_ context.Context) (*ConnectionStatus, error) {
	return m.connectStatus, m.connectErr
}
func (m *mockProvider) ListPipelines(_ context.Context, _, _ string) ([]Pipeline, error) {
	return m.pipelines, nil
}
func (m *mockProvider) GetPipelineRun(_ context.Context, _, _, _ string) (*PipelineRun, error) {
	if len(m.runs) > 0 {
		return &m.runs[0], nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockProvider) ListPipelineRuns(_ context.Context, _, _ string, _ int) ([]PipelineRun, error) {
	return m.runs, nil
}
func (m *mockProvider) TriggerPipeline(_ context.Context, _, _, _, _ string) (*PipelineRun, error) {
	return &PipelineRun{Status: StatusPending}, nil
}

func TestNewManager(t *testing.T) {
	m := NewManager(newTestLogger())
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 connections, got %d", m.Count())
	}
}

func TestAddAndGet(t *testing.T) {
	m := NewManager(newTestLogger())
	mock := &mockProvider{platform: PlatformGitHub}
	if err := m.Add("gh-main", mock); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conn, err := m.Get("gh-main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Name != "gh-main" {
		t.Errorf("expected gh-main, got %s", conn.Name)
	}
	if conn.Platform != PlatformGitHub {
		t.Errorf("expected github, got %s", conn.Platform)
	}
}

func TestAddDuplicate(t *testing.T) {
	m := NewManager(newTestLogger())
	mock := &mockProvider{platform: PlatformGitHub}
	m.Add("gh-main", mock)

	err := m.Add("gh-main", mock)
	if err == nil {
		t.Error("expected error for duplicate name")
	}
}

func TestReplace(t *testing.T) {
	m := NewManager(newTestLogger())
	mock1 := &mockProvider{platform: PlatformGitHub, connectStatus: &ConnectionStatus{User: "user1"}}
	mock2 := &mockProvider{platform: PlatformGitHub, connectStatus: &ConnectionStatus{User: "user2"}}

	m.Add("gh-main", mock1)
	m.Replace("gh-main", mock2)

	if m.Count() != 1 {
		t.Errorf("expected 1 connection, got %d", m.Count())
	}

	status, _ := m.TestConnection(context.Background(), "gh-main")
	if status.User != "user2" {
		t.Errorf("expected user2 after replace, got %s", status.User)
	}
}

func TestRemove(t *testing.T) {
	m := NewManager(newTestLogger())
	m.Add("gh-main", &mockProvider{platform: PlatformGitHub})

	if err := m.Remove("gh-main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Count() != 0 {
		t.Error("expected 0 connections after remove")
	}
}

func TestRemoveNotFound(t *testing.T) {
	m := NewManager(newTestLogger())
	err := m.Remove("nonexistent")
	if err == nil {
		t.Error("expected error for removing nonexistent connection")
	}
}

func TestGetNotFound(t *testing.T) {
	m := NewManager(newTestLogger())
	_, err := m.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing connection")
	}
}

func TestList(t *testing.T) {
	m := NewManager(newTestLogger())
	m.Add("gh-main", &mockProvider{platform: PlatformGitHub})
	m.Add("gl-cloud", &mockProvider{platform: PlatformGitLab})
	m.Add("bb-team", &mockProvider{platform: PlatformBitbucket})

	conns := m.List()
	if len(conns) != 3 {
		t.Fatalf("expected 3 connections, got %d", len(conns))
	}
}

func TestGetByPlatform(t *testing.T) {
	m := NewManager(newTestLogger())
	m.Add("gh-main", &mockProvider{platform: PlatformGitHub})
	m.Add("gh-enterprise", &mockProvider{platform: PlatformGitHub})
	m.Add("gl-cloud", &mockProvider{platform: PlatformGitLab})

	ghConns := m.GetByPlatform(PlatformGitHub)
	if len(ghConns) != 2 {
		t.Fatalf("expected 2 github connections, got %d", len(ghConns))
	}

	glConns := m.GetByPlatform(PlatformGitLab)
	if len(glConns) != 1 {
		t.Fatalf("expected 1 gitlab connection, got %d", len(glConns))
	}

	bbConns := m.GetByPlatform(PlatformBitbucket)
	if len(bbConns) != 0 {
		t.Fatalf("expected 0 bitbucket connections, got %d", len(bbConns))
	}
}

func TestMultipleConnectionsSamePlatform(t *testing.T) {
	m := NewManager(newTestLogger())

	m.Add("gh-org-a", &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "org-a-bot",
		},
	})
	m.Add("gh-org-b", &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "org-b-bot",
		},
	})
	m.Add("gh-personal", &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "personal",
		},
	})

	if m.Count() != 3 {
		t.Fatalf("expected 3 connections, got %d", m.Count())
	}

	results := m.TestByPlatform(context.Background(), PlatformGitHub)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	users := make(map[string]bool)
	for _, status := range results {
		users[status.User] = true
	}
	if !users["org-a-bot"] || !users["org-b-bot"] || !users["personal"] {
		t.Errorf("missing expected users, got %v", users)
	}
}

func TestTestConnection_Single(t *testing.T) {
	m := NewManager(newTestLogger())
	m.Add("gh-main", &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "testuser",
		},
	})

	status, err := m.TestConnection(context.Background(), "gh-main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Connected {
		t.Error("expected connected")
	}
	if status.ConnectionName != "gh-main" {
		t.Errorf("expected connection name gh-main, got %s", status.ConnectionName)
	}
}

func TestTestConnection_NotFound(t *testing.T) {
	m := NewManager(newTestLogger())
	_, err := m.TestConnection(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent connection")
	}
}

func TestTestConnection_ProviderError(t *testing.T) {
	m := NewManager(newTestLogger())
	m.Add("gh-broken", &mockProvider{
		platform:   PlatformGitHub,
		connectErr: fmt.Errorf("token expired"),
	})

	status, err := m.TestConnection(context.Background(), "gh-broken")
	if err != nil {
		t.Fatalf("expected nil error (graceful), got %v", err)
	}
	if status.Connected {
		t.Error("expected not connected")
	}
	if status.Message != "token expired" {
		t.Errorf("expected 'token expired', got %s", status.Message)
	}
	if status.ConnectionName != "gh-broken" {
		t.Errorf("expected connection name gh-broken, got %s", status.ConnectionName)
	}
}

func TestTestAllConnections_MultiPlatform(t *testing.T) {
	m := NewManager(newTestLogger())

	m.Add("gh-main", &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "ghuser",
			Latency:   50 * time.Millisecond,
		},
	})
	m.Add("gh-enterprise", &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "ghentuser",
			Latency:   80 * time.Millisecond,
		},
	})
	m.Add("bb-team", &mockProvider{
		platform: PlatformBitbucket,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformBitbucket,
			User:      "bbuser",
			Latency:   60 * time.Millisecond,
		},
	})

	results := m.TestAllConnections(context.Background())
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if !results["gh-main"].Connected {
		t.Error("expected gh-main connected")
	}
	if !results["gh-enterprise"].Connected {
		t.Error("expected gh-enterprise connected")
	}
	if !results["bb-team"].Connected {
		t.Error("expected bb-team connected")
	}
}

func TestTestAllConnections_WithError(t *testing.T) {
	m := NewManager(newTestLogger())

	m.Add("gh-broken", &mockProvider{
		platform:   PlatformGitHub,
		connectErr: fmt.Errorf("network timeout"),
	})

	results := m.TestAllConnections(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results["gh-broken"].Connected {
		t.Error("expected not connected due to error")
	}
	if results["gh-broken"].Message != "network timeout" {
		t.Errorf("expected 'network timeout', got %s", results["gh-broken"].Message)
	}
}

func TestTestAllConnections_Empty(t *testing.T) {
	m := NewManager(newTestLogger())
	results := m.TestAllConnections(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestTestByPlatform(t *testing.T) {
	m := NewManager(newTestLogger())

	m.Add("gh-1", &mockProvider{
		platform:      PlatformGitHub,
		connectStatus: &ConnectionStatus{Connected: true, Platform: PlatformGitHub, User: "u1"},
	})
	m.Add("gh-2", &mockProvider{
		platform:      PlatformGitHub,
		connectStatus: &ConnectionStatus{Connected: true, Platform: PlatformGitHub, User: "u2"},
	})
	m.Add("gl-1", &mockProvider{
		platform:      PlatformGitLab,
		connectStatus: &ConnectionStatus{Connected: true, Platform: PlatformGitLab, User: "u3"},
	})

	ghResults := m.TestByPlatform(context.Background(), PlatformGitHub)
	if len(ghResults) != 2 {
		t.Fatalf("expected 2 github results, got %d", len(ghResults))
	}

	glResults := m.TestByPlatform(context.Background(), PlatformGitLab)
	if len(glResults) != 1 {
		t.Fatalf("expected 1 gitlab result, got %d", len(glResults))
	}

	bbResults := m.TestByPlatform(context.Background(), PlatformBitbucket)
	if len(bbResults) != 0 {
		t.Fatalf("expected 0 bitbucket results, got %d", len(bbResults))
	}
}
