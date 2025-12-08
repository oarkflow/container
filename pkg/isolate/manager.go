package isolate

import (
	"context"
	"fmt"
	"sync"

	runtimectl "github.com/oarkflow/container/pkg/isolate/runtime"
)

// Manager coordinates container lifecycles backed by a single runtime
// implementation.
type Manager struct {
	runtime    runtimectl.Runtime
	containers map[string]*containerImpl
	mu         sync.RWMutex
}

// NewManager wires a runtime implementation into a container manager.
func NewManager(rt runtimectl.Runtime) (*Manager, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime is required")
	}
	if !rt.Available() {
		return nil, ErrRuntimeUnavailable
	}
	return &Manager{
		runtime:    rt,
		containers: make(map[string]*containerImpl),
	}, nil
}

// NewDefaultManager selects the highest-priority runtime available on the host.
func NewDefaultManager() (*Manager, error) {
	rt, err := runtimectl.DefaultForHost()
	if err != nil {
		return nil, err
	}
	return NewManager(rt)
}

// CreateContainer allocates a VM according to the provided config.
func (m *Manager) CreateContainer(ctx context.Context, cfg *Config) (Container, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.containers[cfg.Name]; exists {
		return nil, ErrContainerExists
	}

	c := newContainer(m.runtime, cfg)
	if err := c.Create(ctx, cfg); err != nil {
		return nil, err
	}

	m.containers[cfg.Name] = c
	return c, nil
}

// GetContainer fetches an existing container by name.
func (m *Manager) GetContainer(name string) (Container, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.containers[name]
	if !ok {
		return nil, false
	}
	return c, true
}

// DeleteContainer removes a container and associated VM resources.
func (m *Manager) DeleteContainer(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.containers[name]
	if !ok {
		return ErrContainerNotFound
	}

	if err := c.Delete(ctx); err != nil {
		return err
	}

	delete(m.containers, name)
	return nil
}

// ListStatuses returns current status from each managed container.
func (m *Manager) ListStatuses(ctx context.Context) ([]*Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]*Status, 0, len(m.containers))
	for _, c := range m.containers {
		status, err := c.Status(ctx)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}
