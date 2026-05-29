package discovery

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sdiscovery "k8s.io/client-go/discovery"
)

type Manager struct {
	client    k8sdiscovery.DiscoveryInterface
	resources []schema.GroupVersionResource

	mu        sync.RWMutex
	present   map[schema.GroupVersionResource]bool
	observers []func(context.Context)
}

func NewManager(client k8sdiscovery.DiscoveryInterface, resources []schema.GroupVersionResource) *Manager {
	copied := append([]schema.GroupVersionResource(nil), resources...)
	return &Manager{
		client:    client,
		resources: copied,
		present:   map[schema.GroupVersionResource]bool{},
	}
}

func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	_ = m.Refresh(ctx)
	m.notifyObservers(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Refresh(ctx)
			m.notifyObservers(ctx)
		}
	}
}

func (m *Manager) AddObserver(observer func(context.Context)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observers = append(m.observers, observer)
}

func (m *Manager) HasCRD(gvr schema.GroupVersionResource) (bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	present, known := m.present[gvr]
	return present, known
}

func (m *Manager) Refresh(ctx context.Context) error {
	next := map[schema.GroupVersionResource]bool{}
	var firstErr error
	for _, gvr := range m.resources {
		if err := ctx.Err(); err != nil {
			firstErr = err
			break
		}
		found, err := m.hasResource(gvr)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			next[gvr] = false
			continue
		}
		next[gvr] = found
	}

	m.mu.Lock()
	m.present = next
	m.mu.Unlock()
	return firstErr
}

func (m *Manager) notifyObservers(ctx context.Context) {
	m.mu.RLock()
	observers := append([]func(context.Context){}, m.observers...)
	m.mu.RUnlock()
	for _, observer := range observers {
		observer(ctx)
	}
}

func (m *Manager) hasResource(gvr schema.GroupVersionResource) (bool, error) {
	list, err := m.client.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	if err != nil {
		return false, err
	}
	for _, resource := range list.APIResources {
		if resource.Name == gvr.Resource {
			return true, nil
		}
	}
	return false, nil
}
