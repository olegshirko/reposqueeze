package state

import (
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

func TestFileStore_RoundTrip(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", repo).Run())

	store := NewFileStore(git.NewOSExecGitGateway(logger.NewLoggerWithWriter(io.Discard)))

	set, err := store.Load(repo)
	require.NoError(t, err)
	assert.Empty(t, set.Mirrors)

	now := time.Now().UTC().Truncate(time.Second)
	m := entity.Mirror{
		Name: "main->p:release", LocalBranch: "main", ProjectID: 7, ProjectName: "p", RemoteBranch: "release",
		Origin: entity.SyncPoint{LocalSHA: "l0", RemoteSHA: "r0", At: now},
	}
	set.Upsert(m)
	m.Journal = append(m.Journal, entity.JournalEntry{SyncPoint: entity.SyncPoint{LocalSHA: "l1", RemoteSHA: "r1", At: now}, Direction: entity.SyncPush})
	set.Upsert(m) // replaces, does not duplicate
	require.NoError(t, store.Save(repo, set))

	loaded, err := store.Load(repo)
	require.NoError(t, err)
	require.Len(t, loaded.Mirrors, 1)
	got, err := loaded.Find("", "main")
	require.NoError(t, err)
	assert.Equal(t, "l1", got.Current().LocalSHA)
	assert.Equal(t, "r1", got.Current().RemoteSHA)
	assert.Equal(t, "l0", got.Origin.LocalSHA)
}

func TestMirrorSet_Find(t *testing.T) {
	set := &entity.MirrorSet{Mirrors: []entity.Mirror{
		{Name: "a", LocalBranch: "main"},
		{Name: "b", LocalBranch: "main"},
		{Name: "c", LocalBranch: "dev"},
	}}
	_, err := set.Find("", "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "several mirrors")
	m, err := set.Find("", "dev")
	require.NoError(t, err)
	assert.Equal(t, "c", m.Name)
	m, err = set.Find("b", "")
	require.NoError(t, err)
	assert.Equal(t, "b", m.Name)
	_, err = set.Find("", "feature")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync-init")
}
