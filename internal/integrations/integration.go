package integrations

import (
	"context"
	"time"
)

// Platform identifies a CI/CD platform.
type Platform string

const (
	PlatformGitHub    Platform = "github"
	PlatformBitbucket Platform = "bitbucket"
)

// PipelineStatus represents the current state of a pipeline run.
type PipelineStatus string

const (
	StatusPending    PipelineStatus = "pending"
	StatusRunning    PipelineStatus = "running"
	StatusSuccess    PipelineStatus = "success"
	StatusFailed     PipelineStatus = "failed"
	StatusCancelled  PipelineStatus = "cancelled"
	StatusUnknown    PipelineStatus = "unknown"
)

// Pipeline represents a CI/CD pipeline definition.
type Pipeline struct {
	ID         string
	Name       string
	Platform   Platform
	Repository string
	URL        string
}

// PipelineRun represents a single execution of a pipeline.
type PipelineRun struct {
	ID         string
	PipelineID string
	Status     PipelineStatus
	Branch     string
	CommitSHA  string
	StartedAt  time.Time
	FinishedAt time.Time
	URL        string
	Steps      []PipelineStep
}

// PipelineStep represents a single step within a pipeline run.
type PipelineStep struct {
	Name      string
	Status    PipelineStatus
	StartedAt time.Time
	Duration  time.Duration
	LogURL    string
}

// ConnectionStatus holds the result of a connection test.
type ConnectionStatus struct {
	Connected   bool
	Platform    Platform
	User        string
	Scopes      []string
	RateLimitOK bool
	Latency     time.Duration
	Message     string
}

// Provider defines the interface that all CI/CD platform integrations must implement.
type Provider interface {
	// Name returns the platform identifier.
	Name() Platform

	// TestConnection verifies that credentials are valid and the API is reachable.
	TestConnection(ctx context.Context) (*ConnectionStatus, error)

	// ListPipelines returns pipelines for the configured repository.
	ListPipelines(ctx context.Context, owner, repo string) ([]Pipeline, error)

	// GetPipelineRun returns details of a specific pipeline run.
	GetPipelineRun(ctx context.Context, owner, repo, runID string) (*PipelineRun, error)

	// ListPipelineRuns returns recent runs for a repository.
	ListPipelineRuns(ctx context.Context, owner, repo string, limit int) ([]PipelineRun, error)

	// TriggerPipeline starts a new pipeline run.
	TriggerPipeline(ctx context.Context, owner, repo, workflow, branch string) (*PipelineRun, error)
}
