package main

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestMoveFiles_RegularMove_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":      "content",
		"/mirror/dir/file.txt":  "content2",
		"/mirror/dir1/file.txt": "content3",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real/dir",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify files moved to real structure.
	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content", string(content))

	content, err = afero.ReadFile(fs, "/real/dir/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	content, err = afero.ReadFile(fs, "/real/dir1/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content3", string(content))

	// Verify files removed from mirror.
	_, err = fs.Stat("/mirror/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_DirectMove_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":      "content",
		"/mirror/dir/file.txt":  "content2",
		"/mirror/dir1/file.txt": "content3",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real/dir",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		Direct:     true,
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify files moved to real structure.
	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content", string(content))

	content, err = afero.ReadFile(fs, "/real/dir/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content2", string(content))

	content, err = afero.ReadFile(fs, "/real/dir1/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content3", string(content))

	// Verify files removed from mirror.
	_, err = fs.Stat("/mirror/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_FileAlreadyExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt": "mirror content",
		"/real/file.txt":   "existing content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify existing file is not overwritten.
	content, err := afero.ReadFile(fs, "/real/file.txt")
	require.NoError(t, err)
	require.Equal(t, "existing content", string(content))

	// Verify mirror file still exists (not moved).
	content, err = afero.ReadFile(fs, "/mirror/file.txt")
	require.NoError(t, err)
	require.Equal(t, "mirror content", string(content))

	require.True(t, prog.hasUnmovedFiles)
}

func TestMoveFiles_WithExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/include.txt": "content",
		"/mirror/exclude.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		Excludes:   excludeArg{"/mirror/exclude.txt"},
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify included file is moved.
	_, err = fs.Stat("/real/include.txt")
	require.NoError(t, err)

	// Verify excluded file is not moved.
	_, err = fs.Stat("/real/exclude.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify excluded file still exists in mirror.
	_, err = fs.Stat("/mirror/exclude.txt")
	require.NoError(t, err)
}

func TestMoveFiles_WithDirExcludes_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/include.txt":         "content",
		"/mirror/exclude/exclude.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
		Excludes:   excludeArg{"/mirror/exclude"},
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify included file is moved.
	_, err = fs.Stat("/real/include.txt")
	require.NoError(t, err)

	// Verify excluded folder is not created.
	_, err = fs.Stat("/real/exclude")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify excluded file is not moved.
	_, err = fs.Stat("/real/exclude/exclude.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify excluded file still exists in mirror.
	_, err = fs.Stat("/mirror/exclude/exclude.txt")
	require.NoError(t, err)
}

func TestMoveFiles_DryRun_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":       "content",
		"/mirror/dir1/file2.txt": "content",
		"/real/otherfile.txt":    "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     true,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify no actual changes were made.
	_, err = fs.Stat("/real/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify mirror file still exists.
	_, err = fs.Stat("/mirror/file.txt")
	require.NoError(t, err)
}

func TestMoveFiles_CreateTargetDirs_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/deep/nested/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	// Assume at least the target base exists
	paths := []string{
		"/real",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify target directory structure is created.
	_, err = fs.Stat("/real/deep/nested")
	require.NoError(t, err)

	// Verify file is moved.
	content, err := afero.ReadFile(fs, "/real/deep/nested/file.txt")
	require.NoError(t, err)
	require.Equal(t, "content", string(content))
}

func TestMoveFiles_EmptyMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{"/mirror", "/real"})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)
}

func TestMoveFiles_DirectoryAlreadyExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	err := createDirStructure(fs, []string{
		"/mirror/existingdir",
		"/real/existingdir",
	})
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify mirror directory still exists.
	_, err = fs.Stat("/mirror/existingdir")
	require.NoError(t, err)

	// Verify directory still exists.
	_, err = fs.Stat("/real/existingdir")
	require.NoError(t, err)
}

func TestMoveFiles_MoveIntoMirror_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mnt/user/mirror/mirror/test.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mnt/user/mirror",
		RealRoot:   "/mnt/user",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.NoError(t, err)

	// Verify file is not moved into the mirror structure (pointless).
	_, err = fs.Stat("/mnt/user/mirror/test.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_CtxCancel_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/file.txt":      "content",
		"/mirror/dir/file.txt":  "content2",
		"/mirror/dir1/file.txt": "content3",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	paths := []string{
		"/real/dir",
	}
	err = createDirStructure(fs, paths)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(ctx)
	require.ErrorIs(t, err, context.Canceled)

	// Verify files not removed from mirror.
	_, err = fs.Stat("/mirror/file.txt")
	require.NoError(t, err)

	// Verify files not moved.
	_, err = fs.Stat("/real/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/dir/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = fs.Stat("/real/dir1/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMoveFiles_CreateTargetDirs_BaseGone_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/mirror/deep/nested/file.txt": "content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	opts := &programOptions{
		MirrorRoot: "/mirror",
		RealRoot:   "/real",
		DryRun:     false,
	}

	// Verify the operation fails as base is missing.
	prog := setupTestProgram(fs, opts)
	err = prog.moveFiles(t.Context())
	require.ErrorIs(t, err, errTargetNotExist)

	// Verify mirror file is not removed.
	_, err = fs.Stat("/mirror/deep/nested/file.txt")
	require.NoError(t, err)
}

func TestMoveFiles_MirrorNotExist_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	opts := &programOptions{
		MirrorRoot: "/nonexistent",
		RealRoot:   "/real",
		DryRun:     false,
	}

	prog := setupTestProgram(fs, opts)
	err := prog.moveFiles(t.Context())
	require.ErrorIs(t, err, errMirrorNotExist)
}

func TestCopyAndRemove_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/src/file.txt": "test content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	err = prog.copyAndRemove("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	// Verify source is removed.
	_, err = fs.Stat("/src/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify destination exists with correct content.
	content, err := afero.ReadFile(fs, "/dst/file.txt")
	require.NoError(t, err)
	require.Equal(t, "test content", string(content))
}

func TestCopyAndRemove_Verify_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/src/file.txt": "test content",
	}
	err := createFiles(fs, files)
	require.NoError(t, err)

	prog := setupTestProgram(fs, nil)
	prog.opts.Verify = true

	err = prog.copyAndRemove("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	// Verify source is removed.
	_, err = fs.Stat("/src/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify destination exists with correct content.
	content, err := afero.ReadFile(fs, "/dst/file.txt")
	require.NoError(t, err)
	require.Equal(t, "test content", string(content))

	// Verify the requested mode did not change within the program.
	require.True(t, prog.opts.Verify)
}

func TestCopyAndRemove_DstTmpFileExists_Success(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()
	files := map[string]string{
		"/src/file.txt":        "hello",
		"/dst/file.txt.mirsht": "existing",
	}
	require.NoError(t, createFiles(fs, files))

	prog := setupTestProgram(fs, nil)

	err := prog.copyAndRemove("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	_, err = fs.Stat("/dst/file.txt")
	require.NoError(t, err)

	_, err = fs.Stat("/dst/file.txt.mirsht")
	require.ErrorIs(t, err, os.ErrNotExist)

	// Verify destination exists with correct content.
	content, err := afero.ReadFile(fs, "/dst/file.txt")
	require.NoError(t, err)
	require.Equal(t, "hello", string(content))
}

func TestCopyAndRemove_SourceNotFound_Error(t *testing.T) {
	t.Parallel()

	fs := setupTestFs()

	prog := setupTestProgram(fs, nil)
	err := prog.copyAndRemove("/nonexistent/file.txt", "/dst/file.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
}
