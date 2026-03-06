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
)

func main() {
	configPath := flag.String("config", "", "path to config file")
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

	logger.Infow("Starting PipeWarden",
		"environment", cfg.Environment,
		"port", cfg.Server.Port,
	)

	// Setup integration manager and load connections from config
	manager := integrations.NewManager(logger)
	for _, conn := range cfg.Connections {
		provider := buildProvider(conn, logger)
		if provider == nil {
			logger.Errorw("Unknown platform in config, skipping", "name", conn.Name, "platform", conn.Platform)
			continue
		}
		if err := manager.Add(conn.Name, provider); err != nil {
			logger.Errorw("Failed to add connection", "name", conn.Name, "error", err)
		}
	}
	logger.Infow("Connections loaded from config", "count", manager.Count())

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to PipeWarden!"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// List all connections
	mux.HandleFunc("/api/v1/connections", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			conns := manager.List()
			type connInfo struct {
				Name     string              `json:"name"`
				Platform integrations.Platform `json:"platform"`
			}
			out := make([]connInfo, len(conns))
			for i, c := range conns {
				out[i] = connInfo{Name: c.Name, Platform: c.Platform}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"connections": out,
				"count":       len(out),
			})

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
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
			if req.Name == "" || req.Platform == "" {
				http.Error(w, `{"error":"name and platform are required"}`, http.StatusBadRequest)
				return
			}

			provider := buildProvider(config.ConnectionConfig{
				Name:        req.Name,
				Platform:    req.Platform,
				Token:       req.Token,
				Username:    req.Username,
				AppPassword: req.AppPassword,
				BaseURL:     req.BaseURL,
			}, logger)
			if provider == nil {
				http.Error(w, `{"error":"unsupported platform"}`, http.StatusBadRequest)
				return
			}

			if err := manager.Add(req.Name, provider); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusConflict)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"name":     req.Name,
				"platform": req.Platform,
				"status":   "added",
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Single connection operations: GET (info), DELETE (remove), POST /test (test)
	mux.HandleFunc("/api/v1/connections/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")
		parts := strings.SplitN(path, "/", 2)
		name := parts[0]

		if name == "" {
			http.Error(w, `{"error":"connection name required"}`, http.StatusBadRequest)
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
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
			return
		}

		// /api/v1/connections/{name}
		switch r.Method {
		case http.MethodGet:
			conn, err := manager.Get(name)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"name":     conn.Name,
				"platform": string(conn.Platform),
			})

		case http.MethodDelete:
			if err := manager.Remove(name); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"name":   name,
				"status": "removed",
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Test all connections at once
	mux.HandleFunc("/api/v1/connections/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		results := manager.TestAllConnections(ctx)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
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

// buildProvider creates a Provider from a ConnectionConfig.
func buildProvider(conn config.ConnectionConfig, logger *logging.Logger) integrations.Provider {
	switch integrations.Platform(conn.Platform) {
	case integrations.PlatformGitHub:
		return github.NewClient(github.Config{
			Token:   conn.Token,
			BaseURL: conn.BaseURL,
		}, logger)
	case integrations.PlatformBitbucket:
		return bitbucket.NewClient(bitbucket.Config{
			Username:    conn.Username,
			AppPassword: conn.AppPassword,
			BaseURL:     conn.BaseURL,
		}, logger)
	case integrations.PlatformGitLab:
		return gitlab.NewClient(gitlab.Config{
			Token:   conn.Token,
			BaseURL: conn.BaseURL,
		}, logger)
	default:
		return nil
	}
}
