package clipboard

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBin writes a shell script standing in for the clipsync helper.
func fakeBin(t *testing.T, script string) string {
	p := filepath.Join(t.TempDir(), "clipsync")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755))
	return p
}

func TestClipsync_ExportImport(t *testing.T) {
	bin := fakeBin(t, `case "$1" in
export) printf 'PAYLOAD'; echo "clipsync: 2 эл., 1 КБ" >&2 ;;
import) cat > "$(dirname "$0")/imported"; echo "clipsync: восстановлено 2 эл." >&2 ;;
esac`)
	c := NewClipsync(bin)

	data, summary, err := c.Export()
	require.NoError(t, err)
	assert.Equal(t, "PAYLOAD", string(data))
	assert.Equal(t, "2 эл., 1 КБ", summary)

	summary, err = c.Import([]byte("IN"))
	require.NoError(t, err)
	assert.Equal(t, "восстановлено 2 эл.", summary)
	got, _ := os.ReadFile(filepath.Join(filepath.Dir(bin), "imported"))
	assert.Equal(t, "IN", string(got))
}

func TestClipsync_Errors(t *testing.T) {
	_, _, err := NewClipsync("/nonexistent/clipsync").Export()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "make clipsync")

	bin := fakeBin(t, `echo "clipsync: буфер пуст" >&2; exit 1`)
	_, _, err = NewClipsync(bin).Export()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "буфер пуст")
}

func TestClipsync_WatchDoubleCopy(t *testing.T) {
	bin := fakeBin(t, `echo "clipsync: слежу" >&2; echo double; sleep 0.1; echo noise; echo double; sleep 5`)
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	done := make(chan error, 1)
	go func() { done <- NewClipsync(bin).WatchDoubleCopy(ctx, time.Second, func() { n.Add(1) }) }()

	require.Eventually(t, func() bool { return n.Load() == 2 }, 2*time.Second, 20*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "stopping via context is not an error")
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not stop")
	}
}
