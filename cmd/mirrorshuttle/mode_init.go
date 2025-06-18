package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

func (prog *program) createMirrorStructure(ctx context.Context) error {
	createdDirsBatch := 0

	// The real root needs to exist, otherwise we have nowhere to mirror from.
	if _, err := prog.fsys.Stat(prog.opts.RealRoot); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %q", errTargetNotExist, prog.opts.RealRoot)
	} else if err != nil {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.RealRoot, err)
	}

	// The mirror root's parent needs to exist, otherwise we cannot create the mirror root.
	mirrorParent := filepath.Dir(prog.opts.MirrorRoot)
	if e, err := prog.fsys.Stat(mirrorParent); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %q (%w)", errMirrorParentNotExist, mirrorParent, err)
		}

		return fmt.Errorf("failed to stat: %q (%w)", mirrorParent, err)
	} else if !e.IsDir() {
		// The mirror root's parent is not a directory, we cannot create the mirror root inside.
		return fmt.Errorf("%w: %q", errMirrorParentNotDir, mirrorParent)
	}

	// If the mirror root exists, it must be empty, otherwise it should not be removed.
	if _, err := prog.fsys.Stat(prog.opts.MirrorRoot); err == nil {
		prog.log.Info("testing if the existing mirror structure is empty...", "op", prog.opts.Mode)

		empty, err := prog.isEmptyStructure(ctx, prog.opts.MirrorRoot)
		if err != nil {
			return fmt.Errorf("failed checking for emptiness: %q (%w)", prog.opts.MirrorRoot, err)
		} else if !empty {
			// The mirror root contains files, we do not want to remove it, user should resolve it.
			return errMirrorNotEmpty
		}

		if !prog.opts.DryRun {
			// The mirror root is empty, we can remove it safely, for later re-creation.
			if err := prog.fsys.RemoveAll(prog.opts.MirrorRoot); err != nil {
				return fmt.Errorf("failed to remove: %q (%w)", prog.opts.MirrorRoot, err)
			}
		}
		prog.log.Info("mirror directory removed", "op", prog.opts.Mode, "path", prog.opts.MirrorRoot, "dry-run", prog.opts.DryRun)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat: %q (%w)", prog.opts.MirrorRoot, err)
	}

	// The mirror root either does not exist or was empty and deleted, re-create it now.
	if !prog.opts.DryRun {
		if err := prog.fsys.Mkdir(prog.opts.MirrorRoot, dirBasePerm); err != nil {
			return fmt.Errorf("failed to create: %q (%w)", prog.opts.MirrorRoot, err)
		}
		prog.state.createdDirs++
	}
	prog.log.Info("mirror directory created", "op", prog.opts.Mode, "path", prog.opts.MirrorRoot, "dry-run", prog.opts.DryRun)

	// Walk the target root and re-create the directory structure inside the mirror root.
	if err := afero.Walk(prog.fsys, prog.opts.RealRoot, func(path string, e os.FileInfo, err error) error {
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
			return prog.walkError(e, fmt.Errorf("failed to walk: %q (%w)", path, err))
		}

		if !e.IsDir() {
			// We do not care about files in this mode, skip them.
			return nil
		}

		if path == prog.opts.MirrorRoot { // Check if the walked path is the mirror root.
			prog.log.Warn("path skipped", "op", prog.opts.Mode, "path", path, "reason", "is_mirror_root")

			// The mirror root can be contained within the target root, skip it.
			return filepath.SkipDir // Do not traverse deeper.
		}

		if isExcluded(path, prog.opts.Excludes) { // Check if the walked path is excluded.
			prog.log.Warn("path skipped", "op", prog.opts.Mode, "path", path, "reason", "is_user_excluded")

			// The path was among the user's excluded paths, skip it.
			return filepath.SkipDir // Do not traverse deeper.
		}

		// Construct the mirror path from the target's relative path.
		relPath, err := filepath.Rel(prog.opts.RealRoot, path)
		if err != nil {
			return prog.walkError(e, fmt.Errorf("failed to get relative path: %q (%w)", path, err))
		}
		mirrorPath := filepath.Join(prog.opts.MirrorRoot, relPath)

		// Respect a user configured maximum mirroring depth for this mode.
		if prog.opts.InitDepth >= 0 {
			if dirDepth := dirDepth(relPath); dirDepth > prog.opts.InitDepth {
				prog.log.Debug("path skipped", "op", prog.opts.Mode, "path", path, "dir_depth", dirDepth, "reason", "exceeds_init_depth")

				// The depth exceeded the user configured limit.
				return filepath.SkipDir // Do not traverse deeper.
			}
		}

		if mirrorPath == prog.opts.MirrorRoot {
			// The mirror root itself was already created above, skip it.
			return nil
		}

		if !prog.opts.DryRun {
			// Create the respective mirror path for the specific target path.
			if err := prog.fsys.Mkdir(mirrorPath, dirBasePerm); err != nil {
				return prog.walkError(e, fmt.Errorf("failed to create: %q (%w)", mirrorPath, err))
			}
			createdDirsBatch++
			prog.state.createdDirs++

			if prog.opts.SlowMode && createdDirsBatch > dirCreationBatch {
				time.Sleep(dirCreationTimeout)
				createdDirsBatch = 0 // Reset the counter after timeout has passed.
			}
		}

		if !prog.opts.DryRun && prog.opts.SlowMode {
			prog.log.Info(
				"directory created",
				"op", prog.opts.Mode,
				"path", mirrorPath,
				"slow-mode", prog.opts.SlowMode,
				"slow-batch", fmt.Sprintf("%d/%d", createdDirsBatch, dirCreationBatch),
				"dry-run", prog.opts.DryRun,
			)

			return nil
		}

		prog.log.Info("directory created", "op", prog.opts.Mode, "path", mirrorPath, "slow-mode", prog.opts.SlowMode, "dry-run", prog.opts.DryRun)

		return nil
	}); err != nil {
		return err
	}

	return nil
}
