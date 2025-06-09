package main

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestParseArgs_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer

	args := []string{
		"program",
		"--mode=init",
		"--mirror=/mirror",
		"--target=/real",
		"--exclude=/exclude",
		"--direct",
		"--verify",
		"--dry-run",
		"--skip-failed",
		"--json",
		"--log-level=warn",
	}

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.NoError(t, err)
	require.NotNil(t, prog)

	err = prog.parseArgs(args)
	require.NoError(t, err)

	require.Equal(t, "init", prog.opts.Mode)
	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)
	require.Equal(t, "/exclude", prog.opts.Excludes[0])
	require.True(t, prog.opts.Direct)
	require.True(t, prog.opts.Verify)
	require.True(t, prog.opts.SkipFailed)
	require.True(t, prog.opts.DryRun)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

func TestParseArgs_ConfigFile_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	yamlContent := `
mirror: /mirror
target: /real
direct: true
log-level: warn
json: true
`
	err := afero.WriteFile(fs, "/config.yaml", []byte(yamlContent), 0o644)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--config=/config.yaml"}

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.NoError(t, err)
	require.NotNil(t, prog)

	err = prog.parseArgs(args)
	require.NoError(t, err)

	require.Equal(t, "move", prog.opts.Mode)
	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)
	require.True(t, prog.opts.Direct)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

func TestParseArgs_ConfigFileOverride_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	yamlContent := `
mirror: /mirror2
target: /real2
direct: false
verify: false
dry-run: false
skip-failed: false
json: false
log-level: invalid
`
	err := afero.WriteFile(fs, "/config.yaml", []byte(yamlContent), 0o644)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer

	args := []string{
		"program",
		"--mode=init",
		"--config=/config.yaml",
		"--mirror=/mirror",
		"--target=/real",
		"--direct",
		"--verify",
		"--dry-run",
		"--skip-failed",
		"--json",
		"--log-level=warn",
	}

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.NoError(t, err)
	require.NotNil(t, prog)

	err = prog.parseArgs(args)
	require.NoError(t, err)

	require.Equal(t, "init", prog.opts.Mode)
	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)
	require.True(t, prog.opts.Direct)
	require.True(t, prog.opts.Verify)
	require.True(t, prog.opts.SkipFailed)
	require.True(t, prog.opts.DryRun)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

func TestValidateOpts_ValidOptions_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil, nil, nil)
	prog.opts = &programOptions{
		Mode:       "init",
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
	}

	err := prog.validateOpts()
	require.NoError(t, err)
}

func TestParseArgs_InvalidLogLevel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=init", "--mirror=/mirror", "--target=/real"}

	prog, err := newProgram(args, fs, &stdout, &stderr, true)
	require.NoError(t, err)
	require.NotNil(t, prog)

	prog.opts.LogLevel = "bogus"

	err = prog.validateOpts()
	require.ErrorIs(t, err, errArgInvalidLogLevel)
}

func TestValidateOpts_MissingMode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil, nil, nil)
	prog.opts = &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgModeMismatch)
}

func TestValidateOpts_SameMirrorAndTarget_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil, nil, nil)
	prog.opts = &programOptions{
		Mode:       "move",
		MirrorRoot: "/same",
		RealRoot:   "/same",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgMirrorTargetSame)
}

func TestValidateOpts_RelativePaths_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil, nil, nil)
	prog.opts = &programOptions{
		Mode:       "move",
		MirrorRoot: "relative/path",
		RealRoot:   "/real",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
}

func TestPrintOpts_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout bytes.Buffer

	prog := setupTestProgram(fs, nil, &stdout, nil)
	prog.opts = &programOptions{
		Mode:       "init",
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		Direct:     true,
	}

	err := prog.printOpts()
	require.NoError(t, err)
	output := stdout.String()

	require.Contains(t, output, "mirror: /mirror")
	require.Contains(t, output, "target: /real")
	require.Contains(t, output, "direct: true")
}
