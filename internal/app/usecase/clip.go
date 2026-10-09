package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/olegshirko/reposqueeze/internal/pkg/opensslenc"
)

// Clip defaults.
const (
	DefaultClipProject = "clipboard"
	clipPackage        = "clipboard"
	clipVersion        = "latest"
	clipFile           = "clip.bin"
	clipMetaFile       = "meta.json"
	maxClipBytes       = 64 << 20
)

// ClipGitLab is what the clipboard needs from GitLab: a private project and
// its generic package registry.
type ClipGitLab interface {
	FindProjectByName(name string) (*entity.Project, error)
	CreatePrivateProject(name string) (*entity.Project, error)
	gateway.GenericPackages
}

// ClipConfig says where the shared clipboard lives.
type ClipConfig struct {
	Project string // private GitLab project, created on first push
	KeyFile string // shared secret, the same file on both Macs
}

func (c ClipConfig) withDefaults() ClipConfig {
	if c.Project == "" {
		c.Project = DefaultClipProject
	}
	return c
}

// clipMeta travels next to the encrypted clipboard.
type clipMeta struct {
	Host    string    `json:"host"`    // for people
	Machine string    `json:"machine"` // stable id: host names of two Macs may coincide
	Summary string    `json:"summary"`
	At      time.Time `json:"at"`
}

// ClipResult describes a transferred clipboard.
type ClipResult struct {
	Summary string    // what clipsync packed or restored
	From    string    // host that pushed it
	At      time.Time // when it was pushed
	Warning string    // non-fatal problem, e.g. old copies not cleaned up
}

// ClipUseCase shares the clipboard between machines through GitLab: the
// packed clipboard is encrypted with a shared key and kept as the only file
// of a generic package in a private project. GitLab never sees plaintext and
// keeps no history of copies.
type ClipUseCase struct {
	gitlab    ClipGitLab
	clipboard gateway.Clipboard
	logger    logger.Logger
	hostname  func() (string, error)
	machineID func() string
	now       func() time.Time
}

// NewClipUseCase creates a ClipUseCase.
func NewClipUseCase(gitlab ClipGitLab, clipboard gateway.Clipboard, log logger.Logger) *ClipUseCase {
	return &ClipUseCase{gitlab: gitlab, clipboard: clipboard, logger: log,
		hostname: os.Hostname, machineID: defaultMachineID, now: time.Now}
}

func (uc *ClipUseCase) password(cfg ClipConfig) (string, error) {
	pass, err := opensslenc.ReadPassword(cfg.KeyFile)
	if err != nil {
		return "", fmt.Errorf("shared key %s: %w (copy the same key file to both Macs)", cfg.KeyFile, err)
	}
	return pass, nil
}

// Push sends the current clipboard.
func (uc *ClipUseCase) Push(ctx context.Context, cfg ClipConfig) (*ClipResult, error) {
	cfg = cfg.withDefaults()
	pass, err := uc.password(cfg)
	if err != nil {
		return nil, err
	}
	payload, summary, err := uc.clipboard.Export()
	if err != nil {
		return nil, err
	}
	if len(payload) > maxClipBytes {
		return nil, fmt.Errorf("clipboard is %d MB, the limit is %d MB", len(payload)>>20, maxClipBytes>>20)
	}
	blob, err := opensslenc.Encrypt(pass, payload)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	project, err := uc.gitlab.FindProjectByName(cfg.Project)
	if err != nil {
		return nil, err
	}
	if project == nil {
		uc.logger.Infof("Creating private GitLab project %q for the clipboard", cfg.Project)
		if project, err = uc.gitlab.CreatePrivateProject(cfg.Project); err != nil {
			return nil, err
		}
	}

	host, _ := uc.hostname()
	res := &ClipResult{Summary: summary, From: host, At: uc.now()}
	meta, err := json.Marshal(clipMeta{Host: host, Machine: uc.machineID(), Summary: summary, At: res.At})
	if err != nil {
		return nil, err
	}
	if err := uc.gitlab.UploadPackageFile(project.ID, clipPackage, clipVersion, clipFile, blob); err != nil {
		return nil, fmt.Errorf("upload to GitLab failed: %w", err)
	}
	if err := uc.gitlab.UploadPackageFile(project.ID, clipPackage, clipVersion, clipMetaFile, meta); err != nil {
		return nil, fmt.Errorf("upload to GitLab failed: %w", err)
	}
	if err := uc.prune(project.ID); err != nil {
		res.Warning = "old copies were not removed: " + err.Error()
		uc.logger.Warn(res.Warning)
	}
	return res, nil
}

// prune keeps only the newest copy of each file, so GitLab holds one clipboard.
func (uc *ClipUseCase) prune(projectID int) error {
	files, err := uc.gitlab.ListPackageFiles(projectID, clipPackage, clipVersion)
	if err != nil {
		return err
	}
	newest := map[string]int{}
	for _, f := range files {
		if f.ID > newest[f.FileName] {
			newest[f.FileName] = f.ID
		}
	}
	var errs []error
	for _, f := range files {
		if f.ID != newest[f.FileName] {
			if err := uc.gitlab.DeletePackageFile(projectID, f.PackageID, f.ID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Pull replaces the clipboard with the last pushed one.
func (uc *ClipUseCase) Pull(ctx context.Context, cfg ClipConfig) (*ClipResult, error) {
	cfg = cfg.withDefaults()
	pass, err := uc.password(cfg)
	if err != nil {
		return nil, err
	}
	project, err := uc.gitlab.FindProjectByName(cfg.Project)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("nothing pushed yet: GitLab project %q does not exist", cfg.Project)
	}
	blob, err := uc.gitlab.DownloadPackageFile(project.ID, clipPackage, clipVersion, clipFile)
	if err != nil {
		return nil, fmt.Errorf("nothing pushed yet to %q: %w", cfg.Project, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := opensslenc.Decrypt(pass, blob)
	if err != nil {
		return nil, err
	}
	summary, err := uc.clipboard.Import(payload)
	if err != nil {
		return nil, fmt.Errorf("%w (is the key file the same on both Macs?)", err)
	}

	res := &ClipResult{Summary: summary}
	if data, err := uc.gitlab.DownloadPackageFile(project.ID, clipPackage, clipVersion, clipMetaFile); err == nil {
		var meta clipMeta
		if json.Unmarshal(data, &meta) == nil {
			res.From, res.At = meta.Host, meta.At
		}
	}
	return res, nil
}

// latest returns the metadata of the last pushed clipboard (nil if none).
func (uc *ClipUseCase) latest(cfg ClipConfig) (*clipMeta, error) {
	project, err := uc.gitlab.FindProjectByName(cfg.Project)
	if err != nil || project == nil {
		return nil, err
	}
	data, err := uc.gitlab.DownloadPackageFile(project.ID, clipPackage, clipVersion, clipMetaFile)
	if err != nil {
		return nil, nil // nothing pushed yet (or not readable): treat as empty
	}
	var meta clipMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, nil
	}
	return &meta, nil
}

// ClipWatchOptions selects what the watcher does.
type ClipWatchOptions struct {
	Push     bool          // push when the same thing is copied twice (⌘C ⌘C)
	Window   time.Duration // max time between the two copies
	Pull     bool          // put clipboards pushed by other machines into this one
	Interval time.Duration // how often to check GitLab for a new clipboard
	// OnEvent reports what happened (for logs and notifications).
	OnEvent func(kind string, res *ClipResult, err error)
}

// Watch runs until ctx is done: it pushes on double copy and/or pulls
// clipboards pushed from other machines.
func (uc *ClipUseCase) Watch(ctx context.Context, cfg ClipConfig, opts ClipWatchOptions) error {
	cfg = cfg.withDefaults()
	if _, err := uc.password(cfg); err != nil {
		return err
	}
	if opts.Window <= 0 {
		opts.Window = time.Second
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Second
	}
	report := opts.OnEvent
	if report == nil {
		report = func(string, *ClipResult, error) {}
	}
	self := uc.machineID()

	// One operation at a time: a push and a pull must not interleave.
	var mu sync.Mutex
	errc := make(chan error, 2)

	if opts.Push {
		go func() {
			errc <- uc.clipboard.WatchDoubleCopy(ctx, opts.Window, func() {
				mu.Lock()
				defer mu.Unlock()
				res, err := uc.Push(ctx, cfg)
				report("push", res, err)
			})
		}()
	}

	if opts.Pull {
		go func() {
			// Only clipboards pushed after the watcher started are pulled.
			var seen time.Time
			if m, _ := uc.latest(cfg); m != nil {
				seen = m.At
			}
			t := time.NewTicker(opts.Interval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					errc <- nil
					return
				case <-t.C:
				}
				m, err := uc.latest(cfg)
				if err != nil || m == nil || !m.At.After(seen) {
					continue
				}
				seen = m.At
				if m.Machine == self {
					continue // our own push
				}
				mu.Lock()
				res, err := uc.Pull(ctx, cfg)
				mu.Unlock()
				report("pull", res, err)
			}
		}()
	}

	if !opts.Push && !opts.Pull {
		return fmt.Errorf("nothing to watch: enable pushing on double copy, pulling, or both")
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
}

// Describe renders a result for humans, e.g. "1 item from mac-home, 3 min ago".
func (r *ClipResult) Describe(now time.Time) string {
	s := r.Summary
	if r.From != "" {
		s += " from " + r.From
	}
	if !r.At.IsZero() {
		s += ", " + ago(now.Sub(r.At))
	}
	return s
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// defaultMachineID returns a random id kept in ~/.clipsync/machine-id,
// creating it on first use.
func defaultMachineID() string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".clipsync", "machine-id")
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return strings.TrimSpace(string(data))
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(id+"\n"), 0o600)
	return id
}
