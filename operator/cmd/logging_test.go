package main

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestParseLogLevel(t *testing.T) {
	tests := map[string]zapcore.Level{
		"debug":   zapcore.DebugLevel,
		"info":    zapcore.InfoLevel,
		"warning": zapcore.WarnLevel,
		"error":   zapcore.ErrorLevel,
		"0":       zapcore.InfoLevel,
		"2":       zapcore.Level(-2),
	}

	for value, want := range tests {
		got, err := parseLogLevel(value)
		if err != nil {
			t.Fatalf("parseLogLevel(%q) returned error: %v", value, err)
		}
		if got != want {
			t.Fatalf("parseLogLevel(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestParseLogLevelOverrides(t *testing.T) {
	got, err := parseLogLevelOverrides("setup=debug, controllers.Tamoss=2")
	if err != nil {
		t.Fatalf("parseLogLevelOverrides returned error: %v", err)
	}

	if got["setup"] != zapcore.DebugLevel {
		t.Fatalf("setup override = %v, want %v", got["setup"], zapcore.DebugLevel)
	}
	if got["controllers.Tamoss"] != zapcore.Level(-2) {
		t.Fatalf("controllers.Tamoss override = %v, want %v", got["controllers.Tamoss"], zapcore.Level(-2))
	}
}

func TestComponentLevelUsesMostSpecificParent(t *testing.T) {
	config := loggingConfig{
		BaseLevel: zapcore.InfoLevel,
		Overrides: map[string]zapcore.Level{
			"controllers":        zapcore.DebugLevel,
			"controllers.Tamoss": zapcore.ErrorLevel,
		},
	}

	if got := config.componentLevel("controllers.Tamoss.reconcile"); got != zapcore.ErrorLevel {
		t.Fatalf("component level = %v, want %v", got, zapcore.ErrorLevel)
	}
	if got := config.componentLevel("controllers.Other"); got != zapcore.DebugLevel {
		t.Fatalf("component level = %v, want %v", got, zapcore.DebugLevel)
	}
	if got := config.componentLevel("setup"); got != zapcore.InfoLevel {
		t.Fatalf("component level = %v, want %v", got, zapcore.InfoLevel)
	}
}
