package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

//nolint:unparam
func setupTestProgram(fs afero.Fs, opts *programOptions, stdout io.Writer, stderr io.Writer) *program {
	if stdout == nil {
		stdout = &bytes.Buffer{}
	}

	if stderr == nil {
		stderr = &bytes.Buffer{}
	}

	if opts == nil {
		args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

		prog, err := newProgram(args, fs, stdout, stderr, false)
		if err != nil {
			panic("expected to set up a working program for testing")
		}

		return prog
	}

	return &program{
		fsys:     fs,
		stdout:   stdout,
		stderr:   stderr,
		testMode: false,
		opts:     opts,
		log: slog.New(slog.NewTextHandler(stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}
}

type flakyFs struct {
	afero.Fs
	failOnPath string
}

func (f flakyFs) Rename(oldname, newname string) error {
	if strings.Contains(newname, f.failOnPath) {
		return fmt.Errorf("simulated rename failure: %q", newname)
	}

	return f.Fs.Rename(oldname, newname)
}

func setupTestFs() afero.Fs {
	fs := afero.NewMemMapFs()

	return fs
}

func createDirStructure(fs afero.Fs, paths []string) error {
	for _, path := range paths {
		if err := fs.MkdirAll(path, 0o777); err != nil {
			return err
		}
	}

	return nil
}

func createFiles(fs afero.Fs, files map[string]string) error {
	for path, content := range files {
		if err := fs.MkdirAll(filepath.Dir(path), 0o777); err != nil {
			return err
		}
		if err := afero.WriteFile(fs, path, []byte(content), 0o666); err != nil {
			return err
		}
	}

	return nil
}

func TestRun_ConfigFileOnly_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	yaml := `
mirror: /mirror
target: /real
dry-run: true
log-level: warn
json: true
`

	files := map[string]string{
		"/config.yaml": yaml,
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--config=/config.yaml"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "dry mode")

	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

func TestRun_ConfigFileWithFlagOverrides_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	yaml := `
mirror: /badmirror
target: /real
dry-run: true
log-level: invalid
json: false
`

	files := map[string]string{
		"/config.yaml": yaml,
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{
		"program",
		"--mode=init",
		"--config=/config.yaml",
		"--mirror=/mirror", // override YAML
		"--dry-run=false",  // override YAML
		"--json",
		"--log-level=warn",
	}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)
	require.Equal(t, exitCodeSuccess, exitCode)
	require.NotContains(t, stdout.String(), "running in dry mode")

	_, err = fs.Stat("/badmirror")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror")
	require.NoError(t, err)

	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

func TestRun_ConfigFileWithExcludesAndFlagOverride_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	err := createDirStructure(fs, []string{
		"/real/include",
		"/real/exclude-by-yaml",
		"/real/exclude-by-flag",
	})
	require.NoError(t, err)

	yaml := `
mirror: /mirror-should-not-be-used
target: /real
dry-run: true
skip-failed: false
verify: true
exclude:
  - /real/exclude-by-yaml
`

	files := map[string]string{
		"/config.yaml": yaml,
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{
		"program",
		"--mode=init",
		"--config=/config.yaml",
		"--mirror=/mirror",
		"--dry-run=false",
		"--skip-failed=true",              // override YAML
		"--exclude=/real/exclude-by-flag", // override YAML
	}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)
	require.True(t, prog.opts.SkipFailed)
	require.True(t, prog.opts.Verify)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)
	require.Equal(t, exitCodeSuccess, exitCode)

	_, err = fs.Stat("/mirror-should-not-be-used")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/exclude-by-flag")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/exclude-by-yaml")
	require.NoError(t, err)
}

func TestRun_ConfigFileWithMultipleExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	err := createDirStructure(fs, []string{
		"/real/include",
		"/real/exclude1",
		"/real/exclude2",
		"/real/exclude1/subdir",
		"/real/exclude2/subdir",
	})
	require.NoError(t, err)

	yaml := `
mirror: /mirror
target: /real
exclude:
  - /real/exclude1
  - /real/exclude2
`

	files := map[string]string{
		"/config.yaml": yaml,
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{
		"program",
		"--mode=init",
		"--config=/config.yaml",
	}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)
	require.Equal(t, exitCodeSuccess, exitCode)

	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/exclude1")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/exclude2")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRun_ValidInitMode_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
}

func TestRun_ValidMoveMode_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/file.txt": "content",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
}

func TestRun_SkipFailed_SimulatedPartialFailure_Success(t *testing.T) {
	t.Parallel()

	base := setupTestFs()
	fs := flakyFs{Fs: base, failOnPath: "fail.txt"}

	err := createFiles(fs, map[string]string{
		"/mirror/ok.txt":   "ok",
		"/mirror/fail.txt": "fail",
	})
	require.NoError(t, err)

	err = createDirStructure(fs, []string{"/real"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real", "--skip-failed"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodePartialFailure, exitCode)

	// Should succeed
	_, err = fs.Stat("/real/ok.txt")
	require.NoError(t, err)

	// Should not be moved due to simulated failure
	_, err = fs.Stat("/real/fail.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Contains(t, stderr.String(), "simulated rename failure")
}

func TestRun_NoSkipFailed_SimulatedPartialFailure_Error(t *testing.T) {
	t.Parallel()

	base := setupTestFs()
	fs := flakyFs{Fs: base, failOnPath: "fail.txt"}

	err := createFiles(fs, map[string]string{
		"/mirror/ok.txt":   "ok",
		"/mirror/fail.txt": "fail",
	})
	require.NoError(t, err)

	err = createDirStructure(fs, []string{"/real"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	exitCode, err := prog.run(t.Context())
	require.Error(t, err)

	require.Equal(t, exitCodeFailure, exitCode)

	// Should not succeed - no files moved
	_, err = fs.Stat("/real/ok.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/fail.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Contains(t, stderr.String(), "simulated rename failure")
}

func TestRun_UnmovedFilesExitCode_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/file.txt": "content",
		"/real/file.txt":   "content2",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	require.Equal(t, exitCodeUnmovedFiles, exitCode)
	require.Contains(t, stderr.String(), "unmoved files")
}

func TestRun_ExcludedSourceAndDestination_NoOp(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	err := createFiles(fs, map[string]string{
		"/mirror/excluded/file.txt": "should-not-move",
	})
	require.NoError(t, err)

	err = createDirStructure(fs, []string{"/real/excluded"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{
		"program", "--mode=move", "--mirror=/mirror", "--target=/real",
		"--exclude=/mirror/excluded", "--exclude=/real/excluded",
	}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")

	// File should not appear in destination
	_, err = fs.Stat("/real/excluded/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// File should remain in source
	_, err = fs.Stat("/mirror/excluded/file.txt")
	require.NoError(t, err)
}

func TestRun_UnmovedFilesExclusionSrc_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/exclude/file.txt": "content",
		"/mirror/file.txt":         "content2",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real", "--exclude=/mirror/exclude"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	_, err = fs.Stat("/real/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/exclude/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")
}

func TestRun_UnmovedFilesExclusionDst_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/exclude/file.txt": "content",
		"/mirror/file.txt":         "content2",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real", "--exclude=/real/exclude"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	_, err = fs.Stat("/real/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/exclude/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")
}

func TestRun_UnmovedDirExclusionSrc_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror/exclude", "/real"})
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/file.txt": "content2",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real", "--exclude=/mirror/exclude"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	_, err = fs.Stat("/real/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")
}

func TestRun_UnmovedDirExclusionDst_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror/exclude", "/real"})
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/file.txt": "content2",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real", "--exclude=/real/exclude"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	_, err = fs.Stat("/real/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")
}

func TestRun_DryRunModeAndSkipFailed_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real", "--skip-failed", "--dry-run"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.True(t, prog.opts.SkipFailed)              // option should be set
	require.NotContains(t, stderr.String(), "skipped") // but should not have really failed

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "running in dry mode")
}

func TestRun_MultipleExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real", "--exclude=/real/dir1", "--exclude=/real/dir2"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")
}

func TestRun_PathCleaning_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror= /mirror// ", "--target= /real/ "}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
}

func TestRun_ValidInitMode_CtxCancel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	exitCode, err := prog.run(ctx)
	require.ErrorIs(t, err, context.Canceled)

	require.Equal(t, exitCodeFailure, exitCode)
	require.NotContains(t, stderr.String(), context.Canceled.Error())
}

func TestRun_ValidMoveMode_CtxCancel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	exitCode, err := prog.run(ctx)
	require.ErrorIs(t, err, context.Canceled)

	require.Equal(t, exitCodeFailure, exitCode)
	require.NotContains(t, stderr.String(), context.Canceled.Error())
}

func TestRun_TargetNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/nonexistent"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	require.Equal(t, exitCodeFailure, exitCode)
	require.Contains(t, stderr.String(), errTargetNotExist.Error())
}

func TestRun_MirrorNotExistForMove_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/nonexistent", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.ErrorIs(t, err, errMirrorNotExist)

	require.Equal(t, exitCodeFailure, exitCode)
	require.Contains(t, stderr.String(), errMirrorNotExist.Error())
}

func TestRun_TargetNotExistForMove_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--mirror=/mirror", "--target=/notexist"}

	paths := []string{
		"/mirror",
		"/mirror/dir2/subdir",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	require.Equal(t, exitCodeFailure, exitCode)
	require.Contains(t, stderr.String(), errTargetNotExist.Error())
}

func TestRun_InitNonEmptyMirrorExitCode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	paths := []string{
		"/real",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	files := map[string]string{
		"/mirror/existing.txt": "content",
	}
	err = createFiles(fs, files)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)

	require.Equal(t, exitCodeMirrNotEmpty, exitCode)
	require.Contains(t, stderr.String(), errMirrorNotEmpty.Error())
}

func TestNewProgram_InvalidFlags_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--invalid-flag"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, true)
	require.Nil(t, prog)
}

func TestNewProgram_MissingConfigFile_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--config=/config.yaml"}

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.ErrorIs(t, err, errArgConfigMissing)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgConfigMissing.Error())
}

func TestNewProgram_MalformedConfigFile_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--config=/config.yaml"}

	yaml := "MALFORMED"

	files := map[string]string{
		"/config.yaml": yaml,
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.ErrorIs(t, err, errArgConfigMalformed)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgConfigMalformed.Error())
}

func TestNewProgram_MalformedConfigFileField_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--config=/config.yaml"}

	yaml := `
myrror: /mirror
target: /real
exclude:
  - /real/exclude1
  - /real/exclude2
`

	files := map[string]string{
		"/config.yaml": yaml,
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.ErrorIs(t, err, errArgConfigMalformed)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgConfigMalformed.Error())
}

func TestNewProgram_InvalidMode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=invalid", "--mirror=/mirror", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgModeMismatch)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgModeMismatch.Error())
}

func TestNewProgram_MissingMode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mirror=/mirror", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgModeMismatch)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgModeMismatch.Error())
}

func TestNewProgram_MissingMirror_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMissingMirrorTarget)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMissingMirrorTarget.Error())
}

func TestNewProgram_MissingTarget_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMissingMirrorTarget)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMissingMirrorTarget.Error())
}

func TestNewProgram_SameMirrorAndTarget_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/same", "--target=/same"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMirrorTargetSame)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMirrorTargetSame.Error())
}

func TestNewProgram_RelativeMirrorPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=relative/path", "--target=/absolute"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMirrorTargetNotAbs.Error())
}

func TestNewProgram_RelativeTargetPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/absolute", "--target=relative/path"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMirrorTargetNotAbs.Error())
}

func TestNewProgram_RelativeExcludePath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real", "--exclude=relative/path"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgExcludePathNotAbs)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgExcludePathNotAbs.Error())
}
