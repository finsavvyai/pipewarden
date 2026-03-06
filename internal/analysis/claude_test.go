package analysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/logging"
)

func testLogger() *logging.Logger {
	l, _ := logging.New(&config.LoggingConfig{Level: "error", JSON: false})
	return l
}

func TestNewClaudeAnalyzer_Defaults(t *testing.T) {
	a := NewClaudeAnalyzer(ClaudeConfig{APIKey: "test-key"}, testLogger())
	if a.config.Model != defaultModel {
		t.Errorf("expected default model %q, got %q", defaultModel, a.config.Model)
	}
	if a.config.BaseURL != "https://api.anthropic.com" {
		t.Errorf("expected default BaseURL, got %q", a.config.BaseURL)
	}
}

func TestNewClaudeAnalyzer_CustomConfig(t *testing.T) {
	a := NewClaudeAnalyzer(ClaudeConfig{
		APIKey:  "key",
		Model:   "custom-model",
		BaseURL: "https://custom.api.com/",
	}, testLogger())
	if a.config.Model != "custom-model" {
		t.Errorf("expected custom model, got %q", a.config.Model)
	}
	if a.config.BaseURL != "https://custom.api.com" {
		t.Errorf("expected trailing slash trimmed, got %q", a.config.BaseURL)
	}
}

func TestEnabled(t *testing.T) {
	a := NewClaudeAnalyzer(ClaudeConfig{}, testLogger())
	if a.Enabled() {
		t.Error("expected Enabled() = false with empty API key")
	}
	a2 := NewClaudeAnalyzer(ClaudeConfig{APIKey: "key"}, testLogger())
	if !a2.Enabled() {
		t.Error("expected Enabled() = true with API key set")
	}
}

func TestCallClaude_Success(t *testing.T) {
	resp := claudeResponse{
		Content: []claudeContent{{Type: "text", Text: "Hello"}},
		Usage:   claudeUsage{InputTokens: 10, OutputTokens: 5},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("unexpected anthropic-version: %q", r.Header.Get("anthropic-version"))
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewClaudeAnalyzer(ClaudeConfig{APIKey: "test-key", BaseURL: server.URL}, testLogger())
	result, err := a.callClaude(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Hello" {
		t.Errorf("unexpected content: %+v", result.Content)
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", result.Usage.InputTokens)
	}
}

func TestCallClaude_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	a := NewClaudeAnalyzer(ClaudeConfig{APIKey: "key", BaseURL: server.URL}, testLogger())
	_, err := a.callClaude(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestParseClaudeResponse_ValidJSON(t *testing.T) {
	jsonResp := `{
		"summary": "No critical issues found",
		"risk_score": 25,
		"findings": [
			{
				"severity": "medium",
				"category": "configuration",
				"title": "Missing branch protection",
				"description": "Main branch has no protection rules",
				"remediation": "Enable branch protection",
				"confidence": 0.85
			}
		]
	}`

	content := []claudeContent{{Type: "text", Text: jsonResp}}
	findings, summary, riskScore := parseClaudeResponse(content, "test-conn", "run-1")

	if summary != "No critical issues found" {
		t.Errorf("unexpected summary: %q", summary)
	}
	if riskScore != 25 {
		t.Errorf("expected risk score 25, got %d", riskScore)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityMedium {
		t.Errorf("expected medium severity, got %q", findings[0].Severity)
	}
	if findings[0].ConnectionName != "test-conn" {
		t.Errorf("expected connection name test-conn, got %q", findings[0].ConnectionName)
	}
	if findings[0].Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", findings[0].Confidence)
	}
}

func TestParseClaudeResponse_MarkdownCodeBlock(t *testing.T) {
	text := "Here is my analysis:\n```json\n{\"summary\": \"All clear\", \"risk_score\": 0, \"findings\": []}\n```\nThat's it."
	content := []claudeContent{{Type: "text", Text: text}}
	findings, summary, riskScore := parseClaudeResponse(content, "conn", "run")

	if summary != "All clear" {
		t.Errorf("unexpected summary: %q", summary)
	}
	if riskScore != 0 {
		t.Errorf("expected risk score 0, got %d", riskScore)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestParseClaudeResponse_EmptyContent(t *testing.T) {
	findings, summary, riskScore := parseClaudeResponse(nil, "conn", "run")
	if summary != "No response from analysis" {
		t.Errorf("unexpected summary: %q", summary)
	}
	if riskScore != 0 {
		t.Errorf("expected risk score 0, got %d", riskScore)
	}
	if findings != nil {
		t.Errorf("expected nil findings, got %v", findings)
	}
}

func TestParseClaudeResponse_InvalidJSON(t *testing.T) {
	content := []claudeContent{{Type: "text", Text: "This is not JSON at all"}}
	findings, summary, _ := parseClaudeResponse(content, "conn", "run")
	if findings != nil {
		t.Errorf("expected nil findings for invalid JSON")
	}
	if summary != "This is not JSON at all" {
		t.Errorf("expected raw text as summary, got %q", summary)
	}
}

func TestParseClaudeResponse_MultipleFindings(t *testing.T) {
	jsonResp := `{
		"summary": "Multiple issues detected",
		"risk_score": 75,
		"findings": [
			{"severity": "critical", "category": "secrets", "title": "Exposed API key", "description": "API key in logs", "remediation": "Rotate key", "confidence": 0.95},
			{"severity": "high", "category": "access-control", "title": "Overly permissive", "description": "Write access too broad", "remediation": "Restrict", "confidence": 0.80},
			{"severity": "low", "category": "configuration", "title": "Missing timeout", "description": "No job timeout set", "remediation": "Add timeout", "confidence": 0.70}
		]
	}`
	content := []claudeContent{{Type: "text", Text: jsonResp}}
	findings, _, riskScore := parseClaudeResponse(content, "conn", "run")

	if riskScore != 75 {
		t.Errorf("expected risk score 75, got %d", riskScore)
	}
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("expected critical severity, got %q", findings[0].Severity)
	}
	if findings[2].Severity != SeverityLow {
		t.Errorf("expected low severity, got %q", findings[2].Severity)
	}
}

func TestBuildAnalysisPrompt(t *testing.T) {
	conn := &integrations.Connection{
		Name:     "test-conn",
		Platform: integrations.PlatformGitHub,
	}
	run := &integrations.PipelineRun{
		ID:        "123",
		Branch:    "main",
		CommitSHA: "abc123",
		Status:    "completed",
		URL:       "https://github.com/org/repo/actions/runs/123",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: "success", Duration: 30 * time.Second},
			{Name: "test", Status: "success", Duration: 45 * time.Second, LogURL: "https://example.com/logs"},
		},
	}

	prompt := buildAnalysisPrompt(conn, run)

	checks := []string{
		"DevSecOps security analyst",
		"github",
		"test-conn",
		"123",
		"main",
		"abc123",
		"Pipeline Steps",
		"build: status=success",
		"test: status=success",
		"https://example.com/logs",
		"risk_score",
		"findings",
	}
	for _, check := range checks {
		if !contains(prompt, check) {
			t.Errorf("prompt missing expected text: %q", check)
		}
	}
}

func TestAnalyzeRun_EndToEnd(t *testing.T) {
	analysisResp := claudeResponse{
		Content: []claudeContent{{
			Type: "text",
			Text: `{"summary": "Pipeline looks secure", "risk_score": 10, "findings": [{"severity": "info", "category": "configuration", "title": "Good practices detected", "description": "Pipeline follows security best practices", "remediation": "No action needed", "confidence": 0.9}]}`,
		}},
		Usage: claudeUsage{InputTokens: 100, OutputTokens: 50},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(analysisResp)
	}))
	defer server.Close()

	a := NewClaudeAnalyzer(ClaudeConfig{APIKey: "key", BaseURL: server.URL}, testLogger())

	conn := &integrations.Connection{
		Name:     "gh-main",
		Platform: integrations.PlatformGitHub,
	}
	run := &integrations.PipelineRun{
		ID:     "456",
		Branch: "main",
		Status: "completed",
	}

	result, err := a.AnalyzeRun(context.Background(), conn, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ConnectionName != "gh-main" {
		t.Errorf("expected connection name gh-main, got %q", result.ConnectionName)
	}
	if result.RunID != "456" {
		t.Errorf("expected run ID 456, got %q", result.RunID)
	}
	if result.RiskScore != 10 {
		t.Errorf("expected risk score 10, got %d", result.RiskScore)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.TokensUsed != 150 {
		t.Errorf("expected 150 tokens used, got %d", result.TokensUsed)
	}
	if result.DurationMS < 0 {
		t.Errorf("expected positive duration, got %d", result.DurationMS)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
