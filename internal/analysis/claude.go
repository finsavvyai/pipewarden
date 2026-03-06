package analysis

import (
	"bytes"
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

const defaultModel = "claude-sonnet-4-20250514"

// ClaudeConfig holds configuration for the Claude API client.
type ClaudeConfig struct {
	APIKey  string
	Model   string
	BaseURL string // defaults to https://api.anthropic.com
}

// ClaudeAnalyzer implements security analysis using the Anthropic Claude API.
type ClaudeAnalyzer struct {
	config     ClaudeConfig
	httpClient *http.Client
	logger     *logging.Logger
}

// NewClaudeAnalyzer creates a new Claude-powered security analyzer.
func NewClaudeAnalyzer(cfg ClaudeConfig, logger *logging.Logger) *ClaudeAnalyzer {
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	return &ClaudeAnalyzer{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logger,
	}
}

// SetHTTPClient allows overriding the HTTP client for testing.
func (a *ClaudeAnalyzer) SetHTTPClient(client *http.Client) {
	a.httpClient = client
}

// AnalyzeRun performs security analysis on a pipeline run using Claude.
func (a *ClaudeAnalyzer) AnalyzeRun(ctx context.Context, conn *integrations.Connection, run *integrations.PipelineRun) (*AnalysisResult, error) {
	start := time.Now()

	prompt := buildAnalysisPrompt(conn, run)

	a.logger.Infow("Starting Claude security analysis",
		"connection", conn.Name,
		"run_id", run.ID,
		"model", a.config.Model,
	)

	response, err := a.callClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude API call failed: %w", err)
	}

	findings, summary, riskScore := parseClaudeResponse(response.Content, conn.Name, run.ID)

	result := &AnalysisResult{
		ConnectionName: conn.Name,
		RunID:          run.ID,
		Findings:       findings,
		Summary:        summary,
		RiskScore:      riskScore,
		TokensUsed:     response.Usage.InputTokens + response.Usage.OutputTokens,
		Model:          a.config.Model,
		AnalyzedAt:     time.Now().UTC(),
		DurationMS:     time.Since(start).Milliseconds(),
	}

	a.logger.Infow("Security analysis complete",
		"connection", conn.Name,
		"run_id", run.ID,
		"findings", len(findings),
		"risk_score", riskScore,
		"tokens_used", result.TokensUsed,
		"duration_ms", result.DurationMS,
	)

	return result, nil
}

// Enabled returns whether the analyzer has an API key configured.
func (a *ClaudeAnalyzer) Enabled() bool {
	return a.config.APIKey != ""
}

func buildAnalysisPrompt(conn *integrations.Connection, run *integrations.PipelineRun) string {
	var sb strings.Builder

	sb.WriteString("You are a DevSecOps security analyst. Analyze this CI/CD pipeline run for security issues.\n\n")
	sb.WriteString("## Pipeline Context\n")
	sb.WriteString(fmt.Sprintf("- Platform: %s\n", conn.Platform))
	sb.WriteString(fmt.Sprintf("- Connection: %s\n", conn.Name))
	sb.WriteString(fmt.Sprintf("- Run ID: %s\n", run.ID))
	sb.WriteString(fmt.Sprintf("- Branch: %s\n", run.Branch))
	sb.WriteString(fmt.Sprintf("- Commit: %s\n", run.CommitSHA))
	sb.WriteString(fmt.Sprintf("- Status: %s\n", run.Status))
	sb.WriteString(fmt.Sprintf("- URL: %s\n", run.URL))

	if len(run.Steps) > 0 {
		sb.WriteString("\n## Pipeline Steps\n")
		for _, step := range run.Steps {
			sb.WriteString(fmt.Sprintf("- %s: status=%s duration=%s\n", step.Name, step.Status, step.Duration))
			if step.LogURL != "" {
				sb.WriteString(fmt.Sprintf("  log: %s\n", step.LogURL))
			}
		}
	}

	sb.WriteString("\n## Analysis Instructions\n")
	sb.WriteString("Analyze this pipeline run for security concerns. Look for:\n")
	sb.WriteString("1. Failed security-related steps (SAST, DAST, dependency scans)\n")
	sb.WriteString("2. Missing security checks that should be present\n")
	sb.WriteString("3. Pipeline configuration weaknesses (overly permissive permissions, missing approvals)\n")
	sb.WriteString("4. Branch protection concerns (deploying from non-protected branches)\n")
	sb.WriteString("5. Secret exposure risks in pipeline configuration\n")
	sb.WriteString("6. Supply chain security issues\n\n")

	sb.WriteString("Respond in this exact JSON format:\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{
  "summary": "Brief overall assessment",
  "risk_score": 0,
  "findings": [
    {
      "severity": "critical|high|medium|low|info",
      "category": "injection|authentication|secrets|cryptography|configuration|dependency|logic-flaw|access-control|data-exposure|other",
      "title": "Short title",
      "description": "Detailed description of the issue",
      "remediation": "How to fix it",
      "confidence": 0.85
    }
  ]
}`)
	sb.WriteString("\n```\n")
	sb.WriteString("If no issues found, return an empty findings array with a positive summary and risk_score of 0.")

	return sb.String()
}

// Claude API types

type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []claudeMessage  `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []claudeContent `json:"content"`
	Usage   claudeUsage     `json:"usage"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (a *ClaudeAnalyzer) callClaude(ctx context.Context, prompt string) (*claudeResponse, error) {
	reqBody := claudeRequest{
		Model:     a.config.Model,
		MaxTokens: 4096,
		Messages: []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// parseClaudeResponse extracts structured findings from Claude's JSON response.
func parseClaudeResponse(content []claudeContent, connName, runID string) ([]Finding, string, int) {
	if len(content) == 0 {
		return nil, "No response from analysis", 0
	}

	text := content[0].Text

	// Extract JSON from markdown code block if present
	if idx := strings.Index(text, "```json"); idx >= 0 {
		text = text[idx+7:]
		if end := strings.Index(text, "```"); end >= 0 {
			text = text[:end]
		}
	} else if idx := strings.Index(text, "```"); idx >= 0 {
		text = text[idx+3:]
		if end := strings.Index(text, "```"); end >= 0 {
			text = text[:end]
		}
	}

	text = strings.TrimSpace(text)

	var parsed struct {
		Summary   string  `json:"summary"`
		RiskScore int     `json:"risk_score"`
		Findings  []struct {
			Severity    string  `json:"severity"`
			Category    string  `json:"category"`
			Title       string  `json:"title"`
			Description string  `json:"description"`
			Remediation string  `json:"remediation"`
			File        string  `json:"file"`
			Line        int     `json:"line"`
			Confidence  float64 `json:"confidence"`
		} `json:"findings"`
	}

	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		// If JSON parsing fails, treat the whole response as a summary
		return nil, text, 0
	}

	findings := make([]Finding, 0, len(parsed.Findings))
	for _, f := range parsed.Findings {
		findings = append(findings, Finding{
			ConnectionName: connName,
			RunID:          runID,
			Severity:       Severity(f.Severity),
			Category:       Category(f.Category),
			Title:          f.Title,
			Description:    f.Description,
			Remediation:    f.Remediation,
			File:           f.File,
			Line:           f.Line,
			Confidence:     f.Confidence,
			Status:         "open",
		})
	}

	return findings, parsed.Summary, parsed.RiskScore
}
