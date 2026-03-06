package analysis

import (
	"testing"
	"time"

	"github.com/finsavvyai/pipewarden/internal/integrations"
)

func TestHeuristicAnalyzer_EmptyRun(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "test-gh", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "1",
		Status: integrations.StatusSuccess,
		Branch: "feature/test",
	}

	result := h.AnalyzeRun(conn, run)
	if result.ConnectionName != "test-gh" {
		t.Errorf("expected connection name test-gh, got %s", result.ConnectionName)
	}
	if result.Model != "heuristic-v1" {
		t.Errorf("expected model heuristic-v1, got %s", result.Model)
	}
	if result.TokensUsed != 0 {
		t.Errorf("expected 0 tokens, got %d", result.TokensUsed)
	}
	// Should find "no steps available" issue
	hasNoSteps := false
	for _, f := range result.Findings {
		if f.Title == "No pipeline steps available for inspection" {
			hasNoSteps = true
		}
	}
	if !hasNoSteps {
		t.Error("expected 'no steps' finding when run has no steps")
	}
}

func TestHeuristicAnalyzer_MainBranch(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-prod", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "42",
		Status: integrations.StatusSuccess,
		Branch: "main",
		Steps:  []integrations.PipelineStep{{Name: "build", Status: integrations.StatusSuccess}},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Category == CategoryAccessControl && f.Severity == SeverityMedium {
			found = true
		}
	}
	if !found {
		t.Error("expected access-control finding for direct push to main")
	}
}

func TestHeuristicAnalyzer_FailedSecurityStep(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-test", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "100",
		Status: integrations.StatusSuccess,
		Branch: "feature/x",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 30 * time.Second},
			{Name: "security-scan", Status: integrations.StatusFailed, Duration: 15 * time.Second},
			{Name: "deploy", Status: integrations.StatusSuccess, Duration: 20 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	foundFailedScan := false
	for _, f := range result.Findings {
		if f.Severity == SeverityHigh && f.Category == CategoryConfig {
			if f.Title == "Security step 'security-scan' failed" {
				foundFailedScan = true
			}
		}
	}
	if !foundFailedScan {
		t.Error("expected high-severity finding for failed security step")
	}
}

func TestHeuristicAnalyzer_NoSecuritySteps(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gl-test", Platform: integrations.PlatformGitLab}
	run := &integrations.PipelineRun{
		ID:     "200",
		Status: integrations.StatusSuccess,
		Branch: "develop",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 30 * time.Second},
			{Name: "deploy", Status: integrations.StatusSuccess, Duration: 20 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Title == "No security scanning steps detected" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for missing security steps")
	}
}

func TestHeuristicAnalyzer_DeployWithoutTests(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "bb-test", Platform: integrations.PlatformBitbucket}
	run := &integrations.PipelineRun{
		ID:     "300",
		Status: integrations.StatusSuccess,
		Branch: "release/1.0",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 30 * time.Second},
			{Name: "deploy-production", Status: integrations.StatusSuccess, Duration: 60 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Title == "Deployment without test steps" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for deployment without test steps")
	}
}

func TestHeuristicAnalyzer_LongRunning(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-test", Platform: integrations.PlatformGitHub}
	start := time.Now().Add(-3 * time.Hour)
	run := &integrations.PipelineRun{
		ID:         "400",
		Status:     integrations.StatusSuccess,
		Branch:     "main",
		StartedAt:  start,
		FinishedAt: start.Add(3 * time.Hour),
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 3 * time.Hour},
		},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Category == CategoryConfig && f.Severity == SeverityMedium {
			if len(f.Title) > 0 && f.Title[0:10] == "Unusually " {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected finding for long-running pipeline")
	}
}

func TestHeuristicAnalyzer_FailedRun(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-test", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "500",
		Status: integrations.StatusFailed,
		Branch: "feature/y",
		Steps: []integrations.PipelineStep{
			{Name: "test", Status: integrations.StatusFailed, Duration: 5 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Title == "Failed pipeline run detected" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for failed run")
	}
}

func TestHeuristicAnalyzer_CancelledRun(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-test", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "600",
		Status: integrations.StatusCancelled,
		Branch: "feature/z",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusCancelled},
		},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Title == "Pipeline run was cancelled" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for cancelled run")
	}
}

func TestHeuristicAnalyzer_RiskScoreCapping(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-test", Platform: integrations.PlatformGitHub}
	// Create a run that triggers many findings
	run := &integrations.PipelineRun{
		ID:     "700",
		Status: integrations.StatusFailed,
		Branch: "main",
		Steps: []integrations.PipelineStep{
			{Name: "deploy-prod", Status: integrations.StatusSuccess, Duration: 1 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	if result.RiskScore > 100 {
		t.Errorf("risk score should be capped at 100, got %d", result.RiskScore)
	}
}

func TestHeuristicAnalyzer_SuspiciousBranch(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-test", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "800",
		Status: integrations.StatusSuccess,
		Branch: "hack/bypass-auth",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 10 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	found := false
	for _, f := range result.Findings {
		if f.Title == "Pipeline run from temporary branch" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for suspicious branch name")
	}
}

func TestHeuristicAnalyzer_CleanRun(t *testing.T) {
	h := NewHeuristicAnalyzer()
	conn := &integrations.Connection{Name: "gh-clean", Platform: integrations.PlatformGitHub}
	run := &integrations.PipelineRun{
		ID:     "900",
		Status: integrations.StatusSuccess,
		Branch: "feature/good",
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 30 * time.Second},
			{Name: "test", Status: integrations.StatusSuccess, Duration: 60 * time.Second},
			{Name: "security-scan", Status: integrations.StatusSuccess, Duration: 45 * time.Second},
		},
	}

	result := h.AnalyzeRun(conn, run)
	// Clean run should have low risk
	if result.RiskScore > 20 {
		t.Errorf("expected low risk score for clean run, got %d", result.RiskScore)
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
}
