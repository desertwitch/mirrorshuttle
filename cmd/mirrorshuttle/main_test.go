package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTestProgram(fs afero.Fs, opts *programOptions) (prog *program, stdout *bytes.Buffer, stderr *bytes.Buffer) {
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	if opts == nil {
		args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

		prog, err := newProgram(args, fs, stdout, stderr, false)
		if err != nil {
			panic("expected to set up a working program for testing")
		}

		return prog, stdout, stderr
	}

	return &program{
		fsys:     fs,
		stdout:   stdout,
		stderr:   stderr,
		testMode: false,
		opts:     opts,
		log: slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}, stdout, stderr
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

// Expectation: The program should run with a configuration file.
func Test_Integ_Run_ConfigFileOnly_Success(t *testing.T) {
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

	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)
	require.True(t, prog.opts.DryRun)
	require.Equal(t, "warn", prog.opts.LogLevel)
	require.True(t, prog.opts.JSON)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
}

// Expectation: The program should run with a configuration file and CLI overrides.
func Test_Integ_Run_ConfigFileWithFlagOverrides_Success(t *testing.T) {
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

	require.NotContains(t, stdout.String(), "dry mode")
	require.False(t, prog.opts.DryRun)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)

	_, err = fs.Stat("/badmirror")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror")
	require.NoError(t, err)
}

// Expectation: The program should run with a config file, excludes and CLI overrides.
func Test_Integ_Run_ConfigFileWithExcludesAndFlagOverride_Success(t *testing.T) {
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

	require.Equal(t, "/real/exclude-by-flag", prog.opts.Excludes[0])
	require.True(t, prog.opts.SkipFailed)
	require.False(t, prog.opts.DryRun)

	_, err = fs.Stat("/mirror-should-not-be-used")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/exclude-by-flag")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/exclude-by-yaml")
	require.NoError(t, err)
}

// Expectation: The program should run with a configuration file that has multiple excludes.
func Test_Integ_Run_ConfigFileWithMultipleExcludes_Success(t *testing.T) {
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

	require.Equal(t, "/real/exclude1", prog.opts.Excludes[0])
	require.Equal(t, "/real/exclude2", prog.opts.Excludes[1])

	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/exclude1")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/exclude2")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// The program should run init mode with only the required CLI arguments.
func Test_Integ_Run_ValidInitMode_Success(t *testing.T) {
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

// Expectation: The program should run move mode with only the required CLI arguments.
func Test_Integ_Run_ValidMoveMode_Success(t *testing.T) {
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

// Expectation: The program should produce the partial failure exit code.
func Test_Integ_Run_SkipFailed_PartialFailureExitCode_Success(t *testing.T) {
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

// Expectation: The program should produce the full failure exit code.
func Test_Integ_Run_NoSkipFailed_FailureExitCode_Error(t *testing.T) {
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

// Expectation: The program should produce the unmoved files exit code.
func Test_Integ_Run_UnmovedFilesExitCode_Success(t *testing.T) {
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

// Expectation: The program should produce the dry run mode warning.
func Test_Integ_Run_DryRunMode_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real", "--dry-run"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.True(t, prog.opts.DryRun)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "running in dry mode")
}

// Expectation: The program should produce normalized exclude paths.
func Test_Integ_Run_ExcludeSanitation_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real", "--exclude=/real/dir1//", "--exclude= /real/dir2 "}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	require.Equal(t, "/real/dir1", prog.opts.Excludes[0])
	require.Equal(t, "/real/dir2", prog.opts.Excludes[1])

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
	require.Contains(t, stderr.String(), "skipped")
}

// Expectation: The program should produce normalized paths.
func Test_Integ_Run_PathCleaning_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror= /mirror// ", "--target= /real/ "}

	prog, _ := newProgram(args, fs, &stdout, &stderr, false)
	require.NotNil(t, prog)

	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)

	exitCode, err := prog.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, exitCodeSuccess, exitCode)
}

// Expectation: The program should respond to context cancellation in init mode.
func Test_Integ_Run_ValidInitMode_CtxCancel_Error(t *testing.T) {
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

// Expectation: The program should respond to context cancellation in move mode.
func Test_Integ_Run_ValidMoveMode_CtxCancel_Error(t *testing.T) {
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

// Expectation: The program should produce the mirror-not-empty exit code.
func Test_Integ_Run_InitNonEmptyMirrorExitCode_Error(t *testing.T) {
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

// Expectation: The program should not establish with invalid flags.
func Test_Integ_NewProgram_InvalidFlags_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--invalid-flag"}

	prog, _ := newProgram(args, fs, &stdout, &stderr, true)
	require.Nil(t, prog)
}

// Expectation: The program should not establish with a missing config file.
func Test_Integ_NewProgram_MissingConfigFile_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--config=/config.yaml"}

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.ErrorIs(t, err, errArgConfigMissing)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgConfigMissing.Error())
}

// Expectation: The program should not establish with a malformed config file.
func Test_Integ_NewProgram_MalformedConfigFile_Error(t *testing.T) {
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

// Expectation: The program should not establish with a malformed config file.
func Test_Integ_NewProgram_MalformedConfigFileField_Error(t *testing.T) {
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

// Expectation: The program should not establish with an invalid mode.
func Test_Integ_NewProgram_InvalidMode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=invalid", "--mirror=/mirror", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgModeMismatch)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgModeMismatch.Error())
}

// Expectation: The program should not establish with a missing mode.
func Test_Integ_NewProgram_MissingMode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mirror=/mirror", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgModeMismatch)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgModeMismatch.Error())
}

// Expectation: The program should not establish with a missing mirror.
func Test_Integ_NewProgram_MissingMirror_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMissingMirrorTarget)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMissingMirrorTarget.Error())
}

// Expectation: The program should not establish with a missing target.
func Test_Integ_NewProgram_MissingTarget_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMissingMirrorTarget)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMissingMirrorTarget.Error())
}

// Expectation: The program should not establish with equal mirror and target.
func Test_Integ_NewProgram_SameMirrorAndTarget_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/same", "--target=/same"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMirrorTargetSame)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMirrorTargetSame.Error())
}

// Expectation: The program should not establish with relative mirror.
func Test_Integ_NewProgram_RelativeMirrorPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=relative/path", "--target=/absolute"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMirrorTargetNotAbs.Error())
}

// Expectation: The program should not establish with relative target.
func Test_Integ_NewProgram_RelativeTargetPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/absolute", "--target=relative/path"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgMirrorTargetNotAbs.Error())
}

// Expectation: The program should not establish with relative exclude paths.
func Test_Integ_NewProgram_RelativeExcludePath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real", "--exclude=relative/path"}

	prog, err := newProgram(args, fs, &stdout, &stderr, false)
	require.ErrorIs(t, err, errArgExcludePathNotAbs)
	require.Nil(t, prog)

	require.Contains(t, stderr.String(), errArgExcludePathNotAbs.Error())
}
