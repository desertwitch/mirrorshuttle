package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
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

func (prog *program) walkError(e fs.FileInfo, err error) error {
	if !errors.Is(err, context.Canceled) && prog.opts.SkipFailed {
		prog.state.hasPartialFailures = true

		prog.log.Error("path skipped",
			"op", prog.opts.Mode,
			"error", err,
			"error-type", "runtime",
			"reason", "error_occurred",
		)

		if e.IsDir() {
			return filepath.SkipDir // Do not traverse deeper.
		}

		return nil
	}

	return err
}

func (prog *program) isEmptyStructure(ctx context.Context, path string) (bool, error) {
	path = filepath.Clean(strings.TrimSpace(path))

	empty := true

	// Walk the given path for any files in the structure.
	if err := afero.Walk(prog.fsys, path, func(subpath string, e os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			// An interrupt was received, also interrupt the walk.
			return fmt.Errorf("failed checking context: %w", err)
		}

		if err != nil {
			// An error has occurred (permissioning, ...), not safe to continue.
			return fmt.Errorf("failed to walk: %q (%w)", subpath, err)
		}

		if !e.IsDir() {
			empty = false
			if prog.opts.Mode == "init" {
				// Output the file that was found, but also continue to get the full list.
				prog.log.Warn("unmoved file found", "op", prog.opts.Mode, "path", subpath)
			} else {
				// Immediately return in other modes, where we do not care about the output.
				return filepath.SkipAll
			}
		}

		return nil
	}); err != nil && !errors.Is(err, filepath.SkipAll) {
		return false, err
	}

	if !empty {
		// The structure contained files.
		return false, nil
	}

	// The structure contained no files.
	return true, nil
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
