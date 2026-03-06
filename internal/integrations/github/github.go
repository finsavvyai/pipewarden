package github

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

// Config holds GitHub-specific configuration.
type Config struct {
	Token   string
	BaseURL string // defaults to https://api.github.com
}

// Client implements integrations.Provider for GitHub Actions.
type Client struct {
	config     Config
	httpClient *http.Client
	logger     *logging.Logger
}

// NewClient creates a new GitHub Actions client.
func NewClient(cfg Config, logger *logging.Logger) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
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
	return integrations.PlatformGitHub
}

func (c *Client) TestConnection(ctx context.Context) (*integrations.ConnectionStatus, error) {
	start := time.Now()

	resp, err := c.doRequest(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return nil, fmt.Errorf("github connection test failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &integrations.ConnectionStatus{
			Connected: false,
			Platform:  integrations.PlatformGitHub,
			Latency:   latency,
			Message:   fmt.Sprintf("authentication failed (HTTP %d): %s", resp.StatusCode, string(body)),
		}, nil
	}

	var user ghUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	scopes := parseScopes(resp.Header.Get("X-OAuth-Scopes"))

	return &integrations.ConnectionStatus{
		Connected:   true,
		Platform:    integrations.PlatformGitHub,
		User:        user.Login,
		Scopes:      scopes,
		RateLimitOK: resp.Header.Get("X-RateLimit-Remaining") != "0",
		Latency:     latency,
		Message:     fmt.Sprintf("connected as %s", user.Login),
	}, nil
}

func (c *Client) ListPipelines(ctx context.Context, owner, repo string) ([]integrations.Pipeline, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows", owner, repo)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API error: HTTP %d", resp.StatusCode)
	}

	var result ghWorkflowsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode workflows: %w", err)
	}

	pipelines := make([]integrations.Pipeline, 0, len(result.Workflows))
	for _, w := range result.Workflows {
		pipelines = append(pipelines, integrations.Pipeline{
			ID:         fmt.Sprintf("%d", w.ID),
			Name:       w.Name,
			Platform:   integrations.PlatformGitHub,
			Repository: fmt.Sprintf("%s/%s", owner, repo),
			URL:        w.HTMLURL,
		})
	}
	return pipelines, nil
}

func (c *Client) GetPipelineRun(ctx context.Context, owner, repo, runID string) (*integrations.PipelineRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%s", owner, repo, runID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API error: HTTP %d", resp.StatusCode)
	}

	var run ghWorkflowRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, fmt.Errorf("failed to decode run: %w", err)
	}

	return convertRun(&run), nil
}

func (c *Client) ListPipelineRuns(ctx context.Context, owner, repo string, limit int) ([]integrations.PipelineRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=%d", owner, repo, limit)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API error: HTTP %d", resp.StatusCode)
	}

	var result ghRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode runs: %w", err)
	}

	runs := make([]integrations.PipelineRun, 0, len(result.WorkflowRuns))
	for i := range result.WorkflowRuns {
		runs = append(runs, *convertRun(&result.WorkflowRuns[i]))
	}
	return runs, nil
}

func (c *Client) TriggerPipeline(ctx context.Context, owner, repo, workflow, branch string) (*integrations.PipelineRun, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", owner, repo, workflow)
	body := fmt.Sprintf(`{"ref":"%s"}`, branch)
	resp, err := c.doRequest(ctx, http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to trigger workflow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return &integrations.PipelineRun{
		Status: integrations.StatusPending,
		Branch: branch,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.config.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.config.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// GitHub API response types

type ghUser struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

type ghWorkflow struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

type ghWorkflowsResponse struct {
	TotalCount int          `json:"total_count"`
	Workflows  []ghWorkflow `json:"workflows"`
}

type ghWorkflowRun struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	WorkflowID   int       `json:"workflow_id"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	RunStartedAt time.Time `json:"run_started_at"`
}

type ghRunsResponse struct {
	TotalCount   int             `json:"total_count"`
	WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
}

func convertRun(run *ghWorkflowRun) *integrations.PipelineRun {
	return &integrations.PipelineRun{
		ID:         fmt.Sprintf("%d", run.ID),
		PipelineID: fmt.Sprintf("%d", run.WorkflowID),
		Status:     mapGitHubStatus(run.Status, run.Conclusion),
		Branch:     run.HeadBranch,
		CommitSHA:  run.HeadSHA,
		StartedAt:  run.RunStartedAt,
		FinishedAt: run.UpdatedAt,
		URL:        run.HTMLURL,
	}
}

func mapGitHubStatus(status, conclusion string) integrations.PipelineStatus {
	switch status {
	case "queued", "waiting", "pending":
		return integrations.StatusPending
	case "in_progress":
		return integrations.StatusRunning
	case "completed":
		switch conclusion {
		case "success":
			return integrations.StatusSuccess
		case "failure":
			return integrations.StatusFailed
		case "cancelled":
			return integrations.StatusCancelled
		default:
			return integrations.StatusUnknown
		}
	default:
		return integrations.StatusUnknown
	}
}

func parseScopes(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	scopes := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes
}
