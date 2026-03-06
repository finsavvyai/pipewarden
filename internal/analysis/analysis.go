package analysis

import (
	"time"
)

// Severity levels for security findings.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Category classifies the type of security finding.
type Category string

const (
	CategoryInjection      Category = "injection"
	CategoryAuth           Category = "authentication"
	CategorySecrets        Category = "secrets"
	CategoryCrypto         Category = "cryptography"
	CategoryConfig         Category = "configuration"
	CategoryDependency     Category = "dependency"
	CategoryLogicFlaw      Category = "logic-flaw"
	CategoryAccessControl  Category = "access-control"
	CategoryDataExposure   Category = "data-exposure"
	CategoryOther          Category = "other"
)

// Finding represents a single security issue discovered by analysis.
type Finding struct {
	ID             int64    `json:"id,omitempty"`
	ConnectionName string   `json:"connection_name"`
	RunID          string   `json:"run_id"`
	Severity       Severity `json:"severity"`
	Category       Category `json:"category"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Remediation    string   `json:"remediation"`
	File           string   `json:"file,omitempty"`
	Line           int      `json:"line,omitempty"`
	Confidence     float64  `json:"confidence"` // 0.0 to 1.0
	FalsePositive  bool     `json:"false_positive"`
	Status         string   `json:"status"` // open, acknowledged, resolved, false_positive
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

// AnalysisResult holds the complete output of a security analysis run.
type AnalysisResult struct {
	ConnectionName string    `json:"connection_name"`
	RunID          string    `json:"run_id"`
	Findings       []Finding `json:"findings"`
	Summary        string    `json:"summary"`
	RiskScore      int       `json:"risk_score"` // 0-100
	TokensUsed     int       `json:"tokens_used"`
	Model          string    `json:"model"`
	AnalyzedAt     time.Time `json:"analyzed_at"`
	DurationMS     int64     `json:"duration_ms"`
}

// AnalysisRequest describes what to analyze.
type AnalysisRequest struct {
	ConnectionName string `json:"connection_name"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	RunID          string `json:"run_id"`
}
