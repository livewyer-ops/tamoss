package status

import (
	"testing"
	"time"
)

func TestWarningEventDeduperSuppressesWithinWindow(t *testing.T) {
	deduper := &WarningEventDeduper{}
	key := WarningEventKey{Namespace: "default", Name: "example", Reason: "IssuerUnreachable", Message: "issuer down"}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	if !deduper.ShouldRecord(key, now, time.Minute) {
		t.Fatal("first warning should be recorded")
	}
	if deduper.ShouldRecord(key, now.Add(30*time.Second), time.Minute) {
		t.Fatal("duplicate warning inside the window should be suppressed")
	}
	if !deduper.ShouldRecord(key, now.Add(time.Minute), time.Minute) {
		t.Fatal("warning at the window boundary should be recorded")
	}
}

func TestWarningEventDeduperKeysByReasonAndMessage(t *testing.T) {
	deduper := &WarningEventDeduper{}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	first := WarningEventKey{Namespace: "default", Name: "example", Reason: "IssuerUnreachable", Message: "issuer down"}
	second := WarningEventKey{Namespace: "default", Name: "example", Reason: "IssuerUnreachable", Message: "issuer still down"}

	if !deduper.ShouldRecord(first, now, time.Minute) {
		t.Fatal("first warning should be recorded")
	}
	if !deduper.ShouldRecord(second, now.Add(time.Second), time.Minute) {
		t.Fatal("same object and reason with a different message should be recorded")
	}
}
