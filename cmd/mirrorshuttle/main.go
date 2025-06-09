/*
mirrorshuttle provides a command-line interface for replicating the full
directory structure of a target location into a sandbox or staging area. Content
can be added to this mirror structure, without exposing the secure target, but
at the benefit of having its entire directory structure available for organizing.

Later, mirrorshuttle moves new content back into the original, secured
structure, while preserving the directory structure as organized in the staging
area. This workflow allows content to be prepared in a public writable
environment, then securely promoted to its protected final destination, without
ever exposing that destination itself to public write access (and ransomware).

The tool operates in two distinct operational modes, `init` and `move`:

  - `init` creates a mirror of the target directory's structure inside a sandbox
    or staging area. It excludes any paths marked with `--exclude` or otherwise
    specified in a configuration file. This is useful for preparing files in a
    public or temporary environment while using the layout of the secure
    destination. The mirror directory can even be a subdirectory of the target
    directory itself, in which case it will be excluded from the mirror process.

  - `move` transfers files that were added to the mirror back into the original
    target directory, preserving the folder structure. It ensures file integrity
    using BLAKE3 checksums and, when possible, uses atomic renames for
    efficiency. If a direct rename isn’t possible (e.g., across filesystems), it
    falls back to a safe copy-and-remove strategy.

# FEATURES

  - Clean CLI and YAML configuration support.
  - Optional dry-run mode for safe previews.
  - Atomic file operations when possible.
  - Safe fallback to copy-and-remove across filesystems.
  - Checksum validation using BLAKE3 to ensure in-memory/file integrity.
  - Exclude rules for omitting specific absolute paths from either mode.
  - Fails early on misconfiguration or unsafe directory states.

# INSTALLATION

To build from source, a Makefile is included with the project's source code.
Running `make all` will compile the application and pull in any necessary
dependencies. `make check` runs the test suite and static analysis tools.

For convenience, precompiled static binaries for common architectures are
released through GitHub. These can be installed into `/usr/bin/` or respective
system locations; ensure they are executable by running `chmod +x` before use.

# USAGE

	mirrorshuttle --mode=init|move --mirror=ABSPATH --target=ABSPATH [flags]

# ARGUMENTS

	--mode string
		Required. Must be either "init" or "move".

		In `--mode=init` the `--mirror` folder must not contain any files, as
		it will be removed and re-created with the latest structure. If any
		files are detected, the operation will fail with a specific return code.

	--config string
		Optional. Path to a YAML configuration file with any CLI arguments.
		Exception: `--mode` argument must always be specified via command-line.
		Direct CLI arguments always override values set via configuration file.

	--mirror string
		Required. Absolute path to the mirror structure. This is where mirrored
		directories will be created and from where files will be moved. It can
		be a subfolder of `--target`, and will be excluded from being mirrored.

	--target string
		Required. Absolute path to the real (target) structure. This is the
		source of truth in init mode and the destination in move mode.

	--exclude string
		Optional. Absolute path to exclude from operations. Can be repeated.
		This prevents specified directories from being mirrored or moved.

	--direct
		Optional. Attempt atomic rename operations. If this fails (e.g., across
		filesystems), fallback to copy and remove.

		In union filesystems, this may result in allocation or disk-relocation
		methods being circumvented and files staying on the same disk despite
		that possibly not being wanted. Disable this setting for such use cases.

		Default: false

	--verify
		Optional. Re-read the target file again after moving and verify against
		a previously calculated (source file) hash, ensuring target was written
		to disk without corruption. Requires a full re-read of the target file.

		Default: false

	--skip-failed
		Optional. Do not exit on non-fatal failures, skip the failed element
		and proceed instead; returns with a partial failure return code.

		Default: false

	--dry-run
		Optional. Perform a preview of operations, without filesystem changes.
		Useful for verifying behavior before execution.

		Default: false

# YAML CONFIGURATION EXAMPLE

	mirror: /mirror/path
	target: /real/path
	exclude:
	  - /real/path/skip-this
	  - /real/path/temp
	direct: true
	verify: false
	skip-failed: false
	dry-run: false

Invalid configurations (unknown or malformed fields) are rejected at runtime.

# RETURN CODES

  - `0`: Success
  - `1`: Failure
  - `2`: Partial Failure (with `--skip-failed`)
  - `3`: Mirror folder contains unmoved files (with `--mode=init`)
  - `4`: Unmoved files due to conflicting target files (with `--mode=move`)
  - `5`: Invalid command-line arguments and/or configuration file provided

# IMPLEMENTATION

An example implementation could be a RAID system that has all user "shares"
inside `/mnt/user`, but only `/mnt/user/incoming` writable from the outside
world (e.g., via Samba). The other folders of `/mnt/user` are read-only to the
outside world and are themselves readable data archives that do not change.

The user wants to prepare data within the `/mnt/user/incoming` structure only,
but also organize where it will end up in the protected archival structures
eventually, so they run the following initial command:

	mirrorshuttle --mode=init --mirror=/mnt/user/incoming --target=/mnt/user

The above command mirrors the `/mnt/user` structure into their staging location.
New content is added there daily, and so a periodic cron job is set up to run:

	mirrorshuttle --mode=move --mirror=/mnt/user/incoming --target=/mnt/user

Whenever the cron job runs, any new content is moved to the respective location.

If the `--target` location never changes outside of mirrorshuttle's operation,
normally no `--mode=init` would need to be run again (after the first time).

But, the user does an occasional cleanup within their archival site directly and
hence runs the initialization command (again) after finishing their cleanup:

	mirrorshuttle --mode=init --mirror=/mnt/user/incoming --target=/mnt/user

They could also run this command as part of their cron job, after the respective
`--mode=move` operation, ensuring that their mirror folder is always up to date.

They understand that if folders were removed in the `--target` structure,
and `--mode=init` was not run again before the next `--mode=move`, any removed
folders would be re-created. This is why `--target` locations should remain
static and not be modified without a follow-up re-running of `--mode=init`.

# DESIGN CHOICES AND LIMITATIONS

mirrorshuttle assumes the `--target` location to be relatively static, in which
case `--mode=init` calls should not need to be frequent (if at all). If the
target structure changes outside of mirrorshuttle's operation, `--mode=init` can
mirror again any new structural changes, but will need the `--mirror` folder to
not contain unmoved files, otherwise requiring manual resolution by the user.

The program is built to automate workflows as much as possible - without
compromising safety. If it cannot proceed safely, it will fail early with clear,
descriptive error messages, leaving any inconsistent directory states for the
user to inspect and resolve. This is a deliberate design decision to avoid
making assumptions about the user's data. The tool only performs operations that
are explicitly safe and in a known-consistent state. As a result, even minor
issues can cause the process to halt, but this behavior ensures users retain
full control over the outcome and can take corrective action with confidence.

The program is intentionally designed not to be run as root. All operations are
expected to be performed under a regular user account. When moving files back
into the target structure, ownership of those files will reflect the user
executing the tool. Additionally, file and directory permissions are created
respecting the environment's current `umask`, ensuring predictable behavior
across environments without requiring privileged access.

All non-routine messages - including warnings, errors, and anything requiring
attention - are written to standard error (`stderr`). All routine operational
output is written to standard output (`stdout`).

# POSSIBLE USE CASES IN PRODUCTION

mirrorshuttle is well-suited for system automation, secure file transfers, and
complex filesystem migration tasks. While it can be executed directly from the
command line interface (CLI), it is often most effective when integrated into
shell scripts or scheduled with cron jobs.

Always use with caution and ensure you fully understand the behavior of its
operational modes before deploying in a production environment.

# SECURITY, CONTRIBUTIONS AND LICENSING

Please report any issues via the GitHub Issues tracker. While no major features
are currently planned, contributions are welcome. Contributions should be
submitted through GitHub and, if possible, should pass the test suite and comply
with the project's linting rules. All code is licensed under the GPLv2 license.
*/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/spf13/afero"
)

const (
	exitCodeSuccess        = 0
	exitCodeFailure        = 1
	exitCodePartialFailure = 2
	exitCodeMirrNotEmpty   = 3
	exitCodeUnmovedFiles   = 4
	exitCodeConfigFailure  = 5

	dirBasePerm = 0o777
	exitTimeout = 60 * time.Second
)

var (
	// Version is the application's version (filled in during compilation).
	Version string

	errArgConfigMalformed     = errors.New("--config yaml file is malformed")
	errArgConfigMissing       = errors.New("--config yaml file does not exist")
	errArgExcludePathNotAbs   = errors.New("--exclude paths must all be absolute")
	errArgMirrorTargetNotAbs  = errors.New("--mirror and --target paths must all be absolute")
	errArgMirrorTargetSame    = errors.New("--mirror and --target paths cannot be the same")
	errArgMissingMirrorTarget = errors.New("--mirror and --target paths must both be set")
	errArgModeMismatch        = errors.New("--mode must either be 'init' or 'move'")

	errMemoryHashMismatch   = errors.New("in-memory hash mismatch; possible corruption during in-memory I/O")
	errVerifyHashMismatch   = errors.New("--verify pass hash mismatch; possible corruption during disk-write I/O")
	errMirrorNotEmpty       = errors.New("--mirror contains files; run with --mode=move to relocate them, or remove the files manually")
	errMirrorNotExist       = errors.New("--mirror does not exist; have nowhere to move from")
	errTargetNotExist       = errors.New("--target does not exist; have nowhere to mirror from or move to")
	errMirrorParentNotExist = errors.New("--mirror parent does not exist; cannot create mirror inside it")
	errMirrorParentNotDir   = errors.New("--mirror parent is not a directory; cannot create mirror inside it")
)

type program struct {
	fsys     afero.Fs
	stdout   io.Writer
	stderr   io.Writer
	testMode bool
	opts     *programOptions
	flags    *flag.FlagSet

	hasUnmovedFiles    bool
	hasPartialFailures bool
}

type programOptions struct {
	Mode       string     `yaml:"-"`
	MirrorRoot string     `yaml:"mirror"`
	RealRoot   string     `yaml:"target"`
	Excludes   excludeArg `yaml:"exclude"`
	Direct     bool       `yaml:"direct"`
	Verify     bool       `yaml:"verify"`
	SkipFailed bool       `yaml:"skip-failed"`
	DryRun     bool       `yaml:"dry-run"`
}

func main() {
	var exitCode int

	defer func() {
		os.Exit(exitCode)
	}()

	fmt.Fprintf(os.Stdout, "MirrorShuttle (v%s) - Keep your organization, ditch the ransomware.\n", Version)
	fmt.Fprintf(os.Stdout, "(c) 2025 - desertwitch (Rysz) / License: GNU General Public License v2\n\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	doneChan := make(chan int, 1)

	prog, err := newProgram(os.Args, afero.NewOsFs(), os.Stdout, os.Stderr, false)
	if prog == nil || err != nil {
		exitCode = exitCodeConfigFailure

		return
	}

	go func() {
		exitCode, _ := prog.run(ctx)
		doneChan <- exitCode
	}()

	select {
	case code := <-doneChan:
		exitCode = code

		return

	case <-sigChan:
		fmt.Fprintln(os.Stderr, "warning: received interrupt signal; shutting down (waiting up to 60s)...")
		cancel()

		select {
		case code := <-doneChan:
			exitCode = code

			return

		case <-time.After(exitTimeout):
			exitCode = exitCodeFailure
			fmt.Fprintln(os.Stderr, "fatal: timed out while waiting for program exit; killing...")

			return
		}
	}
}

func newProgram(cliArgs []string, fsys afero.Fs, stdout io.Writer, stderr io.Writer, testMode bool) (*program, error) {
	prog := &program{
		fsys:     fsys,
		stdout:   stdout,
		stderr:   stderr,
		opts:     &programOptions{},
		testMode: testMode,
	}

	if err := prog.parseArgs(cliArgs); err != nil {
		fmt.Fprintf(prog.stderr, "fatal: failed to parse configuration: %v\n\n", err)
		prog.flags.Usage()

		return nil, fmt.Errorf("failed to parse configuration: %w", err)
	}

	if err := prog.validateOpts(); err != nil {
		fmt.Fprintf(prog.stderr, "fatal: failed to validate configuration: %v\n\n", err)
		prog.flags.Usage()

		return nil, fmt.Errorf("failed to validate configuration: %w", err)
	}

	if err := prog.printOpts(); err != nil {
		fmt.Fprintf(prog.stderr, "fatal: failed to print configuration: %v\n\n", err)
		prog.flags.Usage()

		return nil, fmt.Errorf("failed to print configuration: %w", err)
	}

	return prog, nil
}

func (prog *program) run(ctx context.Context) (retExitCode int, retError error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(prog.stderr, "panic recovered: %v\n", r)
			debug.PrintStack()
			retExitCode = exitCodeFailure
		}
	}()

	if prog.opts.DryRun {
		fmt.Fprintln(prog.stderr, "warning: running in dry mode (no changes will be made)")
	}

	switch prog.opts.Mode {
	case "init":
		fmt.Fprintln(prog.stdout, "setting up the mirror structure...")

		if err := prog.createMirrorStructure(ctx); err != nil {
			if errors.Is(err, errMirrorNotEmpty) {
				fmt.Fprintf(prog.stderr, "fatal: failed creating mirror structure: %v\n", err)

				return exitCodeMirrNotEmpty, fmt.Errorf("failed creating mirror structure: %w", err)
			}

			if !errors.Is(err, context.Canceled) {
				fmt.Fprintf(prog.stderr, "fatal: failed creating mirror structure: %v\n", err)
			}

			return exitCodeFailure, fmt.Errorf("failed creating mirror structure: %w", err)
		}

	case "move":
		fmt.Fprintln(prog.stdout, "moving from mirror to real structure...")

		if err := prog.moveFiles(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				fmt.Fprintf(prog.stderr, "fatal: failed moving to real structure: %v\n", err)
			}

			return exitCodeFailure, fmt.Errorf("failed moving to real structure: %w", err)
		}
	}

	if prog.hasPartialFailures {
		fmt.Fprintln(prog.stderr, "warning: mode has completed, but with partial failures; exiting...")

		return exitCodePartialFailure, nil
	}

	if prog.hasUnmovedFiles {
		fmt.Fprintln(prog.stderr, "warning: mode has completed, but with unmoved files; exiting...")

		return exitCodeUnmovedFiles, nil
	}

	fmt.Fprintln(prog.stdout, "success: mode has completed; exiting...")

	return exitCodeSuccess, nil
}
