package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Expectation: The function should mirror the nested directory structure.
func Test_Unit_CreateMirrorStructure_DeepStructure_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/dir1/dir2/dir3/dir4/dir5",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		InitDepth:  -1,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/dir1/dir2/dir3/dir4/dir5")
	require.NoError(t, err)
}

// Expectation: The function should mirror the nested directory structure in slow-mode.
func Test_Unit_CreateMirrorStructure_DeepStructureSlow_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/dir1/dir2/dir3/dir4/dir5",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		SlowMode:   true,
		InitDepth:  -1,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/dir1/dir2/dir3/dir4/dir5")
	require.NoError(t, err)

	require.True(t, prog.opts.SlowMode)
}

// Expectation: The function should exclude the mirror root itself.
func Test_Unit_CreateMirrorStructure_NestedMirror_Success(t *testing.T) {
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
		InitDepth:  -1,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/real/mirror/dir1")
	require.NoError(t, err)

	_, err = fs.Stat("/real/mirror/dir2/subdir")
	require.NoError(t, err)

	// Verify nested mirror structure is not turning into insane recursion.
	_, err = fs.Stat("/real/mirror/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expecation: The function should only mirror directories of the target root.
func Test_Unit_CreateMirrorStructure_WithFiles_Success(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)
}

// Expectation: The function should not mirror excluded directories.
func Test_Unit_CreateMirrorStructure_WithExcludes_Success(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	// Verify excluded directory is not mirrored.
	_, err = fs.Stat("/mirror/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should mirror the full structure.
func Test_Unit_CreateMirrorStructure_WithInitDepth_Unlimited_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/one",
		"/real/one/two",
		"/real/one/two/three",
		"/real/one/two/three/four",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		InitDepth:  -1,
		DryRun:     false,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/one")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/one/two")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/one/two/three")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/one/two/three/four")
	require.NoError(t, err)
}

// Expectation: The function should not mirror directories deeper than allowed.
func Test_Unit_CreateMirrorStructure_WithInitDepth_Zero_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/lv1",             // depth 0
		"/real/lv1/lv2",         // depth 1
		"/real/lv1/lv2/lv3",     // depth 2
		"/real/lv1/lv2/lv3/lv4", // depth 3
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		InitDepth:  0,
		DryRun:     false,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/lv1")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/lv1/lv2")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/lv1/lv2/lv3")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/lv1/lv2/lv3/lv4")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should not mirror directories deeper than allowed.
func Test_Unit_CreateMirrorStructure_WithInitDepth_NonZero_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/real/lv1",             // depth 0
		"/real/lv1/lv2",         // depth 1
		"/real/lv1/lv2/lv3",     // depth 2
		"/real/lv1/lv2/lv3/lv4", // depth 3
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		InitDepth:  1,
		DryRun:     false,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/lv1")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/lv1/lv2")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/lv1/lv2/lv3")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/lv1/lv2/lv3/lv4")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should respect the dry-run mode and not write anything.
func Test_Unit_CreateMirrorStructure_DryRun_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should not delete an existing mirror in dry-run mode.
func Test_Unit_CreateMirrorStructure_DryRun_MirrorExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1", "/mirror"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expecation: The function should remove and re-create an empty mirror.
func Test_Unit_CreateMirrorStructure_EmptyMirror_Success(t *testing.T) {
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
		InitDepth:  -1,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)

	_, err = fs.Stat("/mirror/dir2/subdir")
	require.NoError(t, err)
}

// Expectation: The function should create a not existing mirror.
func Test_Unit_CreateMirrorStructure_NonExistentMirror_Success(t *testing.T) {
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
		InitDepth:  -1,
	}

	prog, _, _ := setupTestProgram(fs, opts)
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

// Expectation: The function should respond to a context cancellation.
func Test_Unit_CreateMirrorStructure_CtxCancel_Error(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// Verify mirror structure is not created.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/dir2/subdir")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should not delete a mirror containing files.
func Test_Unit_CreateMirrorStructure_MirrorNotEmpty_Error(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)

	_, err = fs.Stat("/mirror/existing.txt")
	require.NoError(t, err)
}

// Expectation: The function should also return a non-empty error in dry mode.
func Test_Unit_CreateMirrorStructure_DryRun_MirrorNotEmpty_Error(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/mirror/file.txt")
	require.NoError(t, err)
}

// Expecation: The function should not execute with a missing real root.
func Test_Unit_CreateMirrorStructure_RealRootNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/nonexistent",
		DryRun:     false,
	}

	prog, _, _ := setupTestProgram(fs, opts)
	err := prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	// Should not create mirror root.
	_, err = fs.Stat("/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should not run if the mirror's parent does not exist.
func Test_Unit_CreateMirrorStructure_MirrorParentNotExist_Error(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorParentNotExist)

	// Should not create mirror root.
	_, err = fs.Stat("/notexist/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should not run if the mirror's parent is a file.
func Test_Unit_CreateMirrorStructure_MirrorParentNotDir_Error(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorParentNotDir)

	// Should not create mirror root.
	_, err = fs.Stat("/notexist/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Expectation: The function should report a known empty structure as empty.
func Test_Unit_IsEmptyStructure_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	paths := []string{
		"/empty/dir1",
		"/empty/dir2/subdir",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	prog, _, _ := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/empty")

	require.NoError(t, err)
	require.True(t, empty)
}

// Expectation: The function should report a known not-empty structure as not-empty.
func Test_Unit_IsEmptyStructure_NotEmpty_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/nonempty/dir1/file.txt": "content",
		"/nonempty/dir2/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog, _, _ := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/nonempty")

	require.NoError(t, err)
	require.False(t, empty)
}

// Expectation: The function should respond to a context cancellation.
func Test_Unit_IsEmptyStructure_CtxCancel_Error(t *testing.T) {
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

	prog, _, _ := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(ctx, "/empty")

	require.ErrorIs(t, err, context.Canceled)
	require.False(t, empty)
}

// Expectation: The function should not run with a non-existing path.
func Test_Unit_IsEmptyStructure_NonExistentPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog, _, _ := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/nonexistent")

	require.ErrorIs(t, err, os.ErrNotExist)
	require.False(t, empty)
}
