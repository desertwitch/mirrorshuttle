package main

import (
	"flag"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"gopkg.in/yaml.v3"
)

func (prog *program) parseArgs(cliArgs []string) error {
	var (
		yamlFile string
		yamlOpts programOptions
	)

	prog.flags = flag.NewFlagSet("mirrorshuttle", flag.ExitOnError)
	prog.flags.SetOutput(prog.stderr)
	prog.flags.Usage = func() {
		fmt.Fprintf(prog.stderr, "usage: %q --mode=init|move --mirror=ABSPATH --target=ABSPATH\n", cliArgs[0])
		fmt.Fprintf(prog.stderr, "\t[--exclude=ABSPATH] [--exclude=ABSPATH] [--direct] [--verify] [--skip-failed]\n")
		fmt.Fprintf(prog.stderr, "\t[--slow-mode] [--dry-run] [--log-level=debug|info|warn|error] [--json]\n\n")
		prog.flags.PrintDefaults()
	}

	prog.flags.StringVar(&prog.opts.Mode, "mode", "", "operation mode: 'init' or 'move'; always needed")
	prog.flags.StringVar(&yamlFile, "config", "", "path to a yaml configuration file; used with the specified mode")
	prog.flags.StringVar(&prog.opts.MirrorRoot, "mirror", "", "absolute path to the mirror structure to create; files will be moved *from* here")
	prog.flags.StringVar(&prog.opts.RealRoot, "target", "", "absolute path to the real structure to mirror; files will be moved *to* here")
	prog.flags.Var(&prog.opts.Excludes, "exclude", "absolute path to exclude; can be repeated multiple times")
	prog.flags.BoolVar(&prog.opts.Direct, "direct", false, "use atomic rename when possible; fallback to copy and remove if it fails or crosses filesystems")
	prog.flags.BoolVar(&prog.opts.Verify, "verify", false, "verify again the hash of a target file after moving it; requires an extra full read of the file")
	prog.flags.BoolVar(&prog.opts.SkipFailed, "skip-failed", false, "do not exit on non-fatal failures; skip failed element and proceed instead")
	prog.flags.BoolVar(&prog.opts.SlowMode, "slow-mode", false, "adds 250ms timeout after directory creations in --mode=init; avoids thrashing filesystem")
	prog.flags.BoolVar(&prog.opts.DryRun, "dry-run", false, "preview only; no changes are written to disk")
	prog.flags.StringVar(&prog.opts.LogLevel, "log-level", "info", "decides the verbosity of emitted logs; debug, info, warn, error")
	prog.flags.BoolVar(&prog.opts.JSON, "json", false, "output all emitted logs in the JSON format; results can be read from stderr")

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
			// Since we established no excludes were given, easier to just append to nil-slice
			prog.opts.Excludes = append(prog.opts.Excludes, filepath.Clean(strings.TrimSpace(p)))
		}
	}
	if !setFlags["direct"] {
		prog.opts.Direct = yamlOpts.Direct
	}
	if !setFlags["verify"] {
		prog.opts.Verify = yamlOpts.Verify
	}
	if !setFlags["skip-failed"] {
		prog.opts.SkipFailed = yamlOpts.SkipFailed
	}
	if !setFlags["slow-mode"] {
		prog.opts.SlowMode = yamlOpts.SlowMode
	}
	if !setFlags["dry-run"] {
		prog.opts.DryRun = yamlOpts.DryRun
	}
	if !setFlags["log-level"] {
		prog.opts.LogLevel = yamlOpts.LogLevel
	}
	if !setFlags["json"] {
		prog.opts.JSON = yamlOpts.JSON
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

	if prog.opts.LogLevel != "" {
		if _, err := parseLogLevel(prog.opts.LogLevel); err != nil {
			return fmt.Errorf("%w: %q", err, prog.opts.LogLevel)
		}
	} else {
		prog.opts.LogLevel = strings.ToLower(defaultLogLevel.String())
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

func (prog *program) logHandler() slog.Handler {
	var logHandler slog.Handler
	var logLevel slog.Level

	logLevel, _ = parseLogLevel(prog.opts.LogLevel)

	if prog.opts.JSON {
		logHandler = slog.NewJSONHandler(prog.stderr, &slog.HandlerOptions{
			Level: logLevel,
		})
	} else {
		logHandler = tint.NewHandler(prog.stderr,
			&tint.Options{
				Level:      logLevel,
				TimeFormat: time.TimeOnly,
			})
	}

	return logHandler
}
