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
	// The mirror root needs to exist, otherwise we have nowhere to move from.
	if _, err := prog.fsys.Stat(prog.opts.MirrorRoot); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %q", errMirrorNotExist, prog.opts.MirrorRoot)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.MirrorRoot, err)
	}

	// The target root needs to exist, otherwise we have nowhere to move to.
	if _, err := prog.fsys.Stat(prog.opts.RealRoot); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %q", errTargetNotExist, prog.opts.RealRoot)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.RealRoot, err)
	}

	// Walk the mirror root and move any contents that do not exist in the target root.
	if err := afero.Walk(prog.fsys, prog.opts.MirrorRoot, func(path string, e os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			// An interrupt was received, so we also interrupt the walk.
			return fmt.Errorf("failed checking context: %w", err)
		}

		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(prog.stderr, "skipped: %q (no longer exists)\n", path)

				// An element has disappeared during the walk, skip it.
				return nil
			}

			// Another failure has occurred during the walk (permissions, ...), handle it.
			return prog.walkError(fmt.Errorf("failed to walk: %q (%w)", path, err))
		}

		if isExcluded(path, prog.opts.Excludes) { // Check if the source path is excluded.
			fmt.Fprintf(prog.stderr, "skipped: %q (src is among excluded)\n", path)

			// The source path was among the user's excluded paths, skip it.
			return nil
		}

		// Construct the target path from the mirror's relative path.
		relPath, err := filepath.Rel(prog.opts.MirrorRoot, path)
		if err != nil {
			return prog.walkError(fmt.Errorf("failed to get relative path: %q (%w)", path, err))
		}
		movePath := filepath.Join(prog.opts.RealRoot, relPath)

		if movePath == prog.opts.MirrorRoot { // Check if target path is the mirror root.
			fmt.Fprintf(prog.stderr, "skipped: %q (cannot move from mirror into mirror)\n", path)

			// The target path is the mirror root, skip it (prevent insane recursion).
			return filepath.SkipDir
		}

		if isExcluded(movePath, prog.opts.Excludes) { // Check if the target path is excluded.
			fmt.Fprintf(prog.stderr, "skipped: %q (dst is among excluded)\n", movePath)

			// The target path was among the user's excluded paths, skip it.
			return nil
		}

		if e.IsDir() { // Handle directories.
			if _, err := prog.fsys.Stat(movePath); errors.Is(err, os.ErrNotExist) { // Check if the target directory exists.
				if prog.opts.DryRun {
					fmt.Fprintf(prog.stdout, "dry: create: %q\n", movePath)
				} else {
					// Create the target directory, if it does not exist.
					if err := prog.fsys.Mkdir(movePath, dirBasePerm); err != nil {
						return prog.walkError(fmt.Errorf("failed to create: %q (%w)", movePath, err))
					}
					fmt.Fprintf(prog.stdout, "created: %q\n", movePath)
				}
			} else if err != nil {
				return prog.walkError(fmt.Errorf("failed to stat: %q (%w)", movePath, err))
			}

			return nil
		} // Must be a file from here downwards.

		if _, err := prog.fsys.Stat(movePath); err == nil { // Check if the target file exists.
			prog.hasUnmovedFiles = true
			fmt.Fprintf(prog.stderr, "exists: %q -x-> %q (not overwriting)\n", path, movePath)

			// The target file exists; do not overwrite it, set unmoved files bit and skip it.
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return prog.walkError(fmt.Errorf("failed to stat: %q (%w)", movePath, err))
		}

		if prog.opts.DryRun {
			fmt.Fprintf(prog.stdout, "dry: move: %q -> %q\n", path, movePath)
		} else {
			if prog.opts.Direct {
				// Direct mode; attempt a rename syscall, otherwise copy and remove.
				if err := prog.fsys.Rename(path, movePath); err == nil {
					fmt.Fprintf(prog.stdout, "moved: %q -> %q (direct)\n", path, movePath)

					return nil
				} // Rename syscall must have failed from here downwards.
			}

			// Do the regular copy and remove operation and handle any failures.
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
	workingFile := dst + ".mirsht" // We work on a temporary file first.

	in, err := prog.fsys.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open: %q (%w)", src, err)
	}
	defer in.Close()

	out, err := prog.fsys.Create(workingFile)
	if err != nil {
		return fmt.Errorf("failed to open: %q (%w)", workingFile, err)
	}
	defer out.Close()

	defer func() {
		if retErr != nil {
			if _, err := prog.fsys.Stat(src); err == nil {
				_ = prog.fsys.Remove(workingFile)
			} else if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(prog.stderr, "cleanup: not found: %q\n", src)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", workingFile)
			} else {
				fmt.Fprintf(prog.stderr, "cleanup: failed to stat: %s (%v)\n", src, err)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", src)
				fmt.Fprintf(prog.stderr, "cleanup: not removing: %q\n", workingFile)
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
		return fmt.Errorf("failed to close: %q (%w)", src, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close: %q (%w)", workingFile, err)
	}

	srcChecksum := hex.EncodeToString(srcHasher.Sum(nil))
	dstChecksum := hex.EncodeToString(dstHasher.Sum(nil))

	if srcChecksum != dstChecksum {
		return fmt.Errorf("%w: %q (%s) != %q (%s)", errMemoryHashMismatch, src, srcChecksum, workingFile, dstChecksum)
	}

	if err := prog.fsys.Rename(workingFile, dst); err != nil {
		return fmt.Errorf("failed to rename: %q -x-> %q (%w)", workingFile, dst, err)
	}

	workingFile = dst // We work on the actual destination file now.

	if prog.opts.Verify {
		verifyHasher := blake3.New()

		verifier, err := prog.fsys.Open(workingFile)
		if err != nil {
			return fmt.Errorf("failed to re-open for --verify pass: %q (%w)", workingFile, err)
		}
		defer verifier.Close()

		if _, err := io.Copy(verifyHasher, verifier); err != nil {
			return fmt.Errorf("failed to re-read for --verify pass: %q (%w)", workingFile, err)
		}

		if err := verifier.Close(); err != nil {
			return fmt.Errorf("failed to close after --verify pass: %q (%w)", workingFile, err)
		}

		verifyChecksum := hex.EncodeToString(verifyHasher.Sum(nil))
		if verifyChecksum != srcChecksum {
			return fmt.Errorf("%w: %q (%s) != %q (%s)", errVerifyHashMismatch, src, srcChecksum, workingFile, verifyChecksum)
		}
	}

	if err := prog.fsys.Remove(src); err != nil {
		return fmt.Errorf("failed to remove (after move): %q (%w)", src, err)
	}

	return nil
}
