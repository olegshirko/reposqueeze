package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/olegshirko/reposqueeze/internal/pkg/opensslenc"
)

// DefaultClipProject is the GitLab project that holds the shared clipboard.
const DefaultClipProject = "clipboard"

const maxClipBytes = 64 << 20

// ErrNoClip means nothing has been pushed yet.
var ErrNoClip = gateway.ErrNoClip

// ClipStore keeps the single, latest encrypted clipboard.
type ClipStore = gateway.ClipStore

// ClipConfig holds the shared secret.
type ClipConfig struct {
	KeyFile string // the same file on both Macs
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
}

// ClipUseCase shares the clipboard between machines: the packed clipboard
// is encrypted with a shared key and kept in a ClipStore, which only ever
// sees ciphertext and holds just the latest copy.
type ClipUseCase struct {
	store     ClipStore
	clipboard gateway.Clipboard
	logger    logger.Logger
	hostname  func() (string, error)
	machineID func() string
	now       func() time.Time
}

// NewClipUseCase creates a ClipUseCase.
func NewClipUseCase(store ClipStore, clipboard gateway.Clipboard, log logger.Logger) *ClipUseCase {
	return &ClipUseCase{store: store, clipboard: clipboard, logger: log,
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
	host, _ := uc.hostname()
	res := &ClipResult{Summary: summary, From: host, At: uc.now()}
	meta, err := json.Marshal(clipMeta{Host: host, Machine: uc.machineID(), Summary: summary, At: res.At})
	if err != nil {
		return nil, err
	}
	if err := uc.store.Put(ctx, blob, meta); err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}
	return res, nil
}

// Pull replaces the clipboard with the last pushed one.
func (uc *ClipUseCase) Pull(ctx context.Context, cfg ClipConfig) (*ClipResult, error) {
	pass, err := uc.password(cfg)
	if err != nil {
		return nil, err
	}
	blob, meta, err := uc.store.Get(ctx)
	if err != nil {
		return nil, err
	}
	return uc.apply(pass, blob, parseMeta(meta))
}

func (uc *ClipUseCase) apply(pass string, blob []byte, meta clipMeta) (*ClipResult, error) {
	payload, err := opensslenc.Decrypt(pass, blob)
	if err != nil {
		return nil, err
	}
	summary, err := uc.clipboard.Import(payload)
	if err != nil {
		return nil, fmt.Errorf("%w (is the key file the same on both Macs?)", err)
	}
	return &ClipResult{Summary: summary, From: meta.Host, At: meta.At}, nil
}

func parseMeta(data []byte) clipMeta {
	var m clipMeta
	_ = json.Unmarshal(data, &m)
	return m
}

// ClipWatchOptions selects what the watcher does.
type ClipWatchOptions struct {
	Push     bool          // push when the same thing is copied twice (⌘C ⌘C)
	Window   time.Duration // max time between the two copies
	Pull     bool          // put clipboards pushed by other machines into this one
	Interval time.Duration // how often to check for a new clipboard
	// OnEvent reports what happened (for logs and notifications).
	OnEvent func(kind string, res *ClipResult, err error)
}

// Watch runs until ctx is done: it pushes on double copy and/or pulls
// clipboards pushed from other machines.
func (uc *ClipUseCase) Watch(ctx context.Context, cfg ClipConfig, opts ClipWatchOptions) error {
	pass, err := uc.password(cfg)
	if err != nil {
		return err
	}
	if !opts.Push && !opts.Pull {
		return fmt.Errorf("nothing to watch: enable pushing on double copy, pulling, or both")
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
			seen, _ := uc.store.Version(ctx)
			t := time.NewTicker(opts.Interval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					errc <- nil
					return
				case <-t.C:
				}
				v, err := uc.store.Version(ctx)
				if err != nil || v == "" || v == seen {
					continue
				}
				seen = v
				uc.pullNew(ctx, pass, self, &mu, report)
			}
		}()
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

// pullNew applies the stored clipboard unless this machine pushed it.
func (uc *ClipUseCase) pullNew(ctx context.Context, pass, self string, mu *sync.Mutex,
	report func(string, *ClipResult, error)) {
	mu.Lock()
	defer mu.Unlock()
	blob, meta, err := uc.store.Get(ctx)
	if err != nil {
		report("pull", nil, err)
		return
	}
	m := parseMeta(meta)
	if m.Machine == self {
		return // our own push
	}
	res, err := uc.apply(pass, blob, m)
	report("pull", res, err)
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
