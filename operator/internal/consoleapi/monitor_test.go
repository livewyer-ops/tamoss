package consoleapi

import (
	"context"
	"errors"
	"testing"
)

type sourceResult struct {
	snapshot RuntimeSnapshot
	err      error
}

type queuedSource struct {
	results []sourceResult
}

func (s *queuedSource) Snapshot(context.Context) (RuntimeSnapshot, error) {
	result := s.results[0]
	s.results = s.results[1:]
	return result.snapshot, result.err
}

func TestMonitorPublishesChangedAndStaleSnapshots(t *testing.T) {
	t.Parallel()
	source := &queuedSource{results: []sourceResult{
		{snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, ObservedAt: "first", Instance: Instance{Phase: "Ready"}}},
		{snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, ObservedAt: "second", Instance: Instance{Phase: "Ready"}}},
		{err: errors.New("API unavailable")},
		{snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, ObservedAt: "third", Instance: Instance{Phase: "Ready"}}},
	}}
	monitor := NewMonitor(source)
	updates, cancel := monitor.Subscribe()
	defer cancel()

	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := <-updates
	if first.ID != 1 || first.Snapshot.Stale {
		t.Fatalf("unexpected first update: %#v", first)
	}
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-updates:
		t.Fatalf("unchanged content produced update: %#v", update)
	default:
	}
	current, ready, found := monitor.Current()
	if !ready || !found || current.ObservedAt != "second" {
		t.Fatalf("current snapshot was not refreshed: %#v, ready=%t found=%t", current, ready, found)
	}

	if err := monitor.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want source error")
	}
	stale := <-updates
	if stale.ID != 2 || !stale.Snapshot.Stale {
		t.Fatalf("unexpected stale update: %#v", stale)
	}
	_, ready, found = monitor.Current()
	if ready || !found {
		t.Fatalf("stale current state should be found but not ready: ready=%t found=%t", ready, found)
	}

	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered := <-updates
	if recovered.ID != 3 || recovered.Snapshot.Stale {
		t.Fatalf("unexpected recovery update: %#v", recovered)
	}
}

func TestMonitorSlowSubscriberReceivesLatestState(t *testing.T) {
	t.Parallel()
	source := &queuedSource{results: []sourceResult{
		{snapshot: RuntimeSnapshot{ObservedAt: "one", Instance: Instance{Phase: "Pending"}}},
		{snapshot: RuntimeSnapshot{ObservedAt: "two", Instance: Instance{Phase: "Progressing"}}},
	}}
	monitor := NewMonitor(source)
	updates, cancel := monitor.Subscribe()
	defer cancel()
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	update := <-updates
	if update.ID != 2 || update.Snapshot.Instance.Phase != "Progressing" {
		t.Fatalf("slow subscriber got %#v, want latest state", update)
	}
}
