package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsExcluded_ExactMatch_Success(t *testing.T) {
	t.Parallel()

	excludes := []string{
		"/home/user/docs",
		"/tmp/cache",
	}

	result := isExcluded("/home/user/docs", excludes)
	require.True(t, result)
}

func TestIsExcluded_SubPath_Success(t *testing.T) {
	t.Parallel()

	excludes := []string{
		"/home/user/docs",
	}

	result := isExcluded("/home/user/docs/file.txt", excludes)
	require.True(t, result)
}

func TestIsExcluded_NotExcluded_Success(t *testing.T) {
	t.Parallel()

	excludes := []string{
		"/home/user/docs",
	}

	result := isExcluded("/home/user/pictures", excludes)
	require.False(t, result)
}

func TestIsExcluded_EmptyExcludeList_Success(t *testing.T) {
	t.Parallel()

	var excludes []string

	result := isExcluded("/any/path", excludes)
	require.False(t, result)
}

func TestWalkError_SkipFailedTrue_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stderr bytes.Buffer

	opts := &programOptions{SkipFailed: true}
	prog := setupTestProgram(fs, opts)
	prog.stderr = &stderr

	err := errors.New("mock error")
	result := prog.walkError(err)

	require.NoError(t, result)
	require.True(t, prog.hasPartialFailures)
	require.Contains(t, stderr.String(), "skipped: mock error")
}

func TestWalkError_SkipFailedFalse_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stderr bytes.Buffer

	opts := &programOptions{SkipFailed: false}
	prog := setupTestProgram(fs, opts)
	prog.stderr = &stderr

	mockErr := errors.New("real error")
	result := prog.walkError(mockErr)

	require.Equal(t, mockErr, result)
	require.False(t, prog.hasPartialFailures)
	require.Empty(t, stderr.String())
}
