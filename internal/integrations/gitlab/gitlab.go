package gitlab

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

// Config holds GitLab-specific configuration.
type Config struct {
	Token   string
	BaseURL string // defaults to https://gitlab.com/api/v4
}

// Client implements integrations.Provider for GitLab CI/CD.
type Client struct {
	config     Config
	httpClient *http.Client
	logger     *logging.Logger
}

// NewClient creates a new GitLab CI/CD client.
func NewClient(cfg Config, logger *logging.Logger) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://gitlab.com/api/v4"
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
	return integrations.PlatformGitLab
}

func (c *Client) TestConnection(ctx context.Context) (*integrations.ConnectionStatus, error) {
	start := time.Now()

	resp, err := c.doRequest(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return nil, fmt.Errorf("gitlab connection test failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &integrations.ConnectionStatus{
			Connected: false,
			Platform:  integrations.PlatformGitLab,
			Latency:   latency,
			Message:   fmt.Sprintf("authentication failed (HTTP %d): %s", resp.StatusCode, string(body)),
		}, nil
	}

	var user glUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	scopes := parseTokenScopes(resp.Header)

	return &integrations.ConnectionStatus{
		Connected:   true,
		Platform:    integrations.PlatformGitLab,
		User:        user.Username,
		Scopes:      scopes,
		RateLimitOK: resp.Header.Get("RateLimit-Remaining") != "0",
		Latency:     latency,
		Message:     fmt.Sprintf("connected as %s", user.Name),
	}, nil
}

func (c *Client) ListPipelines(ctx context.Context, owner, repo string) ([]integrations.Pipeline, error) {
	projectID := fmt.Sprintf("%s%%2F%s", owner, repo)
	path := fmt.Sprintf("/projects/%s/pipelines?per_page=25&order_by=id&sort=desc", projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list pipelines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab API error: HTTP %d", resp.StatusCode)
	}

	var glPipelines []glPipeline
	if err := json.NewDecoder(resp.Body).Decode(&glPipelines); err != nil {
		return nil, fmt.Errorf("failed to decode pipelines: %w", err)
	}

	pipelines := make([]integrations.Pipeline, 0, len(glPipelines))
	for _, p := range glPipelines {
		pipelines = append(pipelines, integrations.Pipeline{
			ID:         fmt.Sprintf("%d", p.ID),
			Name:       fmt.Sprintf("Pipeline #%d", p.ID),
			Platform:   integrations.PlatformGitLab,
			Repository: fmt.Sprintf("%s/%s", owner, repo),
			URL:        p.WebURL,
		})
	}
	return pipelines, nil
}

func (c *Client) GetPipelineRun(ctx context.Context, owner, repo, runID string) (*integrations.PipelineRun, error) {
	projectID := fmt.Sprintf("%s%%2F%s", owner, repo)
	path := fmt.Sprintf("/projects/%s/pipelines/%s", projectID, runID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab API error: HTTP %d", resp.StatusCode)
	}

	var pipeline glPipelineDetail
	if err := json.NewDecoder(resp.Body).Decode(&pipeline); err != nil {
		return nil, fmt.Errorf("failed to decode pipeline: %w", err)
	}

	return convertPipeline(&pipeline), nil
}

func (c *Client) ListPipelineRuns(ctx context.Context, owner, repo string, limit int) ([]integrations.PipelineRun, error) {
	projectID := fmt.Sprintf("%s%%2F%s", owner, repo)
	path := fmt.Sprintf("/projects/%s/pipelines?per_page=%d&order_by=id&sort=desc", projectID, limit)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list pipeline runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab API error: HTTP %d", resp.StatusCode)
	}

	var glPipelines []glPipelineDetail
	if err := json.NewDecoder(resp.Body).Decode(&glPipelines); err != nil {
		return nil, fmt.Errorf("failed to decode pipelines: %w", err)
	}

	runs := make([]integrations.PipelineRun, 0, len(glPipelines))
	for i := range glPipelines {
		runs = append(runs, *convertPipeline(&glPipelines[i]))
	}
	return runs, nil
}

func (c *Client) TriggerPipeline(ctx context.Context, owner, repo, _ string, branch string) (*integrations.PipelineRun, error) {
	projectID := fmt.Sprintf("%s%%2F%s", owner, repo)
	path := fmt.Sprintf("/projects/%s/pipeline?ref=%s", projectID, branch)
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to trigger pipeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var pipeline glPipelineDetail
	if err := json.NewDecoder(resp.Body).Decode(&pipeline); err != nil {
		return nil, fmt.Errorf("failed to decode triggered pipeline: %w", err)
	}

	return convertPipeline(&pipeline), nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.config.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("PRIVATE-TOKEN", c.config.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// GitLab API response types

type glUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

type glPipeline struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	WebURL string `json:"web_url"`
}

type glPipelineDetail struct {
	ID         int       `json:"id"`
	Status     string    `json:"status"`
	Ref        string    `json:"ref"`
	SHA        string    `json:"sha"`
	WebURL     string    `json:"web_url"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

func convertPipeline(p *glPipelineDetail) *integrations.PipelineRun {
	return &integrations.PipelineRun{
		ID:         fmt.Sprintf("%d", p.ID),
		PipelineID: fmt.Sprintf("%d", p.ID),
		Status:     mapGitLabStatus(p.Status),
		Branch:     p.Ref,
		CommitSHA:  p.SHA,
		StartedAt:  p.StartedAt,
		FinishedAt: p.FinishedAt,
		URL:        p.WebURL,
	}
}

func mapGitLabStatus(status string) integrations.PipelineStatus {
	switch strings.ToLower(status) {
	case "pending", "waiting_for_resource", "preparing":
		return integrations.StatusPending
	case "running":
		return integrations.StatusRunning
	case "success":
		return integrations.StatusSuccess
	case "failed":
		return integrations.StatusFailed
	case "canceled", "skipped":
		return integrations.StatusCancelled
	default:
		return integrations.StatusUnknown
	}
}

func parseTokenScopes(headers http.Header) []string {
	scopeHeader := headers.Get("X-Oauth-Scopes")
	if scopeHeader == "" {
		return []string{"api"}
	}
	parts := strings.Split(scopeHeader, ",")
	scopes := make([]string, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes
}
