package clipsetup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/infrastructure/clipstore"
)

func TestStore_TokenMeansHTTPS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".clipsync"), 0o700))
	// A Mac that already uses SSH (e.g. from its LaunchAgent).
	sshRemote := "git@gitlab.com:oleg.shirko/clipboard.git"
	require.NoError(t, os.WriteFile(filepath.Join(home, ".clipsync", "remote"), []byte(sshRemote+"\n"), 0o600))

	calls := 0
	o := Options{Token: "glpat-secret", CurrentUser: func() (string, error) { calls++; return "oleg.shirko", nil }}
	store, where, err := Store(context.Background(), o)
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/oleg.shirko/clipboard.git", where)
	gs := store.(*clipstore.GitStore)
	assert.Equal(t, "glpat-secret", gs.Token)

	// Cached for next time, without touching the SSH remote.
	_, where, err = Store(context.Background(), o)
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/oleg.shirko/clipboard.git", where)
	assert.Equal(t, 1, calls)
	data, _ := os.ReadFile(filepath.Join(home, ".clipsync", "remote"))
	assert.Equal(t, sshRemote, strings.TrimSpace(string(data)))

	// Without a token the SSH remote is used.
	_, where, err = Store(context.Background(), Options{})
	require.NoError(t, err)
	assert.Equal(t, sshRemote, where)
}

func TestStore_SelfManagedHTTPS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, where, err := Store(context.Background(), Options{
		BaseURL: "https://git.example.com:8443", Token: "t",
		CurrentUser: func() (string, error) { return "me", nil },
	})
	require.NoError(t, err)
	assert.Equal(t, "https://git.example.com:8443/me/clipboard.git", where)
}
