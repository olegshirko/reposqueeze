package clipstore

import (
	"context"
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
