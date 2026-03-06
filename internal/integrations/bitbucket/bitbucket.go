package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/logging"
)

// Config holds Bitbucket-specific configuration.
type Config struct {
	Username    string
	AppPassword string
	BaseURL     string // defaults to https://api.bitbucket.org/2.0
}

// Client implements integrations.Provider for Bitbucket Pipelines.
type Client struct {
	config     Config
	httpClient *http.Client
	logger     *logging.Logger
}

// NewClient creates a new Bitbucket Pipelines client.
func NewClient(cfg Config, logger *logging.Logger) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.bitbucket.org/2.0"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// SetHTTPClient allows overriding the default HTTP client (useful for testing).
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

func (c *Client) Name() integrations.Platform {
	return integrations.PlatformBitbucket
}

func (c *Client) TestConnection(ctx context.Context) (*integrations.ConnectionStatus, error) {
	start := time.Now()

	resp, err := c.doRequest(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return nil, fmt.Errorf("bitbucket connection test failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &integrations.ConnectionStatus{
			Connected: false,
			Platform:  integrations.PlatformBitbucket,
			Latency:   latency,
			Message:   fmt.Sprintf("authentication failed (HTTP %d): %s", resp.StatusCode, string(body)),
		}, nil
	}

	var user bbUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return &integrations.ConnectionStatus{
		Connected:   true,
		Platform:    integrations.PlatformBitbucket,
		User:        user.Username,
		Scopes:      []string{"pipeline", "repository"},
		RateLimitOK: true,
		Latency:     latency,
		Message:     fmt.Sprintf("connected as %s", user.DisplayName),
	}, nil
}

func (c *Client) ListPipelines(ctx context.Context, owner, repo string) ([]integrations.Pipeline, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pipelines/?sort=-created_on&pagelen=25", owner, repo)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list pipelines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitbucket API error: HTTP %d", resp.StatusCode)
	}

	var result bbPipelinesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode pipelines: %w", err)
	}

	pipelines := make([]integrations.Pipeline, 0, len(result.Values))
	for _, p := range result.Values {
		pipelines = append(pipelines, integrations.Pipeline{
			ID:         p.UUID,
			Name:       fmt.Sprintf("Pipeline #%d", p.BuildNumber),
			Platform:   integrations.PlatformBitbucket,
			Repository: fmt.Sprintf("%s/%s", owner, repo),
			URL:        fmt.Sprintf("https://bitbucket.org/%s/%s/pipelines/results/%d", owner, repo, p.BuildNumber),
		})
	}
	return pipelines, nil
}

func (c *Client) GetPipelineRun(ctx context.Context, owner, repo, runID string) (*integrations.PipelineRun, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pipelines/%s", owner, repo, runID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitbucket API error: HTTP %d", resp.StatusCode)
	}

	var pipeline bbPipeline
	if err := json.NewDecoder(resp.Body).Decode(&pipeline); err != nil {
		return nil, fmt.Errorf("failed to decode pipeline: %w", err)
	}

	return convertPipeline(&pipeline, owner, repo), nil
}

func (c *Client) ListPipelineRuns(ctx context.Context, owner, repo string, limit int) ([]integrations.PipelineRun, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pipelines/?sort=-created_on&pagelen=%d", owner, repo, limit)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list pipeline runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitbucket API error: HTTP %d", resp.StatusCode)
	}

	var result bbPipelinesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode pipelines: %w", err)
	}

	runs := make([]integrations.PipelineRun, 0, len(result.Values))
	for i := range result.Values {
		runs = append(runs, *convertPipeline(&result.Values[i], owner, repo))
	}
	return runs, nil
}

func (c *Client) TriggerPipeline(ctx context.Context, owner, repo, _ string, branch string) (*integrations.PipelineRun, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pipelines/", owner, repo)
	body := fmt.Sprintf(`{"target":{"ref_type":"branch","type":"pipeline_ref_target","ref_name":"%s"}}`, branch)
	resp, err := c.doRequest(ctx, http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to trigger pipeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bitbucket API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var pipeline bbPipeline
	if err := json.NewDecoder(resp.Body).Decode(&pipeline); err != nil {
		return nil, fmt.Errorf("failed to decode triggered pipeline: %w", err)
	}

	return convertPipeline(&pipeline, owner, repo), nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.config.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(c.config.Username, c.config.AppPassword)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// Bitbucket API response types

type bbUser struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UUID        string `json:"uuid"`
}

type bbPipeline struct {
	UUID        string    `json:"uuid"`
	BuildNumber int       `json:"build_number"`
	CreatedOn   time.Time `json:"created_on"`
	CompletedOn time.Time `json:"completed_on"`
	State       bbState   `json:"state"`
	Target      bbTarget  `json:"target"`
}

type bbState struct {
	Name   string   `json:"name"`
	Result bbResult `json:"result"`
}

type bbResult struct {
	Name string `json:"name"`
}

type bbTarget struct {
	RefType string   `json:"ref_type"`
	RefName string   `json:"ref_name"`
	Commit  bbCommit `json:"commit"`
}

type bbCommit struct {
	Hash string `json:"hash"`
}

type bbPipelinesResponse struct {
	Values []bbPipeline `json:"values"`
	Size   int          `json:"size"`
}

func convertPipeline(p *bbPipeline, owner, repo string) *integrations.PipelineRun {
	return &integrations.PipelineRun{
		ID:         p.UUID,
		PipelineID: fmt.Sprintf("%d", p.BuildNumber),
		Status:     mapBitbucketStatus(p.State),
		Branch:     p.Target.RefName,
		CommitSHA:  p.Target.Commit.Hash,
		StartedAt:  p.CreatedOn,
		FinishedAt: p.CompletedOn,
		URL:        fmt.Sprintf("https://bitbucket.org/%s/%s/pipelines/results/%d", owner, repo, p.BuildNumber),
	}
}

func mapBitbucketStatus(state bbState) integrations.PipelineStatus {
	switch strings.ToUpper(state.Name) {
	case "PENDING":
		return integrations.StatusPending
	case "IN_PROGRESS", "RUNNING":
		return integrations.StatusRunning
	case "COMPLETED":
		switch strings.ToUpper(state.Result.Name) {
		case "SUCCESSFUL":
			return integrations.StatusSuccess
		case "FAILED", "ERROR":
			return integrations.StatusFailed
		case "STOPPED":
			return integrations.StatusCancelled
		default:
			return integrations.StatusUnknown
		}
	default:
		return integrations.StatusUnknown
	}
}
