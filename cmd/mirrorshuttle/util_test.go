package main

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Expectation: The function should handle the exclusions according to the table's expectations.
func Test_Unit_IsExcluded_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		excludes []string
		expected bool
	}{
		{
			name:     "Exact match",
			path:     "/home/user/docs",
			excludes: []string{"/home/user/docs", "/tmp/cache"},
			expected: true,
		},
		{
			name:     "Sub-path match",
			path:     "/home/user/docs/file.txt",
			excludes: []string{"/home/user/docs"},
			expected: true,
		},
		{
			name:     "Not excluded",
			path:     "/home/user/pictures",
			excludes: []string{"/home/user/docs"},
			expected: false,
		},
		{
			name:     "Empty exclude list",
			path:     "/any/path",
			excludes: []string{},
			expected: false,
		},
		{
			name:     "Trailing slash in exclude",
			path:     "/var/log/syslog",
			excludes: []string{"/var/log/"},
			expected: true,
		},
		{
			name:     "Path with whitespace",
			path:     "/home/user/my documents/file.txt",
			excludes: []string{"/home/user/my documents"},
			expected: true,
		},
		{
			name:     "Unclean path with double slash",
			path:     "/tmp//cache/log.txt",
			excludes: []string{"/tmp/cache"},
			expected: true,
		},
		{
			name:     "Unclean path with whitespace and double slash",
			path:     " /tmp//cache/log.txt ",
			excludes: []string{"/tmp/cache"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isExcluded(tt.path, tt.excludes)
			require.Equal(t, tt.expected, result)
		})
	}
}

// Expectation: The function should report and skip errors, not return them.
func Test_Unit_WalkError_SkipFailedTrue_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{SkipFailed: true}
	prog, _, stderr := setupTestProgram(fs, opts)

	err := errors.New("mock error")
	result := prog.walkError(err)

	require.NoError(t, result)
	require.True(t, prog.state.hasPartialFailures)
	require.Contains(t, stderr.String(), "skipped")
}

// Expectation: The function should return errors, not skip them.
func Test_Unit_WalkError_SkipFailedFalse_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{SkipFailed: false}
	prog, stdout, _ := setupTestProgram(fs, opts)

	mockErr := errors.New("real error")
	result := prog.walkError(mockErr)

	require.Equal(t, mockErr, result)
	require.False(t, prog.state.hasPartialFailures)
	require.NotContains(t, stdout.String(), "skipped")
}

// Expectation: The function should parse the log level according to the table's expectations.
func Test_Unit_ParseLogLevel_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       string
		expected    slog.Level
		expectError bool
	}{
		{"debug", slog.LevelDebug, false},
		{" info ", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"", defaultLogLevel, true},
		{"verbose", defaultLogLevel, true},
		{"none", defaultLogLevel, true},
		{"123", defaultLogLevel, true},
		{"error", slog.LevelError, false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			level, err := parseLogLevel(tc.input)

			if tc.expectError {
				require.Error(t, err)
				require.Equal(t, tc.expected, level)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, level)
			}
		})
	}
}

// Expectation: The function should calculate the depth level according to the table's expectations.
func Test_Unit_DirDepth_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		relPath  string
		expected int
	}{
		{
			name:     "Root (dot)",
			relPath:  ".",
			expected: 0,
		},
		{
			name:     "Root-level directory",
			relPath:  "a",
			expected: 0,
		},
		{
			name:     "One-level nested directory",
			relPath:  "a/b",
			expected: 1,
		},
		{
			name:     "Two-level nested directory",
			relPath:  "a/b/c",
			expected: 2,
		},
		{
			name:     "With ./ prefix (./a)",
			relPath:  "./a",
			expected: 0,
		},
		{
			name:     "With ./ and nested (./a/b)",
			relPath:  "./a/b",
			expected: 1,
		},
		{
			name:     "With trailing slash (a/b/)",
			relPath:  "a/b/",
			expected: 1,
		},
		{
			name:     "Multiple slashes (a//b///c)",
			relPath:  "a//b///c",
			expected: 2,
		},
		{
			name:     "Empty string becomes dot",
			relPath:  "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clean := filepath.Clean(tt.relPath)
			result := dirDepth(clean)

			require.Equal(t, tt.expected, result, "relPath: %q (cleaned: %q)", tt.relPath, clean)
		})
	}
}
