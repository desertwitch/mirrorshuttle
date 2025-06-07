/*
mirrorshuttle provides a command-line interface for replicating the full
directory structure of a target location into a sandbox or staging area. Content
can be added to this mirror structure, without exposing the secure target, but
at the benefit of having its entire directory structure available for organizing.

Later, mirrorshuttle moves new content back into the original, secured
structure, while preserving the directory structure as organized in the staging
area. This workflow allows content to be prepared in a public writable
environment, then securely promoted to its protected final destination, without
ever exposing that destination itself to public write access.

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

FEATURES:

  - Clean CLI and YAML configuration support.
  - Optional dry-run mode for safe previews.
  - Atomic file operations when possible.
  - Safe fallback to copy-and-remove across filesystems.
  - Checksum validation using BLAKE3 to ensure in-memory integrity.
  - Exclude rules for omitting specific absolute paths from either mode.
  - Fails early on misconfiguration or unsafe directory states.

USAGE:

	mirrorshuttle --mode=init|move --mirror=ABSPATH --target=ABSPATH [flags]

ARGUMENTS:

	--mode string
		Required. Must be either "init" or "move".

		In `--mode=init` the `--mirror` folder must not contain any files, as
		it will be removed and re-created with the latest structure. If any
		files are detected, the operation will fail with a specific return code.

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

	--skip-failed
		Optional. Do not exit on non-fatal failures, skip the failed element
		and proceed instead; returns with a partial failure return code.

		Default: false

	--dry-run
		Optional. Perform a preview of operations, without filesystem changes.
		Useful for verifying behavior before execution.

		Default: false

	--config string
		Path to a YAML configuration file specifying the same field names.
		CLI flags always override any values set in the configuration file.
		Exception: `--mode` argument must always be specified via command-line.

YAML Configuration Example:

	mirror: /mirror/path
	target: /real/path
	exclude:
	  - /real/path/skip-this
	  - /real/path/temp
	direct: true
	skip-failed: false
	dry-run: false

Invalid configurations (unknown or malformed fields) are rejected at runtime.

RETURN CODES:

  - `0`: Success
  - `1`: Failure
  - `2`: Partial Failure (with `--skip-failed`)
  - `3`: Mirror folder contains unmoved files (with `--mode=init`)
  - `4`: Invalid command-line arguments and/or configuration file provided

IMPLEMENTATION:

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

DESIGN CHOICES AND LIMITATIONS:

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

Any important information is written to standard error (stderr), while verbose
operational information is written to standard output (stdout).

PRODUCTION USE CASES:

mirrorshuttle is ideal for use in system automation, secure moving, or complex
filesystem migration scenarios. Always use it with caution and ensure you
understand the behavior of the operational modes before deploying in production.
*/
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/afero"
	"github.com/zeebo/blake3"
	"gopkg.in/yaml.v3"
)

const (
	exitCodeSuccess        = 0
	exitCodeFailure        = 1
	exitCodePartialFailure = 2
	exitCodeModeFailure    = 3
	exitCodeArgFailure     = 4

	dirBasePerm  = 0o777
	fileBasePerm = 0o666

	exitTimeout = 60 * time.Second
)

var (
	// Version is the application's version (filled in during compilation).
	Version string

	partialFailure bool

	errArgConfigMalformed     = errors.New("--config yaml file is malformed")
	errArgConfigMissing       = errors.New("--config yaml file does not exist")
	errArgExcludePathNotAbs   = errors.New("--exclude paths must all be absolute")
	errArgMirrorTargetNotAbs  = errors.New("--mirror and --target paths must all be absolute")
	errArgMirrorTargetSame    = errors.New("--mirror and --target paths cannot be the same")
	errArgMissingMirrorTarget = errors.New("--mirror and --target paths must both be set")
	errArgModeMismatch        = errors.New("--mode must either be 'init' or 'move'")

	errHashMismatch   = errors.New("in-memory hash mismatch during I/O")
	errMirrorNotEmpty = errors.New("--mirror contains files; run with --mode=move to relocate them, or remove the files manually")
	errMirrorNotExist = errors.New("--mirror does not exist; have nowhere to move from")
	errTargetNotExist = errors.New("--target does not exist; have nowhere to mirror from or move to")
)

type program struct {
	fsys     afero.Fs
	stdout   io.Writer
	stderr   io.Writer
	testMode bool
	opts     *programOptions
	flags    *flag.FlagSet
}

type programOptions struct {
	Mode       string     `yaml:"-"`
	MirrorRoot string     `yaml:"mirror"`
	RealRoot   string     `yaml:"target"`
	Excludes   excludeArg `yaml:"exclude"`
	Direct     bool       `yaml:"direct"`
	SkipFailed bool       `yaml:"skip-failed"`
	DryRun     bool       `yaml:"dry-run"`
}

type excludeArg []string

func (s *excludeArg) String() string {
	return fmt.Sprint(*s)
}

func (s *excludeArg) Set(value string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(value))

	*s = append(*s, cleanPath)

	return nil
}

func main() {
	var exitCode int

	defer func() {
		if partialFailure && exitCode == exitCodeSuccess {
			os.Exit(exitCodePartialFailure)
		}
		os.Exit(exitCode)
	}()

	fmt.Fprintf(os.Stderr, "MirrorShuttle (v%s) - Keep your structure, ditch the risk.\n", Version)
	fmt.Fprintf(os.Stderr, "(c) 2025 - desertwitch (Rysz) / GNU General Public License v2\n\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	doneChan := make(chan int, 1)

	prog, err := newProgram(os.Args, afero.NewOsFs(), os.Stdout, os.Stderr, false)
	if prog == nil || err != nil {
		exitCode = exitCodeArgFailure

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
		fmt.Fprintln(os.Stderr, "received interrupt signal; shutting down (waiting up to 60s)...")
		cancel()

		select {
		case code := <-doneChan:
			exitCode = code

			return

		case <-time.After(exitTimeout):
			exitCode = exitCodeFailure
			fmt.Fprintln(os.Stderr, "timed out while waiting for program exit; killing...")

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

func (prog *program) parseArgs(cliArgs []string) error {
	var (
		yamlFile string
		yamlOpts programOptions
	)

	if !prog.testMode {
		prog.flags = flag.NewFlagSet("prod", flag.ExitOnError)
	} else {
		prog.flags = flag.NewFlagSet("test", flag.ContinueOnError)
	}

	prog.flags.SetOutput(prog.stderr)
	prog.flags.Usage = func() {
		fmt.Fprintf(prog.stderr, "usage: %q --mode=init|move --mirror=ABSPATH --target=ABSPATH\n", cliArgs[0])
		fmt.Fprintf(prog.stderr, "\t\t[--exclude=ABSPATH] [--exclude=ABSPATH] [--direct] [--skip-failed] [--dry-run]\n\n")
		prog.flags.PrintDefaults()
	}

	prog.flags.StringVar(&prog.opts.Mode, "mode", "", "operation mode: 'init' or 'move'; always needed")
	prog.flags.StringVar(&yamlFile, "config", "", "absolute path to a yaml configuration file; used with the specified mode")
	prog.flags.StringVar(&prog.opts.MirrorRoot, "mirror", "", "absolute path to the mirror structure to create; files will be moved *from* here")
	prog.flags.StringVar(&prog.opts.RealRoot, "target", "", "absolute path to the real structure to mirror; files will be moved *to* here")
	prog.flags.Var(&prog.opts.Excludes, "exclude", "absolute path to exclude; can be repeated multiple times")
	prog.flags.BoolVar(&prog.opts.Direct, "direct", false, "use atomic rename when possible; fallback to copy and remove if it fails or crosses filesystems")
	prog.flags.BoolVar(&prog.opts.SkipFailed, "skip-failed", false, "do not exit on non-fatal failures; skip failed element and proceed instead")
	prog.flags.BoolVar(&prog.opts.DryRun, "dry-run", false, "preview only; no changes are written to disk")

	if err := prog.flags.Parse(cliArgs[1:]); err != nil {
		return fmt.Errorf("failed parsing flags: %w", err)
	}

	setFlags := make(map[string]bool)
	prog.flags.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	if yamlFile != "" {
		f, err := prog.fsys.Open(yamlFile)
		if err != nil {
			return fmt.Errorf("%w: %w", errArgConfigMissing, err)
		}
		defer f.Close()

		dec := yaml.NewDecoder(f)
		dec.KnownFields(true)

		if err := dec.Decode(&yamlOpts); err != nil {
			return fmt.Errorf("%w: %w", errArgConfigMalformed, err)
		}
	}

	if !setFlags["mirror"] {
		prog.opts.MirrorRoot = yamlOpts.MirrorRoot
	}
	if !setFlags["target"] {
		prog.opts.RealRoot = yamlOpts.RealRoot
	}
	if !setFlags["exclude"] {
		for _, p := range yamlOpts.Excludes {
			prog.opts.Excludes = append(prog.opts.Excludes, filepath.Clean(strings.TrimSpace(p))) // appends to nil
		}
	}
	if !setFlags["direct"] {
		prog.opts.Direct = yamlOpts.Direct
	}
	if !setFlags["skip-failed"] {
		prog.opts.SkipFailed = yamlOpts.SkipFailed
	}
	if !setFlags["dry-run"] {
		prog.opts.DryRun = yamlOpts.DryRun
	}

	return nil
}

func (prog *program) validateOpts() error {
	if prog.opts.Mode != "init" && prog.opts.Mode != "move" {
		return errArgModeMismatch
	}

	if prog.opts.MirrorRoot == "" || prog.opts.RealRoot == "" {
		return errArgMissingMirrorTarget
	}

	prog.opts.MirrorRoot = filepath.Clean(strings.TrimSpace(prog.opts.MirrorRoot))
	prog.opts.RealRoot = filepath.Clean(strings.TrimSpace(prog.opts.RealRoot))

	if prog.opts.MirrorRoot == prog.opts.RealRoot {
		return errArgMirrorTargetSame
	}

	if !filepath.IsAbs(prog.opts.MirrorRoot) || !filepath.IsAbs(prog.opts.RealRoot) {
		return errArgMirrorTargetNotAbs
	}

	if len(prog.opts.Excludes) > 0 {
		for _, p := range prog.opts.Excludes {
			if !filepath.IsAbs(p) {
				return fmt.Errorf("%w: %q", errArgExcludePathNotAbs, p)
			}
		}
	}

	return nil
}

func (prog *program) printOpts() error {
	out, err := yaml.Marshal(prog.opts)
	if err != nil {
		return fmt.Errorf("failed printing configuration: %w", err)
	}

	fmt.Fprintf(prog.stdout, "configuration for '--mode=%s':\n", prog.opts.Mode)

	lines := strings.SplitSeq(string(out), "\n")
	for line := range lines {
		if line != "" {
			fmt.Fprintf(prog.stdout, "\t%s\n", line)
		}
	}

	fmt.Fprintln(prog.stdout)

	return nil
}

func (prog *program) run(ctx context.Context) (int, error) {
	if prog.opts.DryRun {
		fmt.Fprintln(prog.stdout, "warning: running in dry mode (no changes will be made)")
	}

	switch prog.opts.Mode {
	case "init":
		fmt.Fprintln(prog.stdout, "setting up the mirror structure...")

		if err := prog.createMirrorStructure(ctx); err != nil {
			if errors.Is(err, errMirrorNotEmpty) {
				fmt.Fprintf(prog.stderr, "fatal: failed creating mirror structure: %v\n", err)

				return exitCodeModeFailure, fmt.Errorf("failed creating mirror structure: %w", err)
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

	fmt.Fprintln(prog.stdout, "success: mode has completed; exiting...")

	return exitCodeSuccess, nil
}

func (prog *program) walkError(err error) error {
	if prog.opts.SkipFailed {
		partialFailure = true
		fmt.Fprintf(prog.stderr, "skipped: %v\n", err)

		return nil
	}

	return err
}

func (prog *program) createMirrorStructure(ctx context.Context) error {
	if _, err := prog.fsys.Stat(prog.opts.RealRoot); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %q", errTargetNotExist, prog.opts.RealRoot)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.RealRoot, err)
	}

	if _, err := prog.fsys.Stat(prog.opts.MirrorRoot); err == nil {
		fmt.Fprintln(prog.stdout, "testing if the existing mirror structure is empty...")

		empty, err := prog.isEmptyStructure(ctx, prog.opts.MirrorRoot)
		if err != nil {
			return fmt.Errorf("failed checking for emptiness: %q (%w)", prog.opts.MirrorRoot, err)
		}
		if !empty {
			return errMirrorNotEmpty
		}

		if prog.opts.DryRun {
			fmt.Fprintf(prog.stdout, "dry: remove: %q\n", prog.opts.MirrorRoot)
		} else {
			if err := prog.fsys.RemoveAll(prog.opts.MirrorRoot); err != nil {
				return fmt.Errorf("failed to remove: %q (%w)", prog.opts.MirrorRoot, err)
			}
			fmt.Fprintf(prog.stdout, "removed: %q\n", prog.opts.MirrorRoot)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.MirrorRoot, err)
	}

	if prog.opts.DryRun {
		fmt.Fprintf(prog.stdout, "dry: create: %q\n", prog.opts.MirrorRoot)
	} else {
		if err := prog.fsys.MkdirAll(prog.opts.MirrorRoot, dirBasePerm); err != nil {
			return fmt.Errorf("failed to create: %q (%w)", prog.opts.MirrorRoot, err)
		}
		fmt.Fprintf(prog.stdout, "created: %q\n", prog.opts.MirrorRoot)
	}

	if err := afero.Walk(prog.fsys, prog.opts.RealRoot, func(path string, e os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("failed checking context: %w", err)
		}

		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(prog.stderr, "skipped: %q (no longer exists)\n", path)

				return nil
			}

			return prog.walkError(fmt.Errorf("failed to walk: %q (%w)", path, err))
		}

		if !e.IsDir() {
			return nil
		}

		if path == prog.opts.MirrorRoot {
			fmt.Fprintf(prog.stderr, "skipped: %q (is mirror root)\n", path)

			return filepath.SkipDir
		}

		if isExcluded(path, prog.opts.Excludes) {
			fmt.Fprintf(prog.stderr, "skipped: %q (is among excluded)\n", path)

			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(prog.opts.RealRoot, path)
		if err != nil {
			return prog.walkError(fmt.Errorf("failed to get relative path: %q (%w)", path, err))
		}

		mirrorPath := filepath.Join(prog.opts.MirrorRoot, relPath)

		if prog.opts.DryRun {
			fmt.Fprintf(prog.stdout, "dry: create: %q\n", mirrorPath)
		} else {
			if err := prog.fsys.MkdirAll(mirrorPath, dirBasePerm); err != nil {
				return prog.walkError(fmt.Errorf("failed to create: %q (%w)", mirrorPath, err))
			}
			fmt.Fprintf(prog.stdout, "created: %q\n", mirrorPath)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (prog *program) moveFiles(ctx context.Context) error {
	if _, err := prog.fsys.Stat(prog.opts.MirrorRoot); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %q", errMirrorNotExist, prog.opts.MirrorRoot)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.MirrorRoot, err)
	}

	if _, err := prog.fsys.Stat(prog.opts.RealRoot); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %q", errTargetNotExist, prog.opts.RealRoot)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.RealRoot, err)
	}

	if err := afero.Walk(prog.fsys, prog.opts.MirrorRoot, func(path string, e os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("failed checking context: %w", err)
		}

		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(prog.stderr, "skipped: %q (no longer exists)\n", path)

				return nil
			}

			return prog.walkError(fmt.Errorf("failed to walk: %q (%w)", path, err))
		}

		if isExcluded(path, prog.opts.Excludes) {
			fmt.Fprintf(prog.stderr, "skipped: %q (is among excluded)\n", path)

			return nil
		}

		relPath, err := filepath.Rel(prog.opts.MirrorRoot, path)
		if err != nil {
			return prog.walkError(fmt.Errorf("failed to get relative path: %q (%w)", path, err))
		}

		movePath := filepath.Join(prog.opts.RealRoot, relPath)

		if movePath == prog.opts.MirrorRoot {
			fmt.Fprintf(prog.stderr, "skipped: %q (cannot move from mirror into mirror)\n", path)

			return filepath.SkipDir
		}

		if e.IsDir() {
			if _, err := prog.fsys.Stat(movePath); errors.Is(err, os.ErrNotExist) {
				if prog.opts.DryRun {
					fmt.Fprintf(prog.stdout, "dry: create: %q\n", movePath)
				} else {
					if err := prog.fsys.MkdirAll(movePath, dirBasePerm); err != nil {
						return prog.walkError(fmt.Errorf("failed to create: %q (%w)", movePath, err))
					}
					fmt.Fprintf(prog.stdout, "created: %q\n", movePath)
				}
			} else if err != nil {
				return prog.walkError(fmt.Errorf("failed to stat: %q (%w)", movePath, err))
			}

			return nil
		}

		if _, err := prog.fsys.Stat(movePath); err == nil {
			fmt.Fprintf(prog.stderr, "exists: %q -x-> %q (not overwriting)\n", path, movePath)

			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return prog.walkError(fmt.Errorf("failed to stat: %q (%w)", movePath, err))
		}

		if prog.opts.DryRun {
			fmt.Fprintf(prog.stdout, "dry: move: %q -> %q\n", path, movePath)
		} else {
			if prog.opts.Direct {
				if err := prog.fsys.Rename(path, movePath); err == nil {
					fmt.Fprintf(prog.stdout, "moved: %q -> %q (direct)\n", path, movePath)

					return nil
				}
			}

			if err := prog.copyAndRemove(path, movePath); err != nil {
				return prog.walkError(fmt.Errorf("failed to move: %q -x-> %q (%w)", path, movePath, err))
			}

			fmt.Fprintf(prog.stdout, "moved: %q -> %q (c+r)\n", path, movePath)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (prog *program) isEmptyStructure(ctx context.Context, path string) (bool, error) {
	path = filepath.Clean(path)
	empty := true

	if err := afero.Walk(prog.fsys, path, func(subpath string, e os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("failed checking context: %w", err)
		}

		if err != nil {
			return fmt.Errorf("failed to walk: %q (%w)", subpath, err)
		}

		if !e.IsDir() {
			fmt.Fprintf(prog.stderr, "not-empty: %q", subpath)
			empty = false
		}

		return nil
	}); err != nil {
		return false, err
	}

	if !empty {
		return false, nil
	}

	return true, nil
}

//nolint:nonamedreturns
func (prog *program) copyAndRemove(src string, dst string) (retErr error) {
	var inputClosed, outputClosed bool

	in, err := prog.fsys.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open src: %q (%w)", src, err)
	}
	defer func() {
		if !inputClosed {
			in.Close()
		}
	}()

	out, err := prog.fsys.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, os.FileMode(fileBasePerm))
	if err != nil {
		return fmt.Errorf("failed to open dst: %q (%w)", dst, err)
	}
	defer func() {
		if !outputClosed {
			out.Close()
		}
	}()

	defer func() {
		if retErr != nil {
			if _, err := prog.fsys.Stat(src); err == nil {
				_ = prog.fsys.Remove(dst)
			} else if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(prog.stderr, "cleanup: not found: %q\n", src)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", dst)
			} else {
				fmt.Fprintf(prog.stderr, "cleanup: failed to stat: %s (%v)\n", src, err)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", src)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", dst)
			}
		}
	}()

	srcHasher := blake3.New()
	dstHasher := blake3.New()

	multiReader := io.TeeReader(in, srcHasher)
	multiWriter := io.MultiWriter(out, dstHasher)

	if _, err := io.Copy(multiWriter, multiReader); err != nil {
		return fmt.Errorf("failed during io: %w", err)
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("failed during sync: %w", err)
	}

	if err := in.Close(); err != nil {
		return fmt.Errorf("failed to close src: %q (%w)", src, err)
	}
	inputClosed = true

	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close dst: %q (%w)", dst, err)
	}
	outputClosed = true

	srcChecksum := hex.EncodeToString(srcHasher.Sum(nil))
	dstChecksum := hex.EncodeToString(dstHasher.Sum(nil))

	if srcChecksum != dstChecksum {
		return fmt.Errorf("%w: %q (%s) != %q (%s)", errHashMismatch, src, srcChecksum, dst, dstChecksum)
	}

	if _, err := prog.fsys.Stat(dst); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("dst does not exist (after move): %q (%w)", dst, err)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", dst, err)
	}

	if err := prog.fsys.Remove(src); err != nil {
		return fmt.Errorf("failed to remove src (after move): %q (%w)", src, err)
	}

	return nil
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
