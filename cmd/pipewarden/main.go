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
	"strings"
	"syscall"
	"time"

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
