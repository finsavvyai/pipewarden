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
	if len(m.Platforms()) != 0 {
		t.Errorf("expected 0 platforms, got %d", len(m.Platforms()))
	}
}

func TestRegisterAndGet(t *testing.T) {
	m := NewManager(newTestLogger())
	mock := &mockProvider{platform: PlatformGitHub}
	m.Register(mock)

	p, err := m.Get(PlatformGitHub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != PlatformGitHub {
		t.Errorf("expected github, got %s", p.Name())
	}
}

func TestGetNotFound(t *testing.T) {
	m := NewManager(newTestLogger())
	_, err := m.Get(PlatformBitbucket)
	if err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestPlatforms(t *testing.T) {
	m := NewManager(newTestLogger())
	m.Register(&mockProvider{platform: PlatformGitHub})
	m.Register(&mockProvider{platform: PlatformBitbucket})

	platforms := m.Platforms()
	if len(platforms) != 2 {
		t.Fatalf("expected 2 platforms, got %d", len(platforms))
	}

	found := make(map[Platform]bool)
	for _, p := range platforms {
		found[p] = true
	}
	if !found[PlatformGitHub] || !found[PlatformBitbucket] {
		t.Error("expected both github and bitbucket platforms")
	}
}

func TestTestAllConnections_AllSuccess(t *testing.T) {
	m := NewManager(newTestLogger())

	m.Register(&mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformGitHub,
			User:      "ghuser",
			Latency:   50 * time.Millisecond,
		},
	})
	m.Register(&mockProvider{
		platform: PlatformBitbucket,
		connectStatus: &ConnectionStatus{
			Connected: true,
			Platform:  PlatformBitbucket,
			User:      "bbuser",
			Latency:   60 * time.Millisecond,
		},
	})

	results := m.TestAllConnections(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[PlatformGitHub].Connected {
		t.Error("expected github connected")
	}
	if !results[PlatformBitbucket].Connected {
		t.Error("expected bitbucket connected")
	}
}

func TestTestAllConnections_WithError(t *testing.T) {
	m := NewManager(newTestLogger())

	m.Register(&mockProvider{
		platform:   PlatformGitHub,
		connectErr: fmt.Errorf("network timeout"),
	})

	results := m.TestAllConnections(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[PlatformGitHub].Connected {
		t.Error("expected github not connected due to error")
	}
	if results[PlatformGitHub].Message != "network timeout" {
		t.Errorf("expected 'network timeout' message, got %s", results[PlatformGitHub].Message)
	}
}

func TestTestAllConnections_Empty(t *testing.T) {
	m := NewManager(newTestLogger())
	results := m.TestAllConnections(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRegisterOverwrite(t *testing.T) {
	m := NewManager(newTestLogger())

	mock1 := &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			User:      "user1",
		},
	}
	mock2 := &mockProvider{
		platform: PlatformGitHub,
		connectStatus: &ConnectionStatus{
			Connected: true,
			User:      "user2",
		},
	}

	m.Register(mock1)
	m.Register(mock2)

	if len(m.Platforms()) != 1 {
		t.Errorf("expected 1 platform after overwrite, got %d", len(m.Platforms()))
	}

	results := m.TestAllConnections(context.Background())
	if results[PlatformGitHub].User != "user2" {
		t.Errorf("expected user2 after overwrite, got %s", results[PlatformGitHub].User)
	}
}
