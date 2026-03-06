package integrations

import (
	"context"
	"fmt"
	"sync"

	"github.com/finsavvyai/pipewarden/internal/logging"
)

// Manager orchestrates multiple CI/CD platform providers.
type Manager struct {
	providers map[Platform]Provider
	logger    *logging.Logger
	mu        sync.RWMutex
}

// NewManager creates a new integration manager.
func NewManager(logger *logging.Logger) *Manager {
	return &Manager{
		providers: make(map[Platform]Provider),
		logger:    logger,
	}
}

// Register adds a provider to the manager.
func (m *Manager) Register(provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[provider.Name()] = provider
	m.logger.Infow("Registered integration provider", "platform", provider.Name())
}

// Get returns a provider by platform name.
func (m *Manager) Get(platform Platform) (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	provider, ok := m.providers[platform]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", platform)
	}
	return provider, nil
}

// Platforms returns the list of registered platform names.
func (m *Manager) Platforms() []Platform {
	m.mu.RLock()
	defer m.mu.RUnlock()
	platforms := make([]Platform, 0, len(m.providers))
	for p := range m.providers {
		platforms = append(platforms, p)
	}
	return platforms
}

// TestAllConnections tests connectivity for every registered provider concurrently.
func (m *Manager) TestAllConnections(ctx context.Context) map[Platform]*ConnectionStatus {
	m.mu.RLock()
	providers := make(map[Platform]Provider, len(m.providers))
	for k, v := range m.providers {
		providers[k] = v
	}
	m.mu.RUnlock()

	results := make(map[Platform]*ConnectionStatus, len(providers))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for platform, provider := range providers {
		wg.Add(1)
		go func(p Platform, prov Provider) {
			defer wg.Done()
			status, err := prov.TestConnection(ctx)
			if err != nil {
				m.logger.Errorw("Connection test failed", "platform", p, "error", err)
				status = &ConnectionStatus{
					Connected: false,
					Platform:  p,
					Message:   err.Error(),
				}
			}
			mu.Lock()
			results[p] = status
			mu.Unlock()
		}(platform, provider)
	}

	wg.Wait()
	return results
}
