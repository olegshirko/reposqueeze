package usecase

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()

	p, err := safeJoin(root, "a/b.txt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "a", "b.txt"), p)

	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../x", ".", "a/.."} {
		_, err := safeJoin(root, bad)
		assert.Error(t, err, "path %q must be rejected", bad)
	}

	// A relative root such as "." works too.
	wd, _ := os.Getwd()
	require.NoError(t, os.Chdir(root))
	defer os.Chdir(wd)
	p, err = safeJoin(".", "docs/new.md")
	require.NoError(t, err)
	assert.Equal(t, "new.md", filepath.Base(p))
	_, err = safeJoin(".", "../x")
	assert.Error(t, err)
}

func TestProjectNameFromPath(t *testing.T) {
	assert.Equal(t, "repo", ProjectNameFromPath("/x/repo"))
	assert.Equal(t, "repo", ProjectNameFromPath("/x/repo/"))
	assert.Equal(t, "repo", ProjectNameFromPath("/x/repo.git"))

	dir := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.Mkdir(dir, 0o755))
	wd, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	defer os.Chdir(wd)
	assert.Equal(t, "my-project", ProjectNameFromPath("."))
}

func TestBuildCommitActions(t *testing.T) {
	remote := map[string]bool{"exists.txt": true, "old.txt": true, "gone.txt": true}
	content := func(p string) ([]byte, error) { return []byte("data:" + p), nil }
	exists := func(p string) bool { return remote[p] }

	actions, err := buildCommitActions([]gateway.CommitFileInfo{
		{Status: "A", Path: "new.txt"},
		{Status: "M", Path: "exists.txt"},
		{Status: "D", Path: "gone.txt"},
		{Status: "D", Path: "never-pushed.txt"},
		{Status: "R100", OldPath: "old.txt", Path: "renamed.txt"},
	}, content, exists)
	require.NoError(t, err)

	assert.Equal(t, []gateway.CommitAction{
		{Action: "create", FilePath: "new.txt", Content: "data:new.txt", Encoding: "text"},
		{Action: "update", FilePath: "exists.txt", Content: "data:exists.txt", Encoding: "text"},
		{Action: "delete", FilePath: "gone.txt"},
		{Action: "delete", FilePath: "old.txt"},
		{Action: "create", FilePath: "renamed.txt", Content: "data:renamed.txt", Encoding: "text"},
	}, actions)

	_, err = buildCommitActions([]gateway.CommitFileInfo{{Status: "X", Path: "a"}}, content, exists)
	assert.Error(t, err)

	_, err = buildCommitActions([]gateway.CommitFileInfo{{Status: "A", Path: "a"}},
		func(string) ([]byte, error) { return nil, errors.New("boom") }, exists)
	assert.Error(t, err)
}

func TestApplyRemoteDiff(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "del.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.txt"), []byte("x"), 0o644))

	gl := new(MockGitLabGateway)
	gl.On("GetRawFile", 1, "dir/new.txt", "ref").Return([]byte("new"), nil)
	gl.On("GetRawFile", 1, "renamed.txt", "ref").Return([]byte("ren"), nil)

	n, err := applyRemoteDiff(gl, newTestLogger(), 1, "ref", root, []gateway.DiffEntry{
		{NewPath: "del.txt", DeletedFile: true},
		{OldPath: "old.txt", NewPath: "renamed.txt", RenamedFile: true},
		{NewPath: "dir/new.txt", NewFile: true},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.NoFileExists(t, filepath.Join(root, "del.txt"))
	assert.NoFileExists(t, filepath.Join(root, "old.txt"))
	assert.FileExists(t, filepath.Join(root, "dir", "new.txt"))

	_, err = applyRemoteDiff(gl, newTestLogger(), 1, "ref", root, []gateway.DiffEntry{{NewPath: "../escape.txt", DeletedFile: true}})
	assert.Error(t, err)
}

func newTestLogger() logger.Logger {
	return logger.NewLoggerWithWriter(io.Discard)
}
