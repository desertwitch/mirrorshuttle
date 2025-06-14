package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
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
				prog.log.Warn("path skipped", "op", prog.opts.Mode, "path", path, "reason", "no_longer_exists")

				// An element has disappeared during the walk, skip it.
				return nil
			}

			// Another failure has occurred during the walk (permissions, ...), handle it.
			return prog.walkError(fmt.Errorf("failed to walk: %q (%w)", path, err))
		}

		if isExcluded(path, prog.opts.Excludes) { // Check if the source path is excluded.
			prog.log.Warn("path skipped", "op", prog.opts.Mode, "path", path, "reason", "is_user_excluded")

			// The source path was among the user's excluded paths, skip it.
			if e.IsDir() {
				return filepath.SkipDir // Do not traverse deeper.
			}

			return nil
		}

		// Construct the target path from the mirror's relative path.
		relPath, err := filepath.Rel(prog.opts.MirrorRoot, path)
		if err != nil {
			return prog.walkError(fmt.Errorf("failed to get relative path: %q (%w)", path, err))
		}
		movePath := filepath.Join(prog.opts.RealRoot, relPath)

		if movePath == prog.opts.MirrorRoot { // Check if target path is the mirror root.
			prog.log.Warn("path skipped", "op", prog.opts.Mode, "path", movePath, "reason", "mirror_into_mirror")

			// The target path is the mirror root, skip it (prevent insane recursion).
			return filepath.SkipDir
		}

		if isExcluded(movePath, prog.opts.Excludes) { // Check if the target path is excluded.
			prog.log.Warn("path skipped", "op", prog.opts.Mode, "path", movePath, "reason", "is_user_excluded")

			// The target path was among the user's excluded paths, skip it.
			if e.IsDir() {
				return filepath.SkipDir // Do not traverse deeper.
			}

			return nil
		}

		if e.IsDir() { // Handle directories.
			if _, err := prog.fsys.Stat(movePath); errors.Is(err, os.ErrNotExist) { // Check if the target directory exists.
				if !prog.opts.DryRun {
					// Create the target directory, if it does not exist.
					if err := prog.fsys.Mkdir(movePath, dirBasePerm); err != nil {
						return prog.walkError(fmt.Errorf("failed to create: %q (%w)", movePath, err))
					}
					prog.state.createdDirs++
				}
				prog.log.Info("directory created", "op", prog.opts.Mode, "path", movePath, "dry-run", prog.opts.DryRun)
			} else if err != nil {
				return prog.walkError(fmt.Errorf("failed to stat: %q (%w)", movePath, err))
			}

			return nil
		} // Must be a file from here downwards.

		if _, err := prog.fsys.Stat(movePath); err == nil { // Check if the target file exists.
			prog.state.hasUnmovedFiles = true
			prog.log.Warn("target already exists", "op", prog.opts.Mode, "src", path, "dst", movePath, "action", "skipped")

			// The target file exists; do not overwrite it, set unmoved files bit and skip it.
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return prog.walkError(fmt.Errorf("failed to stat: %q (%w)", movePath, err))
		}

		if !prog.opts.DryRun {
			if prog.opts.Direct {
				// Direct mode; attempt a rename syscall, otherwise copy and remove.
				if err := prog.fsys.Rename(path, movePath); err == nil {
					prog.log.Info("file moved", "op", prog.opts.Mode, "mode", "direct", "src", path, "dst", movePath, "dry-run", prog.opts.DryRun)
					prog.state.movedFiles++

					return nil
				} // Rename syscall must have failed from here downwards.
			}

			// Do the regular copy and remove operation and handle any failures.
			retHashes, err := prog.copyAndRemove(ctx, path, movePath)
			if err != nil {
				return prog.walkError(fmt.Errorf("failed to move: %q -x-> %q (%w)", path, movePath, err))
			}

			// Output the SHA-256 hashes for this operation as well, as parsing programs may care about them.
			prog.log.Info(
				"file moved",
				"op", prog.opts.Mode,
				"mode", "c+r",
				"src", path,
				"dst", movePath,
				"srcHash", retHashes.srcHash,
				"dstHash", retHashes.dstHash,
				"verifyHash", retHashes.verifyHash,
				"verify", prog.opts.Verify,
				"dry-run", prog.opts.DryRun,
			)
			prog.state.movedFiles++

			return nil
		} // Must be in dry mode from here downwards.

		prog.log.Info("file moved", "op", prog.opts.Mode, "mode", "", "src", path, "dst", movePath, "dry-run", prog.opts.DryRun)

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (prog *program) copyAndRemove(ctx context.Context, src string, dst string) (retHashes fileHashes, retErr error) {
	workingFile := dst + ".mirsht" // We work on a temporary file first.

	in, err := prog.fsys.Open(src)
	if err != nil {
		return retHashes, fmt.Errorf("failed to open: %q (%w)", src, err)
	}
	defer in.Close()

	out, err := prog.fsys.Create(workingFile)
	if err != nil {
		return retHashes, fmt.Errorf("failed to open: %q (%w)", workingFile, err)
	}
	defer out.Close()

	defer func() {
		if retErr != nil {
			if _, err := prog.fsys.Stat(src); err == nil {
				if err := prog.fsys.Remove(workingFile); err == nil {
					prog.log.Info("incomplete file removed", "op", prog.opts.Mode+"_cleanup", "path", workingFile)
				} else if !errors.Is(err, os.ErrNotExist) {
					prog.log.Error("incomplete file not removed", "op", prog.opts.Mode+"_cleanup", "path", workingFile, "error", err, "error-type", "runtime", "reason", "error_occurred")
				}
			} else if errors.Is(err, os.ErrNotExist) {
				prog.log.Warn("file not found", "op", prog.opts.Mode+"_cleanup", "path", src)
				prog.log.Warn("incomplete file not removed", "op", prog.opts.Mode+"_cleanup", "path", workingFile, "reason", "src_no_longer_exists")
			} else {
				prog.log.Error("failed to stat", "op", prog.opts.Mode+"_cleanup", "path", src, "error", err, "error-type", "runtime")
				prog.log.Warn("incomplete file not removed", "op", prog.opts.Mode+"_cleanup", "path", src, "reason", "src_existence_unknown")
				prog.log.Warn("incomplete file not removed", "op", prog.opts.Mode+"_cleanup", "path", workingFile, "reason", "src_existence_unknown")
			}
		}
	}()

	srcHasher := sha256.New()
	dstHasher := sha256.New()

	ctxReader := &contextReader{ctx, io.TeeReader(in, srcHasher)}
	multiWriter := io.MultiWriter(out, dstHasher)

	if _, err := io.Copy(multiWriter, ctxReader); err != nil {
		return retHashes, fmt.Errorf("failed during io: %w", err)
	}

	if err := out.Sync(); err != nil {
		return retHashes, fmt.Errorf("failed during sync: %w", err)
	}

	if err := in.Close(); err != nil {
		return retHashes, fmt.Errorf("failed to close: %q (%w)", src, err)
	}

	if err := out.Close(); err != nil {
		return retHashes, fmt.Errorf("failed to close: %q (%w)", workingFile, err)
	}

	retHashes.srcHash = hex.EncodeToString(srcHasher.Sum(nil))
	retHashes.dstHash = hex.EncodeToString(dstHasher.Sum(nil))

	if retHashes.srcHash != retHashes.dstHash {
		return retHashes, fmt.Errorf("%w: %q (srcHash) != %q (dstHash)", errMemoryHashMismatch, retHashes.srcHash, retHashes.dstHash)
	}

	if err := prog.fsys.Rename(workingFile, dst); err != nil {
		return retHashes, fmt.Errorf("failed to rename: %q -x-> %q (%w)", workingFile, dst, err)
	}

	workingFile = dst // We work on the actual destination file now.

	if prog.opts.Verify {
		verifyHasher := sha256.New()

		verifier, err := prog.fsys.Open(workingFile)
		if err != nil {
			return retHashes, fmt.Errorf("failed to re-open for --verify pass: %q (%w)", workingFile, err)
		}
		defer verifier.Close()

		ctxReader := &contextReader{ctx, verifier}

		if _, err := io.Copy(verifyHasher, ctxReader); err != nil {
			return retHashes, fmt.Errorf("failed to re-read for --verify pass: %q (%w)", workingFile, err)
		}

		if err := verifier.Close(); err != nil {
			return retHashes, fmt.Errorf("failed to close after --verify pass: %q (%w)", workingFile, err)
		}

		retHashes.verifyHash = hex.EncodeToString(verifyHasher.Sum(nil))

		if retHashes.srcHash != retHashes.verifyHash {
			return retHashes, fmt.Errorf("%w: %q (srcHash) != %q (verifyHash)", errVerifyHashMismatch, retHashes.srcHash, retHashes.verifyHash)
		}
	}

	if err := prog.fsys.Remove(src); err != nil {
		return retHashes, fmt.Errorf("failed to remove (after move): %q (%w)", src, err)
	}

	return retHashes, nil
}
