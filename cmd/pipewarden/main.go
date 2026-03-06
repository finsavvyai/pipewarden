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
	// Parse command line flags
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Setup logger
	logger, err := logging.New(&cfg.Logging)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Infow("Starting PipeWarden",
		"environment", cfg.Environment,
		"port", cfg.Server.Port,
	)

	// Setup integration manager
	manager := integrations.NewManager(logger)

	if cfg.Integrations.GitHub.Enabled {
		ghClient := github.NewClient(github.Config{
			Token:   cfg.Integrations.GitHub.Token,
			BaseURL: cfg.Integrations.GitHub.BaseURL,
		}, logger)
		manager.Register(ghClient)
		logger.Info("GitHub Actions integration enabled")
	}

	if cfg.Integrations.Bitbucket.Enabled {
		bbClient := bitbucket.NewClient(bitbucket.Config{
			Username:    cfg.Integrations.Bitbucket.Username,
			AppPassword: cfg.Integrations.Bitbucket.AppPassword,
			BaseURL:     cfg.Integrations.Bitbucket.BaseURL,
		}, logger)
		manager.Register(bbClient)
		logger.Info("Bitbucket Pipelines integration enabled")
	}

	if cfg.Integrations.GitLab.Enabled {
		glClient := gitlab.NewClient(gitlab.Config{
			Token:   cfg.Integrations.GitLab.Token,
			BaseURL: cfg.Integrations.GitLab.BaseURL,
		}, logger)
		manager.Register(glClient)
		logger.Info("GitLab CI/CD integration enabled")
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to PipeWarden!"))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/api/v1/integrations/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		results := manager.TestAllConnections(ctx)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	mux.HandleFunc("/api/v1/integrations/platforms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		platforms := manager.Platforms()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"platforms": platforms,
		})
	})

	// Create server with timeouts
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalw("Failed to start server", "error", err)
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Shutdown server gracefully
	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Errorw("Server shutdown failed", "error", err)
	}
	logger.Info("Server gracefully stopped")
}
