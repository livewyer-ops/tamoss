package main

import (
	"testing"
	"time"

	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
)

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
