package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateMirrorStructure_NestedMirror_Success(t *testing.T) {
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
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/real/mirror/dir1")
	require.NoError(t, err)
	_, err = fs.Stat("/real/mirror/dir2/subdir")
	require.NoError(t, err)

	// Verify nested mirror structure is not turning into insane recursion.
	_, err = fs.Stat("/real/mirror/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_WithFiles_Success(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/mirror")
	require.NoError(t, err)
	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)
}

func TestCreateMirrorStructure_WithExcludes_Success(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify included directory is mirrored.
	_, err = fs.Stat("/mirror/include")
	require.NoError(t, err)

	// Verify excluded directory is not mirrored.
	_, err = fs.Stat("/mirror/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_DryRun_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_DryRun_MirrorExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/real/dir1", "/mirror"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_EmptyMirror_Success(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.NoError(t, err)

	// Verify mirror structure is created.
	_, err = fs.Stat("/mirror/dir1")
	require.NoError(t, err)
	_, err = fs.Stat("/mirror/dir2/subdir")
	require.NoError(t, err)
}

func TestCreateMirrorStructure_NonExistentMirror_Success(t *testing.T) {
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
	}

	prog := setupTestProgram(fs, opts)
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

func TestCreateMirrorStructure_CtxCancel_Error(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// Verify mirror structure is not created.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat("/mirror/dir2/subdir")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_MirrorWithFiles_Error(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)
}

func TestCreateMirrorStructure_DryRun_FullMirror_Error(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorNotEmpty)

	// Verify no actual changes were made.
	_, err = fs.Stat("/mirror/dir1")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_RealRootNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/nonexistent",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err := prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	// Should not create mirror root.
	_, err = fs.Stat("/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_MirrorParentNotExist_Error(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorParentNotExist)

	// Should not create mirror root.
	_, err = fs.Stat("/notexist/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCreateMirrorStructure_MirrorParentNotDir_Error(t *testing.T) {
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

	prog := setupTestProgram(fs, opts)
	err = prog.createMirrorStructure(t.Context())
	require.ErrorIs(t, err, errMirrorParentNotDir)

	// Should not create mirror root.
	_, err = fs.Stat("/notexist/mirror")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestIsEmptyStructure_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	paths := []string{
		"/empty/dir1",
		"/empty/dir2/subdir",
	}
	err := createDirStructure(fs, paths)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/empty")
	require.NoError(t, err)
	require.True(t, empty)
}

func TestIsEmptyStructure_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/nonempty/dir1/file.txt": "content",
		"/nonempty/dir2/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/nonempty")
	require.NoError(t, err)
	require.False(t, empty)
}

func TestIsEmptyStructure_CtxCancel_Error(t *testing.T) {
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

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(ctx, "/empty")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, empty)
}

func TestIsEmptyStructure_NonExistentPath_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil)
	empty, err := prog.isEmptyStructure(t.Context(), "/nonexistent")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.False(t, empty)
}
