package clipstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

func TestGitStore_SingleCommitBranch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")

	// Remote missing entirely: nothing pushed yet.
	home := NewGitStore(remote, "clip", filepath.Join(dir, "home-cache"))
	v, err := home.Version(ctx)
	require.NoError(t, err)
	assert.Empty(t, v)

	require.NoError(t, exec.Command("git", "init", "-q", "--bare", remote).Run())
	work := NewGitStore(remote, "clip", filepath.Join(dir, "work-cache"))
	_, _, err = work.Get(ctx)
	require.ErrorIs(t, err, gateway.ErrNoClip)

	require.NoError(t, home.Put(ctx, []byte("Salted__\x00\xffblob-1"), []byte(`{"host":"home"}`)))
	v1, err := work.Version(ctx)
	require.NoError(t, err)
	require.Len(t, v1, 40)

	blob, meta, err := work.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Salted__\x00\xffblob-1", string(blob), "binary survives")
	assert.Equal(t, `{"host":"home"}`, string(meta))

	require.NoError(t, home.Put(ctx, []byte("blob-2"), []byte(`{"host":"home"}`)))
	v2, err := work.Version(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, v1, v2)
	blob, _, err = work.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "blob-2", string(blob))

	// The branch never accumulates history.
	out, err := exec.Command("git", "--git-dir", remote, "rev-list", "--count", "clip").Output()
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(out)))
}

func TestGitStore_NewProjectGetsMainFirst(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	require.NoError(t, exec.Command("git", "init", "-q", "--bare", remote).Run())

	s := NewGitStore(remote, "clip", filepath.Join(dir, "cache"))
	require.NoError(t, s.Put(ctx, []byte("b"), []byte("m")))

	heads, err := exec.Command("git", "--git-dir", remote, "for-each-ref", "--format=%(refname:short)", "refs/heads").Output()
	require.NoError(t, err)
	assert.Equal(t, "clip\nmain", strings.TrimSpace(string(heads)))
	files, err := exec.Command("git", "--git-dir", remote, "ls-tree", "--name-only", "main").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(files)), "main carries no data")
}

func TestGitStore_ProtectedBranchFallsBackToRegularPush(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	require.NoError(t, exec.Command("git", "init", "-q", "--bare", remote).Run())

	s := NewGitStore(remote, "clip", filepath.Join(dir, "cache"))
	var warned string
	s.Warnf = func(f string, a ...interface{}) { warned = f }
	require.NoError(t, s.Put(ctx, []byte("first"), []byte("m")))

	// Emulate GitLab's protected branch: reject non-fast-forward updates.
	hook := "#!/bin/sh\nwhile read old new ref; do\n" +
		"  if [ \"$old\" != 0000000000000000000000000000000000000000 ] && ! git merge-base --is-ancestor \"$old\" \"$new\"; then\n" +
		"    echo 'GitLab: You are not allowed to force push code to a protected branch on this project.' >&2; exit 1\n" +
		"  fi\ndone\n"
	hookPath := filepath.Join(remote, "hooks", "pre-receive")
	require.NoError(t, os.WriteFile(hookPath, []byte(hook), 0o755))

	require.NoError(t, s.Put(ctx, []byte("second"), []byte("m")))
	assert.Contains(t, warned, "protected")
	blob, _, err := NewGitStore(remote, "clip", filepath.Join(dir, "other")).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "second", string(blob))
}
