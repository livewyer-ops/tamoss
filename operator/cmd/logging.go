package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	gozap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	logLevelEnv          = "TAMOSS_OPERATOR_LOG_LEVEL"
	logLevelOverridesEnv = "TAMOSS_OPERATOR_LOG_LEVEL_OVERRIDES"
)

type loggingConfig struct {
	BaseLevel zapcore.Level
	Overrides map[string]zapcore.Level
}

func loggingConfigFromEnv() (loggingConfig, error) {
	baseLevel, err := parseLogLevel(envOrDefault(logLevelEnv, "info"))
	if err != nil {
		return loggingConfig{}, fmt.Errorf("%s: %w", logLevelEnv, err)
	}

	overrides, err := parseLogLevelOverrides(os.Getenv(logLevelOverridesEnv))
	if err != nil {
		return loggingConfig{}, fmt.Errorf("%s: %w", logLevelOverridesEnv, err)
	}

	return loggingConfig{
		BaseLevel: baseLevel,
		Overrides: overrides,
	}, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func parsedFlagValue(name string) (string, bool) {
	var value string
	seen := false
	flag.CommandLine.Visit(func(f *flag.Flag) {
		if f.Name == name {
			value = f.Value.String()
			seen = true
		}
	})
	return value, seen
}

func parseLogLevel(value string) (zapcore.Level, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	}

	verbosity, err := strconv.Atoi(normalized)
	if err != nil {
		return zapcore.InfoLevel, fmt.Errorf("invalid log level %q", value)
	}
	if verbosity < 0 {
		return zapcore.InfoLevel, fmt.Errorf("verbosity must be zero or greater")
	}
	if verbosity > 127 {
		return zapcore.InfoLevel, fmt.Errorf("verbosity must be 127 or less")
	}
	return zapcore.Level(-1 * verbosity), nil //nolint:gosec // Verbosity is range-checked for zapcore.Level.
}

func parseLogLevelOverrides(raw string) (map[string]zapcore.Level, error) {
	overrides := map[string]zapcore.Level{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		component, levelRaw, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("override %q must use component=level", part)
		}
		component = strings.TrimSpace(component)
		if component == "" {
			return nil, fmt.Errorf("override %q has an empty component", part)
		}

		level, err := parseLogLevel(levelRaw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", component, err)
		}
		overrides[component] = level
	}
	return overrides, nil
}

func (cfg loggingConfig) applyMinimumLevel(opts *crzap.Options) {
	level := gozap.NewAtomicLevelAt(cfg.minimumLevel())
	opts.Level = &level
}

func (cfg loggingConfig) minimumLevel() zapcore.Level {
	minimum := cfg.BaseLevel
	for _, level := range cfg.Overrides {
		if level < minimum {
			minimum = level
		}
	}
	return minimum
}

func (cfg loggingConfig) componentLevel(name string) zapcore.Level {
	if level, ok := cfg.Overrides[name]; ok {
		return level
	}

	for {
		lastDot := strings.LastIndexByte(name, '.')
		if lastDot < 0 {
			return cfg.BaseLevel
		}
		name = name[:lastDot]
		if level, ok := cfg.Overrides[name]; ok {
			return level
		}
	}
}

func (cfg loggingConfig) wrapCore(core zapcore.Core) zapcore.Core {
	return componentLevelCore{
		Core:   core,
		config: cfg,
	}
}

type componentLevelCore struct {
	zapcore.Core
	config loggingConfig
}

func (c componentLevelCore) Enabled(level zapcore.Level) bool {
	return level >= c.config.minimumLevel()
}

func (c componentLevelCore) With(fields []zapcore.Field) zapcore.Core {
	return componentLevelCore{
		Core:   c.Core.With(fields),
		config: c.config,
	}
}

func (c componentLevelCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if entry.Level < c.config.componentLevel(entry.LoggerName) {
		return checked
	}
	return c.Core.Check(entry, checked)
}
