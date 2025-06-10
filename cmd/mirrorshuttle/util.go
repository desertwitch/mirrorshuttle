package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

type excludeArg []string

func (s *excludeArg) String() string {
	return fmt.Sprint(*s)
}

func (s *excludeArg) Set(value string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(value))

	*s = append(*s, cleanPath)

	return nil
}

func (prog *program) walkError(err error) error {
	if prog.opts.SkipFailed {
		prog.hasPartialFailures = true
		prog.log.Error("path skipped", "error", err, "reason", "error_occurred")

		return nil
	}

	return err
}

func isExcluded(path string, excludes []string) bool {
	path = filepath.Clean(strings.TrimSpace(path))

	for _, excl := range excludes {
		if path == excl {
			return true
		}
		if rel, err := filepath.Rel(excl, path); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}

	return false
}

func parseLogLevel(levelStr string) (slog.Level, error) {
	switch strings.TrimSpace(levelStr) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, errArgInvalidLogLevel
	}
}
