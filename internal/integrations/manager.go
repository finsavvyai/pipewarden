package integrations

import (
	"context"
	"fmt"
	"sync"

	"github.com/finsavvyai/pipewarden/internal/logging"
)

// Manager orchestrates multiple named CI/CD platform connections.
// Users can add many connections — multiple GitHub orgs, multiple GitLab instances, etc.
type Manager struct {
	connections map[string]*Connection
	logger      *logging.Logger
	mu          sync.RWMutex
}

// NewManager creates a new integration manager.
func NewManager(logger *logging.Logger) *Manager {
	return &Manager{
		connections: make(map[string]*Connection),
		logger:      logger,
	}
}

// Add registers a named connection. Returns an error if the name is already taken.
func (m *Manager) Add(name string, provider Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.connections[name]; exists {
		return fmt.Errorf("connection %q already exists", name)
	}

	m.connections[name] = &Connection{
		Name:     name,
		Platform: provider.Name(),
		Provider: provider,
	}
	m.logger.Infow("Added connection", "name", name, "platform", provider.Name())
	return nil
}

// Replace updates an existing connection or creates it if it doesn't exist.
func (m *Manager) Replace(name string, provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connections[name] = &Connection{
		Name:     name,
		Platform: provider.Name(),
		Provider: provider,
	}
	m.logger.Infow("Replaced connection", "name", name, "platform", provider.Name())
}

// Remove deletes a connection by name. Returns an error if not found.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.connections[name]; !exists {
		return fmt.Errorf("connection %q not found", name)
	}

	delete(m.connections, name)
	m.logger.Infow("Removed connection", "name", name)
	return nil
}

// Get returns a connection by name.
func (m *Manager) Get(name string) (*Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connections[name]
	if !ok {
		return nil, fmt.Errorf("connection %q not found", name)
	}
	return conn, nil
}

// GetByPlatform returns all connections for a given platform type.
func (m *Manager) GetByPlatform(platform Platform) []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Connection
	for _, conn := range m.connections {
		if conn.Platform == platform {
			result = append(result, conn)
		}
	}
	return result
}

// List returns all registered connections.
func (m *Manager) List() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		result = append(result, conn)
	}
	return result
}

// Count returns the total number of connections.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}

// TestConnection tests a single named connection.
func (m *Manager) TestConnection(ctx context.Context, name string) (*ConnectionStatus, error) {
	m.mu.RLock()
	conn, ok := m.connections[name]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("connection %q not found", name)
	}

	status, err := conn.Provider.TestConnection(ctx)
	if err != nil {
		return &ConnectionStatus{
			Connected:      false,
			Platform:       conn.Platform,
			ConnectionName: name,
			Message:        err.Error(),
		}, nil
	}

	status.ConnectionName = name
	return status, nil
}

// TestAllConnections tests connectivity for every registered connection concurrently.
func (m *Manager) TestAllConnections(ctx context.Context) map[string]*ConnectionStatus {
	m.mu.RLock()
	snapshot := make(map[string]*Connection, len(m.connections))
	for k, v := range m.connections {
		snapshot[k] = v
	}
	m.mu.RUnlock()

	results := make(map[string]*ConnectionStatus, len(snapshot))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, conn := range snapshot {
		wg.Add(1)
		go func(n string, c *Connection) {
			defer wg.Done()
			status, err := c.Provider.TestConnection(ctx)
			if err != nil {
				m.logger.Errorw("Connection test failed", "name", n, "platform", c.Platform, "error", err)
				status = &ConnectionStatus{
					Connected:      false,
					Platform:       c.Platform,
					ConnectionName: n,
					Message:        err.Error(),
				}
			} else {
				status.ConnectionName = n
			}
			mu.Lock()
			results[n] = status
			mu.Unlock()
		}(name, conn)
	}

	wg.Wait()
	return results
}

// TestByPlatform tests all connections for a given platform type concurrently.
func (m *Manager) TestByPlatform(ctx context.Context, platform Platform) map[string]*ConnectionStatus {
	conns := m.GetByPlatform(platform)

	results := make(map[string]*ConnectionStatus, len(conns))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, conn := range conns {
		wg.Add(1)
		go func(c *Connection) {
			defer wg.Done()
			status, err := c.Provider.TestConnection(ctx)
			if err != nil {
				status = &ConnectionStatus{
					Connected:      false,
					Platform:       c.Platform,
					ConnectionName: c.Name,
					Message:        err.Error(),
				}
			} else {
				status.ConnectionName = c.Name
			}
			mu.Lock()
			results[c.Name] = status
			mu.Unlock()
		}(conn)
	}

	wg.Wait()
	return results
}
