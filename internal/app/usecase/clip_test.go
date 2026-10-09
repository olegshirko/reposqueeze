package usecase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// fakeRegistry is an in-memory GitLab project with a generic package registry.
type fakeRegistry struct {
	project *entity.Project
	private bool
	files   []gateway.PackageFile
	data    map[int][]byte
	seq     int
}

func (f *fakeRegistry) FindProjectByName(name string) (*entity.Project, error) {
	if f.project != nil && f.project.Name == name {
		p := *f.project
		return &p, nil
	}
	return nil, nil
}

func (f *fakeRegistry) CreatePrivateProject(name string) (*entity.Project, error) {
	f.project, f.private = &entity.Project{ID: 7, Name: name}, true
	p := *f.project
	return &p, nil
}

func (f *fakeRegistry) UploadPackageFile(projectID int, pkg, version, file string, data []byte) error {
	f.seq++
	f.files = append(f.files, gateway.PackageFile{ID: f.seq, PackageID: 1, FileName: file})
	f.data[f.seq] = append([]byte(nil), data...)
	return nil
}

func (f *fakeRegistry) DownloadPackageFile(projectID int, pkg, version, file string) ([]byte, error) {
	for i := len(f.files) - 1; i >= 0; i-- {
		if f.files[i].FileName == file {
			return f.data[f.files[i].ID], nil
		}
	}
	return nil, errors.New("404 Not Found")
}

func (f *fakeRegistry) ListPackageFiles(projectID int, pkg, version string) ([]gateway.PackageFile, error) {
	return append([]gateway.PackageFile(nil), f.files...), nil
}

func (f *fakeRegistry) DeletePackageFile(projectID, packageID, fileID int) error {
	for i, pf := range f.files {
		if pf.ID == fileID {
			f.files = append(f.files[:i], f.files[i+1:]...)
			delete(f.data, fileID)
			return nil
		}
	}
	return fmt.Errorf("no file %d", fileID)
}

// fakeClipboard holds a packed clipboard in memory; sending to doubles
// simulates pressing ⌘C twice.
type fakeClipboard struct {
	mu      sync.Mutex
	content []byte
	doubles chan struct{}
}

func (c *fakeClipboard) get() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.content...)
}

func (c *fakeClipboard) set(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.content = b
}

func (c *fakeClipboard) WatchDoubleCopy(ctx context.Context, window time.Duration, onDouble func()) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.doubles:
			onDouble()
		}
	}
}

func (c *fakeClipboard) Export() ([]byte, string, error) {
	content := c.get()
	if len(content) == 0 {
		return nil, "", errors.New("буфер пуст")
	}
	return content, fmt.Sprintf("1 эл., %d Б", len(content)), nil
}

func (c *fakeClipboard) Import(p []byte) (string, error) {
	if len(p) == 0 || p[0] != 'P' { // our fake payloads start with "P"
		return "", errors.New("не разобрать полезную нагрузку")
	}
	c.set(append([]byte(nil), p...))
	return "восстановлено 1 эл.", nil
}

func keyFile(t *testing.T, key string) string {
	p := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(p, []byte(key+"\n"), 0o600))
	return p
}

func TestClip_PushPullBetweenMacs(t *testing.T) {
	reg := &fakeRegistry{data: map[int][]byte{}}
	key := keyFile(t, "shared-secret")
	home := &fakeClipboard{content: []byte("P: text, image and a file")}
	work := &fakeClipboard{}

	homeUC := clipUC(reg, home)
	homeUC.hostname = func() (string, error) { return "mac-home", nil }
	workUC := clipUC(reg, work)

	_, err := workUC.Pull(context.Background(), ClipConfig{KeyFile: key})
	require.ErrorIs(t, err, ErrNoClip)

	// The first push creates a private project.
	res, err := homeUC.Push(context.Background(), ClipConfig{KeyFile: key})
	require.NoError(t, err)
	assert.True(t, reg.private)
	assert.Equal(t, "clipboard", reg.project.Name)
	assert.Equal(t, "mac-home", res.From)

	// GitLab only sees ciphertext.
	blob, _ := reg.DownloadPackageFile(7, clipPackage, clipVersion, clipFile)
	assert.Equal(t, "Salted__", string(blob[:8]))
	assert.NotContains(t, string(blob), "text, image")

	got, err := workUC.Pull(context.Background(), ClipConfig{KeyFile: key})
	require.NoError(t, err)
	assert.Equal(t, home.content, work.content)
	assert.Equal(t, "mac-home", got.From)
	assert.Contains(t, got.Describe(got.At.Add(3*time.Minute)), "from mac-home, 3 min ago")

	// Repeated pushes keep a single copy of each file.
	home.content = []byte("P: second copy")
	_, err = homeUC.Push(context.Background(), ClipConfig{KeyFile: key})
	require.NoError(t, err)
	assert.Len(t, reg.files, 2, "clip.bin + meta.json only")
	_, err = workUC.Pull(context.Background(), ClipConfig{KeyFile: key})
	require.NoError(t, err)
	assert.Equal(t, "P: second copy", string(work.content))
}

func TestClip_WrongKeyIsReported(t *testing.T) {
	reg := &fakeRegistry{data: map[int][]byte{}}
	home := &fakeClipboard{content: []byte("P: data")}
	_, err := clipUC(reg, home).Push(context.Background(), ClipConfig{KeyFile: keyFile(t, "one")})
	require.NoError(t, err)

	work := &fakeClipboard{}
	_, err = clipUC(reg, work).Pull(context.Background(), ClipConfig{KeyFile: keyFile(t, "two")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same on both Macs")
	assert.Empty(t, work.content)

	_, err = clipUC(reg, work).Pull(context.Background(), ClipConfig{KeyFile: "/nonexistent/key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copy the same key file")
}

// syncRegistry serialises access to fakeRegistry for the concurrent watcher test.
type syncRegistry struct {
	mu sync.Mutex
	r  *fakeRegistry
}

func (s *syncRegistry) FindProjectByName(n string) (*entity.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.FindProjectByName(n)
}
func (s *syncRegistry) CreatePrivateProject(n string) (*entity.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.CreatePrivateProject(n)
}
func (s *syncRegistry) UploadPackageFile(id int, p, v, f string, d []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.UploadPackageFile(id, p, v, f, d)
}
func (s *syncRegistry) DownloadPackageFile(id int, p, v, f string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.DownloadPackageFile(id, p, v, f)
}
func (s *syncRegistry) ListPackageFiles(id int, p, v string) ([]gateway.PackageFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.ListPackageFiles(id, p, v)
}
func (s *syncRegistry) DeletePackageFile(id, pid, fid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.DeletePackageFile(id, pid, fid)
}

func TestClip_WatchPushesOnDoubleCopyAndPullsFromOthers(t *testing.T) {
	reg := &syncRegistry{r: &fakeRegistry{data: map[int][]byte{}}}
	key := keyFile(t, "k")
	cfg := ClipConfig{KeyFile: key}

	home := &fakeClipboard{doubles: make(chan struct{})}
	work := &fakeClipboard{content: []byte("P: old work clipboard")}
	homeUC := clipUC(reg, home)
	// Same host name on purpose: machines are told apart by machine id.
	homeUC.hostname = func() (string, error) { return "MacBook-Pro", nil }
	homeUC.machineID = func() string { return "home-id" }
	workUC := clipUC(reg, work)
	workUC.hostname = func() (string, error) { return "MacBook-Pro", nil }
	workUC.machineID = func() string { return "work-id" }

	// Something pushed before the work watcher starts is not pulled.
	home.set([]byte("P: stale"))
	_, err := homeUC.Push(context.Background(), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 10)
	report := func(kind string, res *ClipResult, err error) {
		if err != nil {
			events <- kind + " error: " + err.Error()
			return
		}
		events <- kind
	}
	go homeUC.Watch(ctx, cfg, ClipWatchOptions{Push: true, Pull: true, Interval: 20 * time.Millisecond, OnEvent: report})
	go workUC.Watch(ctx, cfg, ClipWatchOptions{Pull: true, Interval: 20 * time.Millisecond, OnEvent: report})
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, "P: old work clipboard", string(work.get()), "stale clipboard is not pulled")

	// ⌘C ⌘C at home -> pushed -> the work Mac picks it up by itself.
	home.set([]byte("P: fresh from home"))
	home.doubles <- struct{}{}

	got := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for !(got["push"] && got["pull"]) {
		select {
		case e := <-events:
			require.NotContains(t, e, "error")
			got[e] = true
		case <-deadline:
			t.Fatalf("events so far: %v", got)
		}
	}
	assert.Equal(t, "P: fresh from home", string(work.get()))

	// The home watcher does not pull its own push back.
	time.Sleep(100 * time.Millisecond)
	select {
	case e := <-events:
		t.Fatalf("unexpected event %q", e)
	default:
	}
}

// clipUC never touches ~/.clipsync: the machine id is fixed per clipboard.
func clipUC(reg ClipGitLab, cb *fakeClipboard) *ClipUseCase {
	uc := NewClipUseCase(NewPackageClipStore(reg, ""), cb, newTestLogger())
	uc.machineID = func() string { return fmt.Sprintf("test-%p", cb) }
	return uc
}
