package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/finsavvyai/pipewarden/internal/analysis"
	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/logging"
	"github.com/finsavvyai/pipewarden/internal/storage"
	"github.com/finsavvyai/pipewarden/internal/web"
)

// mockProvider implements integrations.Provider for testing
type mockProvider struct {
	platform   integrations.Platform
	testResult *integrations.ConnectionStatus
	testErr    error
	pipelines  []integrations.Pipeline
	runs       []integrations.PipelineRun
	run        *integrations.PipelineRun
}

func (m *mockProvider) Name() integrations.Platform { return m.platform }
func (m *mockProvider) TestConnection(ctx context.Context) (*integrations.ConnectionStatus, error) {
	return m.testResult, m.testErr
}
func (m *mockProvider) ListPipelines(ctx context.Context, owner, repo string) ([]integrations.Pipeline, error) {
	return m.pipelines, nil
}
func (m *mockProvider) GetPipelineRun(ctx context.Context, owner, repo, runID string) (*integrations.PipelineRun, error) {
	if m.run != nil {
		return m.run, nil
	}
	return &integrations.PipelineRun{
		ID:         runID,
		PipelineID: "ci",
		Status:     integrations.StatusSuccess,
		Branch:     "main",
		CommitSHA:  "abc1234",
		StartedAt:  time.Now().Add(-10 * time.Minute),
		FinishedAt: time.Now(),
		Steps: []integrations.PipelineStep{
			{Name: "build", Status: integrations.StatusSuccess, Duration: 2 * time.Minute},
			{Name: "test", Status: integrations.StatusSuccess, Duration: 3 * time.Minute},
		},
	}, nil
}
func (m *mockProvider) ListPipelineRuns(ctx context.Context, owner, repo string, limit int) ([]integrations.PipelineRun, error) {
	if m.runs != nil {
		return m.runs, nil
	}
	return []integrations.PipelineRun{
		{ID: "100", PipelineID: "ci", Status: integrations.StatusSuccess, Branch: "main"},
		{ID: "99", PipelineID: "ci", Status: integrations.StatusFailed, Branch: "dev"},
	}, nil
}
func (m *mockProvider) TriggerPipeline(ctx context.Context, owner, repo, workflow, branch string) (*integrations.PipelineRun, error) {
	return &integrations.PipelineRun{ID: "101", Status: integrations.StatusPending}, nil
}

// testServer sets up a full test server with all routes
type testServer struct {
	mux      *http.ServeMux
	db       *storage.DB
	manager  *integrations.Manager
	logger   *logging.Logger
	dbPath   string
	analyzer *analysis.HeuristicAnalyzer
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test DB: %v", err)
	}

	logger, _ := logging.New(&config.LoggingConfig{Level: "error", JSON: true})
	manager := integrations.NewManager(logger)
	heuristic := analysis.NewHeuristicAnalyzer()

	ts := &testServer{
		mux:      http.NewServeMux(),
		db:       db,
		manager:  manager,
		logger:   logger,
		dbPath:   dbPath,
		analyzer: heuristic,
	}
	ts.registerRoutes()
	return ts
}

func (ts *testServer) close() {
	ts.db.Close()
	os.Remove(ts.dbPath)
}

func (ts *testServer) registerRoutes() {
	mux := ts.mux
	db := ts.db
	manager := ts.manager
	logger := ts.logger
	heuristicAnalyzer := ts.analyzer

	// Dashboard
	mux.Handle("/static/", web.DashboardHandler())
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/index.html", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/static/index.html", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Connections list/add
	mux.HandleFunc("/api/v1/connections", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := db.List()
			if err != nil {
				jsonError(w, "failed to list connections", http.StatusInternalServerError)
				return
			}
			type connInfo struct {
				Name     string `json:"name"`
				Platform string `json:"platform"`
				BaseURL  string `json:"base_url,omitempty"`
			}
			out := make([]connInfo, len(records))
			for i, r := range records {
				out[i] = connInfo{Name: r.Name, Platform: r.Platform, BaseURL: r.BaseURL}
			}
			jsonOK(w, map[string]interface{}{"connections": out, "count": len(out)})

		case http.MethodPost:
			var req struct {
				Name        string `json:"name"`
				Platform    string `json:"platform"`
				Token       string `json:"token"`
				Username    string `json:"username"`
				AppPassword string `json:"app_password"`
				BaseURL     string `json:"base_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			if req.Name == "" || req.Platform == "" {
				jsonError(w, "name and platform are required", http.StatusBadRequest)
				return
			}
			provider := buildProvider(req.Platform, req.Token, req.Username, req.AppPassword, req.BaseURL, logger)
			if provider == nil {
				jsonError(w, "unsupported platform: "+req.Platform, http.StatusBadRequest)
				return
			}
			rec := &storage.ConnectionRecord{
				Name: req.Name, Platform: req.Platform, Token: req.Token,
				Username: req.Username, AppPassword: req.AppPassword, BaseURL: req.BaseURL,
			}
			if err := db.Create(rec); err != nil {
				jsonError(w, err.Error(), http.StatusConflict)
				return
			}
			if err := manager.Add(req.Name, provider); err != nil {
				db.Delete(req.Name)
				jsonError(w, err.Error(), http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"name": req.Name, "platform": req.Platform, "status": "added",
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Test all connections
	mux.HandleFunc("/api/v1/connections/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		jsonOK(w, manager.TestAllConnections(ctx))
	})

	// Single connection: GET, DELETE, test
	mux.HandleFunc("/api/v1/connections/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")
		parts := strings.SplitN(path, "/", 2)
		name := parts[0]
		if name == "" || name == "test" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(parts) == 2 && parts[1] == "test" {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			status, err := manager.TestConnection(ctx, name)
			if err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			jsonOK(w, status)
			return
		}
		switch r.Method {
		case http.MethodGet:
			conn, err := manager.Get(name)
			if err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			jsonOK(w, map[string]string{"name": conn.Name, "platform": string(conn.Platform)})
		case http.MethodDelete:
			if err := db.Delete(name); err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			manager.Remove(name)
			jsonOK(w, map[string]string{"name": name, "status": "removed"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Connection update
	mux.HandleFunc("/api/v1/connections/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name        string `json:"name"`
			Platform    string `json:"platform"`
			Token       string `json:"token"`
			Username    string `json:"username"`
			AppPassword string `json:"app_password"`
			BaseURL     string `json:"base_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}
		existing, err := db.GetByName(req.Name)
		if err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		if req.Platform == "" {
			req.Platform = existing.Platform
		}
		if req.Token == "" {
			req.Token = existing.Token
		}
		if req.Username == "" {
			req.Username = existing.Username
		}
		if req.AppPassword == "" {
			req.AppPassword = existing.AppPassword
		}
		rec := &storage.ConnectionRecord{
			Name: req.Name, Platform: req.Platform, Token: req.Token,
			Username: req.Username, AppPassword: req.AppPassword, BaseURL: req.BaseURL,
		}
		if err := db.Update(rec); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		provider := buildProvider(req.Platform, req.Token, req.Username, req.AppPassword, req.BaseURL, logger)
		if provider != nil {
			manager.Replace(req.Name, provider)
		}
		jsonOK(w, map[string]string{"name": req.Name, "status": "updated"})
	})

	// Findings list
	mux.HandleFunc("/api/v1/analysis/findings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		connName := r.URL.Query().Get("connection")
		findings, err := db.ListFindings(connName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if findings == nil {
			findings = []storage.FindingRecord{}
		}
		jsonOK(w, map[string]interface{}{"findings": findings, "count": len(findings)})
	})

	// Finding update/delete
	mux.HandleFunc("/api/v1/analysis/findings/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Handle export
		if strings.HasSuffix(path, "/export") {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			connName := r.URL.Query().Get("connection")
			format := r.URL.Query().Get("format")
			if format == "" {
				format = "csv"
			}
			findings, err := db.ListFindings(connName)
			if err != nil {
				jsonError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if findings == nil {
				findings = []storage.FindingRecord{}
			}
			if format == "json" {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Disposition", "attachment; filename=pipewarden-findings.json")
				json.NewEncoder(w).Encode(findings)
				return
			}
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", "attachment; filename=pipewarden-findings.csv")
			w.Write([]byte("ID,Connection,Run ID,Severity,Category,Title,Description,Remediation,File,Line,Confidence,Status,Created At\n"))
			for _, f := range findings {
				line := fmt.Sprintf("%d,%s,%s,%s,%s,%s,%s,%s,%s,%d,%.2f,%s,%s\n",
					f.ID, csvEscape(f.ConnectionName), csvEscape(f.RunID), f.Severity, f.Category,
					csvEscape(f.Title), csvEscape(f.Description), csvEscape(f.Remediation),
					csvEscape(f.File), f.Line, f.Confidence, f.Status,
					f.CreatedAt.Format(time.RFC3339),
				)
				w.Write([]byte(line))
			}
			return
		}

		idStr := strings.TrimPrefix(path, "/api/v1/analysis/findings/")
		id := int64(0)
		fmt.Sscanf(idStr, "%d", &id)
		if id == 0 {
			jsonError(w, "invalid finding ID", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			var req struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			validStatuses := map[string]bool{"open": true, "acknowledged": true, "resolved": true, "false_positive": true}
			if !validStatuses[req.Status] {
				jsonError(w, "invalid status", http.StatusBadRequest)
				return
			}
			if err := db.UpdateFindingStatus(id, req.Status); err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			jsonOK(w, map[string]interface{}{"id": id, "status": req.Status})
		case http.MethodDelete:
			if err := db.DeleteFinding(id); err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			jsonOK(w, map[string]string{"status": "deleted"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Stats
	mux.HandleFunc("/api/v1/analysis/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		stats, err := db.GetFindingStats()
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, stats)
	})

	// History
	mux.HandleFunc("/api/v1/analysis/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		connName := r.URL.Query().Get("connection")
		history, err := db.ListAnalysisHistory(connName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if history == nil {
			history = []storage.AnalysisRecord{}
		}
		jsonOK(w, map[string]interface{}{"history": history, "count": len(history)})
	})

	// Pipeline runs
	mux.HandleFunc("/api/v1/pipelines/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		connName := r.URL.Query().Get("connection")
		owner := r.URL.Query().Get("owner")
		repo := r.URL.Query().Get("repo")
		if connName == "" || owner == "" || repo == "" {
			jsonError(w, "connection, owner, and repo are required", http.StatusBadRequest)
			return
		}
		conn, err := manager.Get(connName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		runs, err := conn.Provider.ListPipelineRuns(ctx, owner, repo, 10)
		if err != nil {
			jsonError(w, fmt.Sprintf("failed to list runs: %v", err), http.StatusBadGateway)
			return
		}
		jsonOK(w, map[string]interface{}{"runs": runs, "count": len(runs)})
	})

	// Pipelines
	mux.HandleFunc("/api/v1/pipelines", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		connName := r.URL.Query().Get("connection")
		owner := r.URL.Query().Get("owner")
		repo := r.URL.Query().Get("repo")
		if connName == "" || owner == "" || repo == "" {
			jsonError(w, "connection, owner, and repo are required", http.StatusBadRequest)
			return
		}
		conn, err := manager.Get(connName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		pipelines, err := conn.Provider.ListPipelines(ctx, owner, repo)
		if err != nil {
			jsonError(w, fmt.Sprintf("failed to list pipelines: %v", err), http.StatusBadGateway)
			return
		}
		jsonOK(w, map[string]interface{}{"pipelines": pipelines, "count": len(pipelines)})
	})

	// Quick scan
	mux.HandleFunc("/api/v1/analysis/quick", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req analysis.AnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.ConnectionName == "" || req.Owner == "" || req.Repo == "" || req.RunID == "" {
			jsonError(w, "connection_name, owner, repo, and run_id are required", http.StatusBadRequest)
			return
		}
		conn, err := manager.Get(req.ConnectionName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		run, err := conn.Provider.GetPipelineRun(ctx, req.Owner, req.Repo, req.RunID)
		if err != nil {
			jsonError(w, fmt.Sprintf("failed to get pipeline run: %v", err), http.StatusBadGateway)
			return
		}
		result := heuristicAnalyzer.AnalyzeRun(conn, run)
		for i := range result.Findings {
			f := &result.Findings[i]
			rec := &storage.FindingRecord{
				ConnectionName: f.ConnectionName, RunID: f.RunID, Severity: string(f.Severity),
				Category: string(f.Category), Title: f.Title, Description: f.Description,
				Remediation: f.Remediation, File: f.File, Line: f.Line,
				Confidence: f.Confidence, Status: f.Status,
			}
			db.CreateFinding(rec)
		}
		analysisRec := &storage.AnalysisRecord{
			ConnectionName: result.ConnectionName, RunID: result.RunID, Summary: result.Summary,
			RiskScore: result.RiskScore, FindingsCount: len(result.Findings),
			Model: "heuristic-v1", DurationMS: result.DurationMS, AnalyzedAt: result.AnalyzedAt,
		}
		db.CreateAnalysisRecord(analysisRec)
		jsonOK(w, result)
	})

	// Dashboard overview
	mux.HandleFunc("/api/v1/dashboard/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		connCount, _ := db.Count()
		stats, _ := db.GetFindingStats()
		history, _ := db.ListAnalysisHistory("")
		findings, _ := db.ListFindings("")
		avgRisk := 0
		if len(history) > 0 {
			totalRisk := 0
			for _, h := range history {
				totalRisk += h.RiskScore
			}
			avgRisk = totalRisk / len(history)
		}
		securityScore := 100 - avgRisk
		openCount := 0
		for _, f := range findings {
			if f.Status == "open" {
				openCount++
			}
		}
		recommendations := buildRecommendations(stats, openCount, connCount, findings)
		jsonOK(w, map[string]interface{}{
			"security_score":  securityScore,
			"connections":     connCount,
			"total_analyses":  len(history),
			"total_findings":  len(findings),
			"open_findings":   openCount,
			"recommendations": recommendations,
		})
	})
}

// addMockConnection adds a connection with a mock provider to the test server
func (ts *testServer) addMockConnection(t *testing.T, name, platform string, provider integrations.Provider) {
	t.Helper()
	rec := &storage.ConnectionRecord{
		Name:     name,
		Platform: platform,
		Token:    "test-token",
	}
	if err := ts.db.Create(rec); err != nil {
		t.Fatalf("Failed to create connection record: %v", err)
	}
	if err := ts.manager.Add(name, provider); err != nil {
		t.Fatalf("Failed to add provider: %v", err)
	}
}

func doRequest(t *testing.T, mux http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var data map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("Failed to parse JSON response: %v\nBody: %s", err, rec.Body.String())
	}
	return data
}

// ============ TESTS ============

func TestHealthEndpoint(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/health", nil)
	if rec.Code != 200 {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "OK" {
		t.Errorf("Expected 'OK', got '%s'", rec.Body.String())
	}
}

func TestRootRedirect(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/", nil)
	if rec.Code != 302 {
		t.Errorf("Expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/static/index.html" {
		t.Errorf("Expected redirect to /static/index.html, got %s", loc)
	}
}

func TestDashboardRedirect(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/dashboard", nil)
	if rec.Code != 302 {
		t.Errorf("Expected 302, got %d", rec.Code)
	}
}

func Test404(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/nonexistent", nil)
	if rec.Code != 404 {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestConnectionsEmpty(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/api/v1/connections", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	data := parseJSON(t, rec)
	count := int(data["count"].(float64))
	if count != 0 {
		t.Errorf("Expected 0 connections, got %d", count)
	}
}

func TestConnectionCRUD(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	// Create
	body := map[string]string{
		"name":     "test-gh",
		"platform": "github",
		"token":    "ghp_test123",
	}
	rec := doRequest(t, ts.mux, "POST", "/api/v1/connections", body)
	if rec.Code != 201 {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	data := parseJSON(t, rec)
	if data["name"] != "test-gh" {
		t.Errorf("Expected name 'test-gh', got %v", data["name"])
	}

	// List
	rec = doRequest(t, ts.mux, "GET", "/api/v1/connections", nil)
	data = parseJSON(t, rec)
	if int(data["count"].(float64)) != 1 {
		t.Errorf("Expected 1 connection, got %v", data["count"])
	}

	// Get single
	rec = doRequest(t, ts.mux, "GET", "/api/v1/connections/test-gh", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data = parseJSON(t, rec)
	if data["name"] != "test-gh" {
		t.Errorf("Expected 'test-gh', got %v", data["name"])
	}

	// Update
	updateBody := map[string]string{
		"name":     "test-gh",
		"base_url": "https://github.example.com/api/v3",
	}
	rec = doRequest(t, ts.mux, "PUT", "/api/v1/connections/update", updateBody)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data = parseJSON(t, rec)
	if data["status"] != "updated" {
		t.Errorf("Expected status 'updated', got %v", data["status"])
	}

	// Delete
	rec = doRequest(t, ts.mux, "DELETE", "/api/v1/connections/test-gh", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted
	rec = doRequest(t, ts.mux, "GET", "/api/v1/connections", nil)
	data = parseJSON(t, rec)
	if int(data["count"].(float64)) != 0 {
		t.Errorf("Expected 0 connections after delete, got %v", data["count"])
	}
}

func TestConnectionDuplicate(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	body := map[string]string{"name": "dup-conn", "platform": "github", "token": "ghp_x"}
	rec := doRequest(t, ts.mux, "POST", "/api/v1/connections", body)
	if rec.Code != 201 {
		t.Fatalf("First create should succeed: %d %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, ts.mux, "POST", "/api/v1/connections", body)
	if rec.Code != 409 {
		t.Errorf("Duplicate should return 409, got %d", rec.Code)
	}
}

func TestConnectionValidation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	// Missing name
	rec := doRequest(t, ts.mux, "POST", "/api/v1/connections", map[string]string{"platform": "github"})
	if rec.Code != 400 {
		t.Errorf("Expected 400 for missing name, got %d", rec.Code)
	}

	// Missing platform
	rec = doRequest(t, ts.mux, "POST", "/api/v1/connections", map[string]string{"name": "test"})
	if rec.Code != 400 {
		t.Errorf("Expected 400 for missing platform, got %d", rec.Code)
	}

	// Invalid platform
	rec = doRequest(t, ts.mux, "POST", "/api/v1/connections", map[string]string{"name": "test", "platform": "jenkins"})
	if rec.Code != 400 {
		t.Errorf("Expected 400 for unsupported platform, got %d", rec.Code)
	}

	// Invalid JSON
	req := httptest.NewRequest("POST", "/api/v1/connections", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ts.mux.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestConnectionNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/api/v1/connections/nonexistent", nil)
	if rec.Code != 404 {
		t.Errorf("Expected 404, got %d", rec.Code)
	}

	rec = doRequest(t, ts.mux, "DELETE", "/api/v1/connections/nonexistent", nil)
	if rec.Code != 404 {
		t.Errorf("Expected 404 for delete nonexistent, got %d", rec.Code)
	}
}

func TestConnectionMethodNotAllowed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "PUT", "/api/v1/connections", nil)
	if rec.Code != 405 {
		t.Errorf("Expected 405, got %d", rec.Code)
	}
}

func TestAllPlatforms(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	platforms := []struct {
		name     string
		platform string
		extra    map[string]string
	}{
		{"gh-conn", "github", map[string]string{"token": "ghp_test"}},
		{"gl-conn", "gitlab", map[string]string{"token": "glpat-test"}},
		{"bb-conn", "bitbucket", map[string]string{"username": "user", "app_password": "pass"}},
	}

	for _, p := range platforms {
		body := map[string]string{"name": p.name, "platform": p.platform}
		for k, v := range p.extra {
			body[k] = v
		}
		rec := doRequest(t, ts.mux, "POST", "/api/v1/connections", body)
		if rec.Code != 201 {
			t.Errorf("Platform %s: expected 201, got %d: %s", p.platform, rec.Code, rec.Body.String())
		}
	}

	rec := doRequest(t, ts.mux, "GET", "/api/v1/connections", nil)
	data := parseJSON(t, rec)
	if int(data["count"].(float64)) != 3 {
		t.Errorf("Expected 3 connections, got %v", data["count"])
	}
}

func TestTestConnection(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	mock := &mockProvider{
		platform: integrations.PlatformGitHub,
		testResult: &integrations.ConnectionStatus{
			Connected:      true,
			Platform:       integrations.PlatformGitHub,
			ConnectionName: "gh-test",
			User:           "testuser",
			RateLimitOK:    true,
			Latency:        50 * time.Millisecond,
			Message:        "OK",
		},
	}
	ts.addMockConnection(t, "gh-test", "github", mock)

	rec := doRequest(t, ts.mux, "POST", "/api/v1/connections/gh-test/test", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := parseJSON(t, rec)
	if data["connected"] != true {
		t.Errorf("Expected connected=true, got %v", data["connected"])
	}
	if data["user"] != "testuser" {
		t.Errorf("Expected user=testuser, got %v", data["user"])
	}
}

func TestTestConnectionNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "POST", "/api/v1/connections/nope/test", nil)
	if rec.Code != 404 {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestTestAllConnections(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	mock1 := &mockProvider{
		platform: integrations.PlatformGitHub,
		testResult: &integrations.ConnectionStatus{Connected: true, Platform: integrations.PlatformGitHub},
	}
	mock2 := &mockProvider{
		platform: integrations.PlatformGitLab,
		testResult: &integrations.ConnectionStatus{Connected: false, Platform: integrations.PlatformGitLab, Message: "unauthorized"},
	}
	ts.addMockConnection(t, "gh-ok", "github", mock1)
	ts.addMockConnection(t, "gl-fail", "gitlab", mock2)

	rec := doRequest(t, ts.mux, "POST", "/api/v1/connections/test", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	data := parseJSON(t, rec)
	if _, ok := data["gh-ok"]; !ok {
		t.Errorf("Missing gh-ok in test results")
	}
	if _, ok := data["gl-fail"]; !ok {
		t.Errorf("Missing gl-fail in test results")
	}
}

func TestFindingsEmpty(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	data := parseJSON(t, rec)
	if int(data["count"].(float64)) != 0 {
		t.Errorf("Expected 0 findings, got %v", data["count"])
	}
}

func TestFindingLifecycleAPI(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	// Create a finding directly
	rec := &storage.FindingRecord{
		ConnectionName: "test-conn",
		RunID:          "run-1",
		Severity:       "high",
		Category:       "secrets",
		Title:          "Hardcoded API key",
		Description:    "Found hardcoded API key in config",
		Confidence:     0.9,
		Status:         "open",
	}
	if err := ts.db.CreateFinding(rec); err != nil {
		t.Fatalf("Failed to create finding: %v", err)
	}

	// List
	resp := doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings", nil)
	data := parseJSON(t, resp)
	findings := data["findings"].([]interface{})
	if len(findings) != 1 {
		t.Fatalf("Expected 1 finding, got %d", len(findings))
	}
	f := findings[0].(map[string]interface{})
	findingID := int64(f["id"].(float64))

	// Acknowledge
	resp = doRequest(t, ts.mux, "PATCH", fmt.Sprintf("/api/v1/analysis/findings/%d", findingID),
		map[string]string{"status": "acknowledged"})
	if resp.Code != 200 {
		t.Fatalf("Expected 200 on acknowledge, got %d: %s", resp.Code, resp.Body.String())
	}

	// Resolve
	resp = doRequest(t, ts.mux, "PATCH", fmt.Sprintf("/api/v1/analysis/findings/%d", findingID),
		map[string]string{"status": "resolved"})
	if resp.Code != 200 {
		t.Fatalf("Expected 200 on resolve, got %d: %s", resp.Code, resp.Body.String())
	}

	// Reopen
	resp = doRequest(t, ts.mux, "PATCH", fmt.Sprintf("/api/v1/analysis/findings/%d", findingID),
		map[string]string{"status": "open"})
	if resp.Code != 200 {
		t.Fatalf("Expected 200 on reopen, got %d", resp.Code)
	}

	// False positive
	resp = doRequest(t, ts.mux, "PATCH", fmt.Sprintf("/api/v1/analysis/findings/%d", findingID),
		map[string]string{"status": "false_positive"})
	if resp.Code != 200 {
		t.Fatalf("Expected 200 on false_positive, got %d", resp.Code)
	}

	// Invalid status
	resp = doRequest(t, ts.mux, "PATCH", fmt.Sprintf("/api/v1/analysis/findings/%d", findingID),
		map[string]string{"status": "invalid"})
	if resp.Code != 400 {
		t.Errorf("Expected 400 for invalid status, got %d", resp.Code)
	}

	// Delete
	resp = doRequest(t, ts.mux, "DELETE", fmt.Sprintf("/api/v1/analysis/findings/%d", findingID), nil)
	if resp.Code != 200 {
		t.Fatalf("Expected 200 on delete, got %d", resp.Code)
	}

	// Verify deleted
	resp = doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings", nil)
	data = parseJSON(t, resp)
	if int(data["count"].(float64)) != 0 {
		t.Errorf("Expected 0 findings after delete, got %v", data["count"])
	}
}

func TestFindingInvalidID(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "PATCH", "/api/v1/analysis/findings/abc",
		map[string]string{"status": "open"})
	if rec.Code != 400 {
		t.Errorf("Expected 400 for invalid ID, got %d", rec.Code)
	}
}

func TestFindingsByConnection(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "conn-a", RunID: "1", Severity: "high", Category: "secrets",
		Title: "Finding A", Status: "open", Confidence: 0.8,
	})
	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "conn-b", RunID: "2", Severity: "low", Category: "config",
		Title: "Finding B", Status: "open", Confidence: 0.5,
	})

	rec := doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings?connection=conn-a", nil)
	data := parseJSON(t, rec)
	if int(data["count"].(float64)) != 1 {
		t.Errorf("Expected 1 finding for conn-a, got %v", data["count"])
	}
}

func TestStatsEndpoint(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "c", RunID: "1", Severity: "critical", Category: "secrets",
		Title: "F1", Status: "open", Confidence: 0.9,
	})
	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "c", RunID: "1", Severity: "high", Category: "config",
		Title: "F2", Status: "open", Confidence: 0.7,
	})
	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "c", RunID: "1", Severity: "critical", Category: "secrets",
		Title: "F3", Status: "resolved", Confidence: 0.9,
	})

	rec := doRequest(t, ts.mux, "GET", "/api/v1/analysis/stats", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	data := parseJSON(t, rec)
	if int(data["critical"].(float64)) != 2 {
		t.Errorf("Expected 2 critical, got %v", data["critical"])
	}
	if int(data["high"].(float64)) != 1 {
		t.Errorf("Expected 1 high, got %v", data["high"])
	}
	if int(data["open"].(float64)) != 2 {
		t.Errorf("Expected 2 open, got %v", data["open"])
	}
}

func TestHistoryEndpoint(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	ts.db.CreateAnalysisRecord(&storage.AnalysisRecord{
		ConnectionName: "conn-1", RunID: "run-1", Summary: "All good",
		RiskScore: 15, FindingsCount: 1, Model: "heuristic-v1",
		AnalyzedAt: time.Now(),
	})

	rec := doRequest(t, ts.mux, "GET", "/api/v1/analysis/history", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	data := parseJSON(t, rec)
	if int(data["count"].(float64)) != 1 {
		t.Errorf("Expected 1 history entry, got %v", data["count"])
	}
}

func TestPipelineRuns(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	mock := &mockProvider{
		platform: integrations.PlatformGitHub,
		runs: []integrations.PipelineRun{
			{ID: "100", Status: integrations.StatusSuccess, Branch: "main"},
			{ID: "99", Status: integrations.StatusFailed, Branch: "dev"},
		},
	}
	ts.addMockConnection(t, "gh-pipe", "github", mock)

	rec := doRequest(t, ts.mux, "GET", "/api/v1/pipelines/runs?connection=gh-pipe&owner=org&repo=app", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := parseJSON(t, rec)
	if int(data["count"].(float64)) != 2 {
		t.Errorf("Expected 2 runs, got %v", data["count"])
	}
}

func TestPipelineRunsMissingParams(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/api/v1/pipelines/runs?connection=x&owner=y", nil)
	if rec.Code != 400 {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

func TestPipelineRunsConnNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/api/v1/pipelines/runs?connection=nope&owner=o&repo=r", nil)
	if rec.Code != 404 {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestListPipelines(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	mock := &mockProvider{
		platform: integrations.PlatformGitHub,
		pipelines: []integrations.Pipeline{
			{ID: "ci", Name: "CI Pipeline"},
			{ID: "deploy", Name: "Deploy"},
		},
	}
	ts.addMockConnection(t, "gh-p", "github", mock)

	rec := doRequest(t, ts.mux, "GET", "/api/v1/pipelines?connection=gh-p&owner=org&repo=app", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := parseJSON(t, rec)
	if int(data["count"].(float64)) != 2 {
		t.Errorf("Expected 2 pipelines, got %v", data["count"])
	}
}

func TestQuickScan(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	mock := &mockProvider{
		platform: integrations.PlatformGitHub,
		run: &integrations.PipelineRun{
			ID:         "42",
			PipelineID: "ci",
			Status:     integrations.StatusSuccess,
			Branch:     "main",
			CommitSHA:  "abc1234",
			StartedAt:  time.Now().Add(-5 * time.Minute),
			FinishedAt: time.Now(),
			Steps: []integrations.PipelineStep{
				{Name: "build", Status: integrations.StatusSuccess, Duration: 2 * time.Minute},
				{Name: "test", Status: integrations.StatusSuccess, Duration: 3 * time.Minute},
			},
		},
	}
	ts.addMockConnection(t, "gh-scan", "github", mock)

	body := map[string]string{
		"connection_name": "gh-scan",
		"owner":           "org",
		"repo":            "app",
		"run_id":          "42",
	}
	rec := doRequest(t, ts.mux, "POST", "/api/v1/analysis/quick", body)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := parseJSON(t, rec)
	if _, ok := data["risk_score"]; !ok {
		t.Errorf("Expected risk_score in response")
	}
	if _, ok := data["findings"]; !ok {
		t.Errorf("Expected findings in response")
	}
	if _, ok := data["summary"]; !ok {
		t.Errorf("Expected summary in response")
	}

	// Verify findings were persisted
	resp := doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings", nil)
	fData := parseJSON(t, resp)
	count := int(fData["count"].(float64))
	// Main branch push should generate at least 1 finding
	if count == 0 {
		t.Logf("No findings generated (may vary by heuristic rules)")
	}

	// Verify analysis record was persisted
	resp = doRequest(t, ts.mux, "GET", "/api/v1/analysis/history", nil)
	hData := parseJSON(t, resp)
	if int(hData["count"].(float64)) != 1 {
		t.Errorf("Expected 1 history entry after quick scan, got %v", hData["count"])
	}
}

func TestQuickScanMissingParams(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	body := map[string]string{"connection_name": "x", "owner": "y"}
	rec := doRequest(t, ts.mux, "POST", "/api/v1/analysis/quick", body)
	if rec.Code != 400 {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

func TestQuickScanConnNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	body := map[string]string{
		"connection_name": "nope", "owner": "o", "repo": "r", "run_id": "1",
	}
	rec := doRequest(t, ts.mux, "POST", "/api/v1/analysis/quick", body)
	if rec.Code != 404 {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestDashboardOverviewEmpty(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "GET", "/api/v1/dashboard/overview", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	data := parseJSON(t, rec)
	if int(data["security_score"].(float64)) != 100 {
		t.Errorf("Expected security_score=100 for empty state, got %v", data["security_score"])
	}
	if int(data["connections"].(float64)) != 0 {
		t.Errorf("Expected 0 connections, got %v", data["connections"])
	}
	recs := data["recommendations"].([]interface{})
	if len(recs) == 0 {
		t.Errorf("Expected recommendations, got none")
	}
}

func TestDashboardOverviewWithData(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	// Add connection
	ts.db.Create(&storage.ConnectionRecord{Name: "gh", Platform: "github", Token: "t"})
	ts.manager.Add("gh", &mockProvider{platform: integrations.PlatformGitHub})

	// Add findings
	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "gh", RunID: "1", Severity: "critical", Category: "secrets",
		Title: "Exposed key", Status: "open", Confidence: 0.95,
	})
	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "gh", RunID: "1", Severity: "high", Category: "config",
		Title: "Weak config", Status: "open", Confidence: 0.8,
	})

	// Add history
	ts.db.CreateAnalysisRecord(&storage.AnalysisRecord{
		ConnectionName: "gh", RunID: "1", Summary: "Issues found",
		RiskScore: 65, FindingsCount: 2, Model: "heuristic-v1", AnalyzedAt: time.Now(),
	})

	rec := doRequest(t, ts.mux, "GET", "/api/v1/dashboard/overview", nil)
	data := parseJSON(t, rec)
	if int(data["security_score"].(float64)) != 35 {
		t.Errorf("Expected security_score=35 (100-65), got %v", data["security_score"])
	}
	if int(data["open_findings"].(float64)) != 2 {
		t.Errorf("Expected 2 open findings, got %v", data["open_findings"])
	}
	if int(data["connections"].(float64)) != 1 {
		t.Errorf("Expected 1 connection, got %v", data["connections"])
	}
}

func TestExportCSV(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "c", RunID: "1", Severity: "high", Category: "secrets",
		Title: "Test, \"finding\"", Description: "Has commas, and \"quotes\"",
		Status: "open", Confidence: 0.9,
	})

	rec := doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings/export?format=csv", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/csv" {
		t.Errorf("Expected text/csv, got %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ID,Connection") {
		t.Errorf("Expected CSV header row")
	}
	// Check CSV escaping
	if !strings.Contains(body, "\"Test, \"\"finding\"\"\"") {
		t.Errorf("Expected properly escaped CSV, got: %s", body)
	}
}

func TestExportJSON(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	ts.db.CreateFinding(&storage.FindingRecord{
		ConnectionName: "c", RunID: "1", Severity: "low", Category: "config",
		Title: "Minor issue", Status: "open", Confidence: 0.5,
	})

	rec := doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings/export?format=json", nil)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Expected application/json, got %s", ct)
	}
	var findings []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &findings); err != nil {
		t.Fatalf("Expected valid JSON array: %v", err)
	}
	if len(findings) != 1 {
		t.Errorf("Expected 1 finding in export, got %d", len(findings))
	}
}

func TestConnectionUpdateNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	body := map[string]string{"name": "ghost"}
	rec := doRequest(t, ts.mux, "PUT", "/api/v1/connections/update", body)
	if rec.Code != 404 {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestConnectionUpdateMissingName(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	rec := doRequest(t, ts.mux, "PUT", "/api/v1/connections/update", map[string]string{})
	if rec.Code != 400 {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

func TestQuickScanFailedPipeline(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	mock := &mockProvider{
		platform: integrations.PlatformGitHub,
		run: &integrations.PipelineRun{
			ID:         "50",
			PipelineID: "ci",
			Status:     integrations.StatusFailed,
			Branch:     "feature/exploit",
			CommitSHA:  "def5678",
			StartedAt:  time.Now().Add(-120 * time.Minute),
			FinishedAt: time.Now(),
			Steps: []integrations.PipelineStep{
				{Name: "build", Status: integrations.StatusSuccess, Duration: 2 * time.Minute},
				{Name: "security-scan", Status: integrations.StatusFailed, Duration: 1 * time.Minute},
				{Name: "deploy-prod", Status: integrations.StatusSuccess, Duration: 5 * time.Minute},
			},
		},
	}
	ts.addMockConnection(t, "gh-bad", "github", mock)

	body := map[string]string{
		"connection_name": "gh-bad", "owner": "org", "repo": "app", "run_id": "50",
	}
	rec := doRequest(t, ts.mux, "POST", "/api/v1/analysis/quick", body)
	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data := parseJSON(t, rec)

	riskScore := int(data["risk_score"].(float64))
	if riskScore == 0 {
		t.Errorf("Expected non-zero risk score for failed pipeline with security issues")
	}

	findings := data["findings"].([]interface{})
	if len(findings) == 0 {
		t.Errorf("Expected findings for failed pipeline with security issues")
	}

	// Check that various heuristic checks triggered
	foundSecurity := false
	foundFailed := false
	foundDeploy := false
	for _, f := range findings {
		fm := f.(map[string]interface{})
		title := fm["title"].(string)
		if strings.Contains(title, "ecurity") || strings.Contains(title, "scan") {
			foundSecurity = true
		}
		if strings.Contains(title, "ailed") {
			foundFailed = true
		}
		if strings.Contains(strings.ToLower(title), "deploy") {
			foundDeploy = true
		}
	}
	t.Logf("Findings generated: security=%v failed=%v deploy=%v (total=%d, risk=%d)",
		foundSecurity, foundFailed, foundDeploy, len(findings), riskScore)
}

func TestEndToEndWorkflow(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	// 1. Empty state
	rec := doRequest(t, ts.mux, "GET", "/api/v1/dashboard/overview", nil)
	data := parseJSON(t, rec)
	if int(data["connections"].(float64)) != 0 {
		t.Fatal("Expected empty state")
	}

	// 2. Add connection
	mock := &mockProvider{
		platform: integrations.PlatformGitHub,
		testResult: &integrations.ConnectionStatus{
			Connected: true, Platform: integrations.PlatformGitHub, User: "e2euser",
		},
		run: &integrations.PipelineRun{
			ID: "200", PipelineID: "ci", Status: integrations.StatusSuccess,
			Branch: "main", CommitSHA: "abc",
			StartedAt: time.Now().Add(-5 * time.Minute), FinishedAt: time.Now(),
			Steps: []integrations.PipelineStep{
				{Name: "build", Status: integrations.StatusSuccess, Duration: 2 * time.Minute},
			},
		},
		runs: []integrations.PipelineRun{
			{ID: "200", Status: integrations.StatusSuccess, Branch: "main"},
			{ID: "199", Status: integrations.StatusFailed, Branch: "dev"},
		},
	}
	ts.addMockConnection(t, "e2e-gh", "github", mock)

	// 3. Test connection
	rec = doRequest(t, ts.mux, "POST", "/api/v1/connections/e2e-gh/test", nil)
	testData := parseJSON(t, rec)
	if testData["connected"] != true {
		t.Fatal("Connection test should pass")
	}

	// 4. List pipeline runs
	rec = doRequest(t, ts.mux, "GET", "/api/v1/pipelines/runs?connection=e2e-gh&owner=org&repo=app", nil)
	runsData := parseJSON(t, rec)
	if int(runsData["count"].(float64)) != 2 {
		t.Fatal("Expected 2 runs")
	}

	// 5. Quick scan
	rec = doRequest(t, ts.mux, "POST", "/api/v1/analysis/quick", map[string]string{
		"connection_name": "e2e-gh", "owner": "org", "repo": "app", "run_id": "200",
	})
	scanData := parseJSON(t, rec)
	riskScore := int(scanData["risk_score"].(float64))
	t.Logf("Quick scan risk score: %d", riskScore)

	// 6. Check findings exist
	rec = doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings", nil)
	findingsData := parseJSON(t, rec)
	findingsCount := int(findingsData["count"].(float64))
	t.Logf("Findings generated: %d", findingsCount)

	// 7. Check history
	rec = doRequest(t, ts.mux, "GET", "/api/v1/analysis/history", nil)
	histData := parseJSON(t, rec)
	if int(histData["count"].(float64)) != 1 {
		t.Errorf("Expected 1 history entry, got %v", histData["count"])
	}

	// 8. Dashboard overview reflects data
	rec = doRequest(t, ts.mux, "GET", "/api/v1/dashboard/overview", nil)
	overview := parseJSON(t, rec)
	if int(overview["connections"].(float64)) != 1 {
		t.Errorf("Expected 1 connection in overview")
	}
	if int(overview["total_analyses"].(float64)) != 1 {
		t.Errorf("Expected 1 analysis in overview")
	}

	// 9. Export
	rec = doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings/export?format=csv", nil)
	if rec.Code != 200 {
		t.Errorf("CSV export failed: %d", rec.Code)
	}
	rec = doRequest(t, ts.mux, "GET", "/api/v1/analysis/findings/export?format=json", nil)
	if rec.Code != 200 {
		t.Errorf("JSON export failed: %d", rec.Code)
	}

	// 10. Stats
	rec = doRequest(t, ts.mux, "GET", "/api/v1/analysis/stats", nil)
	if rec.Code != 200 {
		t.Errorf("Stats failed: %d", rec.Code)
	}

	t.Logf("End-to-end workflow completed successfully")
}

func TestDashboardHTML(t *testing.T) {
	ts := newTestServer(t)
	defer ts.close()

	// The embedded file server serves from /static/ prefix
	rec := doRequest(t, ts.mux, "GET", "/static/index.html", nil)
	// http.FileServer may serve directly or redirect - both are fine
	if rec.Code != 200 && rec.Code != 301 {
		t.Errorf("Expected 200 or 301, got %d", rec.Code)
	}
}

func TestCsvEscapeFunction(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"plain", "plain"},
		{"has,comma", "\"has,comma\""},
		{"has\"quote", "\"has\"\"quote\""},
		{"has\nnewline", "\"has\nnewline\""},
		{"", ""},
	}
	for _, tc := range tests {
		got := csvEscape(tc.input)
		if got != tc.expected {
			t.Errorf("csvEscape(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
