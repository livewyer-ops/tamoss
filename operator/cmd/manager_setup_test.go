package main

import (
	"net/http/httptest"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
)

type healthCheckManager struct {
	ctrl.Manager
	server webhook.Server
	live   healthz.Checker
	ready  healthz.Checker
}

func (m *healthCheckManager) GetWebhookServer() webhook.Server { return m.server }

func (m *healthCheckManager) AddHealthzCheck(_ string, check healthz.Checker) error {
	m.live = check
	return nil
}

func (m *healthCheckManager) AddReadyzCheck(_ string, check healthz.Checker) error {
	m.ready = check
	return nil
}

func TestReadinessWaitsForEnabledWebhookServer(t *testing.T) {
	for _, enabled := range []string{"", "false"} {
		t.Run("ENABLE_WEBHOOKS="+enabled, func(t *testing.T) {
			t.Setenv("ENABLE_WEBHOOKS", enabled)
			mgr := &healthCheckManager{}
			if enabled != "false" {
				mgr.server = webhook.NewServer(webhook.Options{})
			}
			if err := addHealthChecks(mgr); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("GET", "/readyz", nil)
			if err := mgr.live(request); err != nil {
				t.Fatalf("liveness before webhook start: %v", err)
			}
			if err := mgr.ready(request); (err == nil) != (enabled == "false") {
				t.Fatalf("readiness before webhook start with ENABLE_WEBHOOKS=%q: %v", enabled, err)
			}
		})
	}
}

func TestAuthentikProbeTimeoutFromEnvironment(t *testing.T) {
	t.Setenv("TAMOSS_AUTHENTIK_PROBE_TIMEOUT", "45s")
	if got := authentikProbeTimeout(); got != 45*time.Second {
		t.Fatalf("expected 45s timeout, got %s", got)
	}
}

func TestAuthentikProbeTimeoutUsesDefaultForInvalidValues(t *testing.T) {
	for _, value := range []string{"", "invalid", "0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TAMOSS_AUTHENTIK_PROBE_TIMEOUT", value)
			if got := authentikProbeTimeout(); got != authentik.DefaultProbeTimeout {
				t.Fatalf("expected default timeout %s, got %s", authentik.DefaultProbeTimeout, got)
			}
		})
	}
}
