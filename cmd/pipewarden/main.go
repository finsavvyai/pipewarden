package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/finsavvyai/pipewarden/internal/analysis"
	"github.com/finsavvyai/pipewarden/internal/config"
	"github.com/finsavvyai/pipewarden/internal/integrations"
	"github.com/finsavvyai/pipewarden/internal/integrations/bitbucket"
	"github.com/finsavvyai/pipewarden/internal/integrations/github"
	"github.com/finsavvyai/pipewarden/internal/integrations/gitlab"
	"github.com/finsavvyai/pipewarden/internal/logging"
	"github.com/finsavvyai/pipewarden/internal/storage"
	"github.com/finsavvyai/pipewarden/internal/web"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	dbPath := flag.String("db", "pipewarden.db", "path to SQLite database")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	logger, err := logging.New(&cfg.Logging)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Open database
	db, err := storage.New(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	logger.Infow("Starting PipeWarden",
		"environment", cfg.Environment,
		"port", cfg.Server.Port,
		"database", *dbPath,
	)

	// Setup integration manager and load connections from DB
	manager := integrations.NewManager(logger)
	loadConnectionsFromDB(db, manager, logger)

	// Setup Claude security analyzer
	analyzer := analysis.NewClaudeAnalyzer(analysis.ClaudeConfig{
		APIKey:  cfg.Anthropic.APIKey,
		Model:   cfg.Anthropic.Model,
		BaseURL: cfg.Anthropic.BaseURL,
	}, logger)
	if analyzer.Enabled() {
		logger.Infow("Claude security analysis enabled", "model", cfg.Anthropic.Model)
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// Serve dashboard UI
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

	// List all connections / Add connection
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

			// Persist to DB
			rec := &storage.ConnectionRecord{
				Name:        req.Name,
				Platform:    req.Platform,
				Token:       req.Token,
				Username:    req.Username,
				AppPassword: req.AppPassword,
				BaseURL:     req.BaseURL,
			}
			if err := db.Create(rec); err != nil {
				jsonError(w, err.Error(), http.StatusConflict)
				return
			}

			// Register in memory
			if err := manager.Add(req.Name, provider); err != nil {
				// DB succeeded but manager failed (shouldn't happen), rollback
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

	// Test all connections at once — must be registered BEFORE the /{name} catch-all
	mux.HandleFunc("/api/v1/connections/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		jsonOK(w, manager.TestAllConnections(ctx))
	})

	// Single connection: GET, DELETE, POST test
	mux.HandleFunc("/api/v1/connections/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")
		parts := strings.SplitN(path, "/", 2)
		name := parts[0]
		if name == "" || name == "test" {
			// Already handled by the /test route above; avoid conflict
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// /api/v1/connections/{name}/test
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
			// Remove from DB
			if err := db.Delete(name); err != nil {
				jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
			// Remove from memory
			manager.Remove(name)
			jsonOK(w, map[string]string{"name": name, "status": "removed"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Analysis: run on-demand analysis for a pipeline run
	mux.HandleFunc("/api/v1/analysis/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !analyzer.Enabled() {
			jsonError(w, "Claude API key not configured. Set PIPEWARDEN_ANTHROPIC_APIKEY", http.StatusServiceUnavailable)
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

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		run, err := conn.Provider.GetPipelineRun(ctx, req.Owner, req.Repo, req.RunID)
		if err != nil {
			jsonError(w, fmt.Sprintf("failed to get pipeline run: %v", err), http.StatusBadGateway)
			return
		}

		result, err := analyzer.AnalyzeRun(ctx, conn, run)
		if err != nil {
			jsonError(w, fmt.Sprintf("analysis failed: %v", err), http.StatusInternalServerError)
			return
		}

		// Persist findings
		for i := range result.Findings {
			f := &result.Findings[i]
			rec := &storage.FindingRecord{
				ConnectionName: f.ConnectionName,
				RunID:          f.RunID,
				Severity:       string(f.Severity),
				Category:       string(f.Category),
				Title:          f.Title,
				Description:    f.Description,
				Remediation:    f.Remediation,
				File:           f.File,
				Line:           f.Line,
				Confidence:     f.Confidence,
				Status:         f.Status,
			}
			if err := db.CreateFinding(rec); err != nil {
				logger.Errorw("Failed to persist finding", "error", err)
			}
		}

		// Persist analysis record
		analysisRec := &storage.AnalysisRecord{
			ConnectionName: result.ConnectionName,
			RunID:          result.RunID,
			Summary:        result.Summary,
			RiskScore:      result.RiskScore,
			FindingsCount:  len(result.Findings),
			TokensUsed:     result.TokensUsed,
			Model:          result.Model,
			DurationMS:     result.DurationMS,
			AnalyzedAt:     result.AnalyzedAt,
		}
		if err := db.CreateAnalysisRecord(analysisRec); err != nil {
			logger.Errorw("Failed to persist analysis record", "error", err)
		}

		jsonOK(w, result)
	})

	// Analysis: list findings
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

	// Analysis: list history
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

	// Update finding status (PATCH /api/v1/analysis/findings/{id})
	mux.HandleFunc("/api/v1/analysis/findings/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/analysis/findings/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
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
				jsonError(w, "invalid status: must be open, acknowledged, resolved, or false_positive", http.StatusBadRequest)
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

	// Finding stats
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

	// Pipeline browser: list runs for a connection/repo
	mux.HandleFunc("/api/v1/pipelines/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		connName := r.URL.Query().Get("connection")
		owner := r.URL.Query().Get("owner")
		repo := r.URL.Query().Get("repo")
		limitStr := r.URL.Query().Get("limit")
		if connName == "" || owner == "" || repo == "" {
			jsonError(w, "connection, owner, and repo are required", http.StatusBadRequest)
			return
		}
		limit := 10
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
				limit = l
			}
		}
		conn, err := manager.Get(connName)
		if err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		runs, err := conn.Provider.ListPipelineRuns(ctx, owner, repo, limit)
		if err != nil {
			jsonError(w, fmt.Sprintf("failed to list runs: %v", err), http.StatusBadGateway)
			return
		}
		jsonOK(w, map[string]interface{}{"runs": runs, "count": len(runs)})
	})

	// Pipeline browser: list pipelines/workflows
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

	// Connection update (PUT)
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
		// Load current record to fill in unchanged fields
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
		// Allow clearing base_url by sending empty, but if not sent use existing
		rec := &storage.ConnectionRecord{
			Name:        req.Name,
			Platform:    req.Platform,
			Token:       req.Token,
			Username:    req.Username,
			AppPassword: req.AppPassword,
			BaseURL:     req.BaseURL,
		}
		if err := db.Update(rec); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Rebuild provider in memory
		provider := buildProvider(req.Platform, req.Token, req.Username, req.AppPassword, req.BaseURL, logger)
		if provider != nil {
			manager.Replace(req.Name, provider)
		}
		jsonOK(w, map[string]string{"name": req.Name, "status": "updated"})
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		logger.Infow("Dashboard available at", "url", fmt.Sprintf("http://localhost:%d", cfg.Server.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalw("Failed to start server", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Errorw("Server shutdown failed", "error", err)
	}
	logger.Info("Server gracefully stopped")
}

func loadConnectionsFromDB(db *storage.DB, manager *integrations.Manager, logger *logging.Logger) {
	records, err := db.List()
	if err != nil {
		logger.Errorw("Failed to load connections from DB", "error", err)
		return
	}
	for _, rec := range records {
		provider := buildProvider(rec.Platform, rec.Token, rec.Username, rec.AppPassword, rec.BaseURL, logger)
		if provider == nil {
			logger.Errorw("Unknown platform in DB, skipping", "name", rec.Name, "platform", rec.Platform)
			continue
		}
		if err := manager.Add(rec.Name, provider); err != nil {
			logger.Errorw("Failed to load connection", "name", rec.Name, "error", err)
		}
	}
	logger.Infow("Connections loaded from database", "count", len(records))
}

func buildProvider(platform, token, username, appPassword, baseURL string, logger *logging.Logger) integrations.Provider {
	switch integrations.Platform(platform) {
	case integrations.PlatformGitHub:
		return github.NewClient(github.Config{Token: token, BaseURL: baseURL}, logger)
	case integrations.PlatformBitbucket:
		return bitbucket.NewClient(bitbucket.Config{Username: username, AppPassword: appPassword, BaseURL: baseURL}, logger)
	case integrations.PlatformGitLab:
		return gitlab.NewClient(gitlab.Config{Token: token, BaseURL: baseURL}, logger)
	default:
		return nil
	}
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
