package main

import (
	"context"
	"fmt"
	"io"
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
		return defaultLogLevel, errArgInvalidLogLevel
	}
}

func (prog *program) walkError(err error) error {
	if prog.opts.SkipFailed {
		prog.log.Error("path skipped", "op", prog.opts.Mode, "error", err, "error-type", "runtime", "reason", "error_occurred")
		prog.state.hasPartialFailures = true

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

func dirDepth(relPath string) int {
	return strings.Count(filepath.Clean(relPath), string(filepath.Separator))
}

type fileHashes struct {
	srcHash    string
	dstHash    string
	verifyHash string
}

// contextReader is an implementation of [io.Reader] that is Context-aware for
// receiving mid-transfer cancellation.
type contextReader struct {
	ctx    context.Context //nolint:containedctx
	reader io.Reader
}

// Read wraps the [io.Reader] reading function while being aware of and handling
// any mid-transfer Context cancellations.
func (cr *contextReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, context.Canceled
	default:
		return cr.reader.Read(p) //nolint:wrapcheck
	}
}
