package main

import (
	"fmt"
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
		prog.log.Error("skipped:", "error", err)

		return nil
	}

	return err
}

func isExcluded(path string, excludes []string) bool {
	path = filepath.Clean(path)

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
