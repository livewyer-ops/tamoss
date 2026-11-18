package consoleapi

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type RuntimeUpdate struct {
	ID       uint64
	Snapshot RuntimeSnapshot
}

// Monitor maintains one bounded latest-state projection for every HTTP and SSE
// client. Slow SSE clients drop intermediate snapshots instead of building an
// unbounded queue; the next delivered event always contains complete state.
type Monitor struct {
	source SnapshotSource

	mu          sync.RWMutex
	current     *RuntimeSnapshot
	currentHash string
	ready       bool
	sequence    uint64
	nextID      uint64
	subscribers map[uint64]chan RuntimeUpdate
}

func NewMonitor(source SnapshotSource) *Monitor {
	return &Monitor{
		source:      source,
		subscribers: map[uint64]chan RuntimeUpdate{},
	}
}

func (m *Monitor) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	refresh := func() {
		if err := m.Refresh(ctx); err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
	}
	refresh()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func (m *Monitor) Refresh(ctx context.Context) error {
	snapshot, err := m.source.Snapshot(ctx)
	if err != nil {
		m.markStale()
		return err
	}
	snapshot.Stale = false
	hash := snapshotContentHash(snapshot)

	m.mu.Lock()
	changed := m.current == nil || m.currentHash != hash || m.current.Stale
	current := copySnapshot(snapshot)
	m.current = &current
	m.currentHash = hash
	m.ready = true
	if changed {
		m.sequence++
		m.publishLocked(RuntimeUpdate{ID: m.sequence, Snapshot: snapshot})
	}
	m.mu.Unlock()
	return nil
}

func (m *Monitor) markStale() {
	m.mu.Lock()
	m.ready = false
	if m.current != nil && !m.current.Stale {
		stale := copySnapshot(*m.current)
		stale.Stale = true
		m.current = &stale
		m.sequence++
		m.publishLocked(RuntimeUpdate{ID: m.sequence, Snapshot: stale})
	}
	m.mu.Unlock()
}

func (m *Monitor) Current() (RuntimeSnapshot, bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return RuntimeSnapshot{}, m.ready, false
	}
	return copySnapshot(*m.current), m.ready, true
}

func (m *Monitor) Subscribe() (<-chan RuntimeUpdate, func()) {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	updates := make(chan RuntimeUpdate, 1)
	m.subscribers[id] = updates
	if m.current != nil {
		updates <- RuntimeUpdate{ID: m.sequence, Snapshot: copySnapshot(*m.current)}
	}
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		delete(m.subscribers, id)
		m.mu.Unlock()
	}
	return updates, cancel
}

func (m *Monitor) publishLocked(update RuntimeUpdate) {
	for _, updates := range m.subscribers {
		select {
		case updates <- update:
		default:
			select {
			case <-updates:
			default:
			}
			updates <- update
		}
	}
}

func snapshotContentHash(snapshot RuntimeSnapshot) string {
	snapshot.ObservedAt = ""
	encoded, _ := json.Marshal(snapshot)
	return string(encoded)
}

func copySnapshot(snapshot RuntimeSnapshot) RuntimeSnapshot {
	copy := snapshot
	copy.Instance.Conditions = append([]InstanceCondition(nil), snapshot.Instance.Conditions...)
	copy.Workloads = append([]Workload(nil), snapshot.Workloads...)
	for i := range copy.Workloads {
		copy.Workloads[i].Conditions = append([]ResourceCondition(nil), snapshot.Workloads[i].Conditions...)
	}
	copy.Services = append([]Service(nil), snapshot.Services...)
	for i := range copy.Services {
		copy.Services[i].Ports = append([]ServicePort(nil), snapshot.Services[i].Ports...)
	}
	copy.EndpointSlices = append([]EndpointSlice(nil), snapshot.EndpointSlices...)
	for i := range copy.EndpointSlices {
		copy.EndpointSlices[i].Ports = append([]EndpointSlicePort(nil), snapshot.EndpointSlices[i].Ports...)
	}
	copy.Pods = append([]Pod(nil), snapshot.Pods...)
	copy.Jobs = append([]Job(nil), snapshot.Jobs...)
	for i := range copy.Jobs {
		copy.Jobs[i].Conditions = append([]ResourceCondition(nil), snapshot.Jobs[i].Conditions...)
	}
	copy.Events = append([]KubernetesEvent(nil), snapshot.Events...)
	return copy
}
