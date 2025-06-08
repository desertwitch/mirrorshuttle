package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTestFs() afero.Fs {
	fs := afero.NewMemMapFs()

	return fs
}

func setupTestProgram(fs afero.Fs, opts *programOptions) *program {
	if opts == nil {
		args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

		prog, err := newProgram(args, fs, os.Stdout, os.Stderr, false)
		if err != nil {
			panic("expected to set up a working program for testing")
		}

		return prog
	}

	return &program{
		fsys:     fs,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		testMode: false,
		opts:     opts,
	}
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
	require.Contains(t, stdout.String(), "dry-run")
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
	require.Empty(t, stderr.String())
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
	require.Empty(t, stderr.String())
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

func TestRun_UnmovedFilesExclusionSrcExitCode_Success(t *testing.T) {
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

	require.Equal(t, exitCodeUnmovedFiles, exitCode)
	require.Contains(t, stderr.String(), "unmoved files")
}

func TestRun_UnmovedFilesExclusionDstExitCode_Success(t *testing.T) {
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

	require.Equal(t, exitCodeUnmovedFiles, exitCode)
	require.Contains(t, stderr.String(), "unmoved files")
}

func TestRun_UnmovedFoldersExclusionSrcExitCode_Success(t *testing.T) {
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
	require.Contains(t, stderr.String(), "skipped:")
}

func TestRun_UnmovedFoldersExclusionDstExitCode_Success(t *testing.T) {
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
	require.Contains(t, stderr.String(), "skipped:")
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
	require.Contains(t, stderr.String(), "skipped:")
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
	require.Empty(t, stderr.String())
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

func TestIsEmptyStructure_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	paths := []string{
		"/empty/dir1",
		"/empty/dir2/subdir",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/empty")
	require.NoError(t, err)
	require.True(t, empty)
}

func TestIsEmptyStructure_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/nonempty/dir1/file.txt": "content",
		"/nonempty/dir2/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/nonempty")
	require.NoError(t, err)
	require.False(t, empty)
}

func TestIsEmptyStructure_CtxCancel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	paths := []string{
		"/empty/dir1",
		"/empty/dir2/subdir",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(ctx, "/empty")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, empty)
}

func TestIsEmptyStructure_NonExistentPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/nonexistent")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.False(t, empty)
}

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

func TestCopyAndRemove_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/src/file.txt": "test content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	err = prog.copyAndRemove("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	// Verify source is removed.
	_, err = fs.Stat("/src/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify destination exists with correct content.
	content, err := afero.ReadFile(fs, "/dst/file.txt")
	require.NoError(t, err)
	require.Equal(t, "test content", string(content))
}

func TestCopyAndRemove_Verify_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/src/file.txt": "test content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	prog.opts.Verify = true

	err = prog.copyAndRemove("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	// Verify source is removed.
	_, err = fs.Stat("/src/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify destination exists with correct content.
	content, err := afero.ReadFile(fs, "/dst/file.txt")
	require.NoError(t, err)
	require.Equal(t, "test content", string(content))

	// Verify the requested mode did not change within the program.
	require.True(t, prog.opts.Verify)
}

func TestCopyAndRemove_SourceNotFound_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil)
	err := prog.copyAndRemove("/nonexistent/file.txt", "/dst/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCopyAndRemove_DstTmpFileExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/src/file.txt":        "hello",
		"/dst/file.txt.mirsht": "existing",
	}
	require.NoError(t, createFiles(fs, files))

	prog := setupTestProgram(fs, nil)

	err := prog.copyAndRemove("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	_, err = fs.Stat("/dst/file.txt")
	require.NoError(t, err)

	_, err = fs.Stat("/dst/file.txt.mirsht")
	require.ErrorIs(t, err, os.ErrNotExist)
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

func TestCreateMirrorStructure_EmptyMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/mirror",
		"/real/dir1",
		"/real/dir2/subdir",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)
	_, err = fs.Stat("/mirror/dir2/subdir")
	require.NoError(t, err)
}

func TestCreateMirrorStructure_NonExistentMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/dir1",
		"/real/dir2",
		"/real/dir2/dir3",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/mirror")
	require.NoError(t, err)
	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)
	_, err = fs.Stat("/mirror/dir2/dir3")
	require.NoError(t, err)
}

func TestCreateMirrorStructure_CtxCancel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/mirror",
		"/real/dir1",
		"/real/dir2/subdir",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// Verify mirror structure is not created.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat("/mirror/dir2/subdir")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_MirrorWithFiles_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/existing.txt": "content",
		"/real/dir1/file.txt":  "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)
}

func TestCreateMirrorStructure_NestedMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/mirror",
		"/real/dir1",
		"/real/dir2/subdir",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/real/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/real/mirror/dir1")
	require.NoError(t, err)
	_, err = fs.Stat("/real/mirror/dir2/subdir")
	require.NoError(t, err)

	// Verify nested mirror structure is not turning into insane recursion.
	_, err = fs.Stat("/real/mirror/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_WithFiles_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	paths := []string{
		"/mirror",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	err = createFiles(fs, map[string]string{
		"/real/dir1":          "test",
		"/real/dir2":          "test",
		"/real/dir1/file.txt": "test",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/mirror")
	require.NoError(t, err)
	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)
}

func TestCreateMirrorStructure_WithExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/include",
		"/real/exclude",
		"/real/exclude/subdir",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		Excludes:   excludeArg{"/real/exclude"},
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify included directory is mirrored.
	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	// Verify excluded directory is not mirrored.
	_, err = fs.Stat("/mirror/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_DryRun_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_DryRun_MirrorExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1", "/mirror"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_DryRun_FullMirror_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createFiles(fs, map[string]string{
		"/real/dir1":          "test",
		"/real/dir2":          "test",
		"/real/dir1/file.txt": "test",
		"/mirror/file.txt":    "test",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_RealRootNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/nonexistent",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err := prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	// Should not create mirror root.
	_, err = fs.Stat("/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_MirrorParentNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/notexist/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	err := createFiles(fs, map[string]string{
		"/real/dir1":          "test",
		"/real/dir2":          "test",
		"/real/dir1/file.txt": "test",
	})
	require.NoError(t, err)

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorParentNotExist)

	// Should not create mirror root.
	_, err = fs.Stat("/notexist/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_MirrorParentNotDir_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/notexist/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	err := createFiles(fs, map[string]string{
		"/real/dir1":          "test",
		"/real/dir2":          "test",
		"/real/dir1/file.txt": "test",
		"/notexist":           "test",
	})
	require.NoError(t, err)

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorParentNotDir)

	// Should not create mirror root.
	_, err = fs.Stat("/notexist/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_RegularMove_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":      "content",
		"/mirror/dir/file.txt":  "content2",
		"/mirror/dir1/file.txt": "content3",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real/dir",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify files moved to real structure.
	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content", string(content))

	content, err = afero.ReadFile(fs, "/real/dir/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	content, err = afero.ReadFile(fs, "/real/dir1/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content3", string(content))

	// Verify files removed from mirror.
	_, err = fs.Stat("/mirror/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_DirectMove_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":      "content",
		"/mirror/dir/file.txt":  "content2",
		"/mirror/dir1/file.txt": "content3",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real/dir",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		Direct:     true,
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify files moved to real structure.
	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content", string(content))

	content, err = afero.ReadFile(fs, "/real/dir/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	content, err = afero.ReadFile(fs, "/real/dir1/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content3", string(content))

	// Verify files removed from mirror.
	_, err = fs.Stat("/mirror/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_FileAlreadyExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt": "mirror content",
		"/real/file.txt":   "existing content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify existing file is not overwritten.
	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "existing content", string(content))

	// Verify mirror file still exists (not moved).
	content, err = afero.ReadFile(fs, "/mirror/file.txt")
	require.NoError(t, err)
	require.Equal(t, "mirror content", string(content))
}

func TestMoveFiles_WithExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/include.txt": "content",
		"/mirror/exclude.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		Excludes:   excludeArg{"/mirror/exclude.txt"},
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify included file is moved.
	_, err = fs.Stat("/real/include.txt")
	require.NoError(t, err)

	// Verify excluded file is not moved.
	_, err = fs.Stat("/real/exclude.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify excluded file still exists in mirror.
	_, err = fs.Stat("/mirror/exclude.txt")
	require.NoError(t, err)
}

func TestMoveFiles_WithDirExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/include.txt":         "content",
		"/mirror/exclude/exclude.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		Excludes:   excludeArg{"/mirror/exclude"},
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify included file is moved.
	_, err = fs.Stat("/real/include.txt")
	require.NoError(t, err)

	// Verify excluded folder is not created.
	_, err = fs.Stat("/real/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify excluded file is not moved.
	_, err = fs.Stat("/real/exclude/exclude.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify excluded file still exists in mirror.
	_, err = fs.Stat("/mirror/exclude/exclude.txt")
	require.NoError(t, err)
}

func TestMoveFiles_DryRun_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":       "content",
		"/mirror/dir1/file2.txt": "content",
		"/real/otherfile.txt":    "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/real/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify mirror file still exists.
	_, err = fs.Stat("/mirror/file.txt")
	require.NoError(t, err)
}

func TestMoveFiles_CreateTargetDirs_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/deep/nested/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	// Assume at least the target base exists
	paths := []string{
		"/real",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify target directory structure is created.
	_, err = fs.Stat("/real/deep/nested")
	require.NoError(t, err)

	// Verify file is moved.
	content, err := afero.ReadFile(fs, "/real/deep/nested/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content", string(content))
}

func TestMoveFiles_CtxCancel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":      "content",
		"/mirror/dir/file.txt":  "content2",
		"/mirror/dir1/file.txt": "content3",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real/dir",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// Verify files not removed from mirror.
	_, err = fs.Stat("/mirror/file.txt")
	require.NoError(t, err)

	// Verify files not moved.
	_, err = fs.Stat("/real/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/dir/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/dir1/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_CreateTargetDirs_BaseGone_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/deep/nested/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	// Verify the operation fails as base is missing.
	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	// Verify mirror file is not removed.
	_, err = fs.Stat("/mirror/deep/nested/file.txt")
	require.NoError(t, err)
}

func TestMoveFiles_EmptyMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)
}

func TestMoveFiles_MirrorNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/nonexistent",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err := prog.moveFiles(t.Context())
	require.ErrorIs(t, err, errMirrorNotExist)
}

func TestMoveFiles_DirectoryAlreadyExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/mirror/existingdir",
		"/real/existingdir",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify mirror directory still exists.
	_, err = fs.Stat("/mirror/existingdir")
	require.NoError(t, err)

	// Verify directory still exists.
	_, err = fs.Stat("/real/existingdir")
	require.NoError(t, err)
}

func TestMoveFiles_MoveIntoMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mnt/user/mirror/mirror/test.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mnt/user/mirror",
		RealRoot:   "/mnt/user",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify file is not moved into the mirror structure (pointless).
	_, err = fs.Stat("/mnt/user/mirror/test.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}
