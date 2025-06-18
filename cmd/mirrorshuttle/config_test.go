package main

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Expectation: The function sets all non-provided arguments to their defaults.
func Test_Unit_ParseArgs_Unset_Defaults_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	var stdout, stderr bytes.Buffer

	args := []string{
		"program",
		"--mode=init",
		"--mirror=/mirror",
		"--target=/real",
	}

	prog, err := newProgram(args, fs, &stdout, &stderr)
	require.NoError(t, err)
	require.NotNil(t, prog)

	err = prog.parseArgs(args)
	require.NoError(t, err)

	require.Equal(t, "init", prog.opts.Mode)
	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)
	require.Empty(t, prog.opts.Excludes)
	require.False(t, prog.opts.Direct)
	require.False(t, prog.opts.Verify)
	require.False(t, prog.opts.SkipEmpty)
	require.False(t, prog.opts.RemoveEmpty)
	require.False(t, prog.opts.SkipFailed)
	require.False(t, prog.opts.DryRun)
	require.False(t, prog.opts.SlowMode)
	require.Equal(t, defaultInitDepth, prog.opts.InitDepth)
	require.False(t, prog.opts.JSON)
	require.Equal(t, "info", prog.opts.LogLevel)
}

// Expectation: The function can parse all known arguments to their non-defaults.
func Test_Unit_ParseArgs_All_Success(t *testing.T) {
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
		"--slow-mode",
		"--init-depth=5",
		"--skip-empty",
		"--remove-empty",
		"--skip-failed",
		"--json",
		"--log-level=warn",
	}

	prog, err := newProgram(args, fs, &stdout, &stderr)
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
	require.True(t, prog.opts.SkipEmpty)
	require.True(t, prog.opts.RemoveEmpty)
	require.True(t, prog.opts.SkipFailed)
	require.True(t, prog.opts.DryRun)
	require.True(t, prog.opts.SlowMode)
	require.Equal(t, 5, prog.opts.InitDepth)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

// Expectation: The function can parse all known YAML arguments to their non-defaults.
func Test_Unit_ParseArgs_ConfigFile_All_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	yamlContent := `
mirror: /mirror
target: /real
exclude:
  - /exclude
direct: true
verify: true
dry-run: true
slow-mode: true
init-depth: 5
skip-empty: true
remove-empty: true
skip-failed: true
log-level: warn
json: true
`
	err := afero.WriteFile(fs, "/config.yaml", []byte(yamlContent), 0o644)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	args := []string{"program", "--mode=move", "--config=/config.yaml"}

	prog, err := newProgram(args, fs, &stdout, &stderr)
	require.NoError(t, err)
	require.NotNil(t, prog)

	err = prog.parseArgs(args)
	require.NoError(t, err)

	require.Equal(t, "move", prog.opts.Mode)
	require.Equal(t, "/mirror", prog.opts.MirrorRoot)
	require.Equal(t, "/real", prog.opts.RealRoot)
	require.Equal(t, "/exclude", prog.opts.Excludes[0])
	require.True(t, prog.opts.Direct)
	require.True(t, prog.opts.Verify)
	require.True(t, prog.opts.SkipEmpty)
	require.True(t, prog.opts.RemoveEmpty)
	require.True(t, prog.opts.SkipFailed)
	require.True(t, prog.opts.DryRun)
	require.True(t, prog.opts.SlowMode)
	require.Equal(t, 5, prog.opts.InitDepth)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

// Expectation: The function can override all known YAML arguments from the CLI.
func Test_Unit_ParseArgs_ConfigFileOverride_All_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	yamlContent := `
mirror: /mirror2
target: /real2
exclude:
  - /exclude2
direct: false
verify: false
dry-run: false
slow-mode: false
init-depth: 3
skip-empty: false
remove-empty: false
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
		"--exclude=/exclude",
		"--direct",
		"--verify",
		"--slow-mode",
		"--init-depth=5",
		"--dry-run",
		"--skip-empty",
		"--remove-empty",
		"--skip-failed",
		"--json",
		"--log-level=warn",
	}

	prog, err := newProgram(args, fs, &stdout, &stderr)
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
	require.True(t, prog.opts.SkipEmpty)
	require.True(t, prog.opts.RemoveEmpty)
	require.True(t, prog.opts.SkipFailed)
	require.True(t, prog.opts.DryRun)
	require.True(t, prog.opts.SlowMode)
	require.Equal(t, 5, prog.opts.InitDepth)
	require.True(t, prog.opts.JSON)
	require.Equal(t, "warn", prog.opts.LogLevel)
}

// Expectation: The function validates known to be correct options.
func Test_Unit_ValidateOpts_ValidOptions_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	prog.opts = &programOptions{
		Mode:        "init",
		MirrorRoot:  "/mirror",
		RealRoot:    "/real",
		Excludes:    []string{"/exclude", "/exclude2"},
		Direct:      true,
		Verify:      true,
		SkipEmpty:   true,
		RemoveEmpty: true,
		SkipFailed:  true,
		DryRun:      true,
		LogLevel:    "warn",
		JSON:        true,
	}

	err := prog.validateOpts()
	require.NoError(t, err)
}

// Expectation: The function rejects an invalid log level among otherwise valid options.
func Test_Unit_ValidateOpts_InvalidLogLevel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	prog.opts = &programOptions{
		Mode:        "init",
		MirrorRoot:  "/mirror",
		RealRoot:    "/real",
		Excludes:    []string{"/exclude", "/exclude2"},
		Direct:      true,
		Verify:      true,
		SkipEmpty:   true,
		RemoveEmpty: true,
		SkipFailed:  true,
		DryRun:      true,
		LogLevel:    "warnx",
		JSON:        true,
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgInvalidLogLevel)
}

// Expectation: The function rejects a missing mode option.
func Test_Unit_ValidateOpts_MissingMode_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	prog.opts = &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgModeMismatch)
}

// Expectation: The function rejects an equal mirror and target.
func Test_Unit_ValidateOpts_SameMirrorAndTarget_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	prog.opts = &programOptions{
		Mode:       "move",
		MirrorRoot: "/same",
		RealRoot:   "/same",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgMirrorTargetSame)
}

// Expectation: The function rejects a relative mirror path.
func Test_Unit_ValidateOpts_RelativeMirrorPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	prog.opts = &programOptions{
		Mode:       "move",
		MirrorRoot: "relative/path",
		RealRoot:   "/real",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
}

// Expectation: The function rejects a relative target path.
func Test_Unit_ValidateOpts_RelativeTargetPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	prog.opts = &programOptions{
		Mode:       "move",
		MirrorRoot: "/mirror",
		RealRoot:   "relative/path",
	}

	err := prog.validateOpts()
	require.ErrorIs(t, err, errArgMirrorTargetNotAbs)
}

// Expectation: The function prints the configuration to standard output.
func Test_Unit_PrintOpts_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, stdout, _ := setupTestProgram(fs, nil)
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
