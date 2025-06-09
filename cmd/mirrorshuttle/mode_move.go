package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/zeebo/blake3"
)

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
			fmt.Fprintf(prog.stderr, "skipped: %q (src is among excluded)\n", path)

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

		if isExcluded(movePath, prog.opts.Excludes) {
			fmt.Fprintf(prog.stderr, "skipped: %q (dst is among excluded)\n", movePath)

			return nil
		}

		if e.IsDir() {
			if _, err := prog.fsys.Stat(movePath); errors.Is(err, os.ErrNotExist) {
				if prog.opts.DryRun {
					fmt.Fprintf(prog.stdout, "dry: create: %q\n", movePath)
				} else {
					if err := prog.fsys.Mkdir(movePath, dirBasePerm); err != nil {
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
			prog.hasUnmovedFiles = true
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

func (prog *program) copyAndRemove(src string, dst string) (retErr error) {
	var inputClosed, outputClosed, verifierClosed, dstWritten bool

	in, err := prog.fsys.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open src: %q (%w)", src, err)
	}
	defer func() {
		if !inputClosed {
			in.Close()
		}
	}()

	tmp := dst + ".mirsht"

	out, err := prog.fsys.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to open tmp: %q (%w)", tmp, err)
	}
	defer func() {
		if !outputClosed {
			out.Close()
		}
	}()

	defer func() {
		if retErr != nil { //nolint:nestif
			if _, err := prog.fsys.Stat(src); err == nil {
				if !dstWritten {
					_ = prog.fsys.Remove(tmp)
				} else {
					_ = prog.fsys.Remove(dst)
				}
			} else if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(prog.stderr, "cleanup: not found: %q\n", src)
				if !dstWritten {
					fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", tmp)
				} else {
					fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", dst)
				}
			} else {
				fmt.Fprintf(prog.stderr, "cleanup: failed to stat: %s (%v)\n", src, err)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", src)
				if !dstWritten {
					fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", tmp)
				} else {
					fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", dst)
				}
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
		return fmt.Errorf("failed to close tmp: %q (%w)", tmp, err)
	}
	outputClosed = true

	srcChecksum := hex.EncodeToString(srcHasher.Sum(nil))
	dstChecksum := hex.EncodeToString(dstHasher.Sum(nil))

	if srcChecksum != dstChecksum {
		return fmt.Errorf("%w: %q (%s) != %q (%s)", errMemoryHashMismatch, src, srcChecksum, tmp, dstChecksum)
	}

	if err := prog.fsys.Rename(tmp, dst); err != nil {
		return fmt.Errorf("failed to rename tmp to dst: %q -x-> %q (%w)", tmp, dst, err)
	}
	dstWritten = true

	if prog.opts.Verify {
		verifyHasher := blake3.New()

		verifier, err := prog.fsys.Open(dst)
		if err != nil {
			return fmt.Errorf("failed to re-open dst for --verify pass: %q (%w)", dst, err)
		}
		defer func() {
			if !verifierClosed {
				verifier.Close()
			}
		}()

		if _, err := io.Copy(verifyHasher, verifier); err != nil {
			return fmt.Errorf("failed to re-read dst for --verify pass: %q (%w)", dst, err)
		}

		if err := verifier.Close(); err != nil {
			return fmt.Errorf("failed to close dst after --verify pass: %q (%w)", dst, err)
		}
		verifierClosed = true

		verifyChecksum := hex.EncodeToString(verifyHasher.Sum(nil))
		if verifyChecksum != srcChecksum {
			return fmt.Errorf("%w: %q (%s) != %q (%s)", errVerifyHashMismatch, src, srcChecksum, dst, verifyChecksum)
		}
	}

	if err := prog.fsys.Remove(src); err != nil {
		return fmt.Errorf("failed to remove src (after move): %q (%w)", src, err)
	}

	return nil
}
