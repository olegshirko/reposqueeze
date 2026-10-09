// Package clipsetup wires the shared-clipboard store from user settings.
package clipsetup

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/clipstore"
)

// Transports.
const (
	TransportSSH = "ssh" // git over SSH with the user's key (default, no token)
	TransportAPI = "api" // GitLab generic package registry (needs GITLAB_TOKEN)
)

// Options selects where the clipboard is stored.
type Options struct {
	Transport string
	Project   string // GitLab project name (default "clipboard")
	Remote    string // ssh: explicit git remote; detected when empty
	BaseURL   string // GitLab URL; its host is used for SSH (default gitlab.com)
	API       usecase.ClipGitLab
	Warnf     func(format string, args ...interface{}) // non-fatal problems (optional)
	// Token, when set, makes git use HTTPS with this token instead of SSH
	// (for networks where only HTTPS reaches GitLab).
	Token string
	// CurrentUser returns the token owner's username (used with Token).
	CurrentUser func() (string, error)
}

// Dir is where clipsync keeps its files.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clipsync")
}

// KeyFile is the shared key: $REPOSQUEEZE_CLIP_KEY or ~/.clipsync/key.
func KeyFile() string {
	if p := os.Getenv("REPOSQUEEZE_CLIP_KEY"); p != "" {
		return p
	}
	return filepath.Join(Dir(), "key")
}

// Store builds the clipboard store.
func Store(ctx context.Context, o Options) (usecase.ClipStore, string, error) {
	if o.Project == "" {
		o.Project = usecase.DefaultClipProject
	}
	switch o.Transport {
	case TransportAPI:
		if o.API == nil {
			return nil, "", fmt.Errorf("the api transport needs GITLAB_TOKEN")
		}
		return usecase.NewPackageClipStore(o.API, o.Project), "GitLab packages of project " + o.Project, nil
	case "", TransportSSH:
		remote, err := remote(ctx, o)
		if err != nil {
			return nil, "", err
		}
		store := clipstore.NewGitStore(remote, "clip", filepath.Join(Dir(), "git"))
		store.Warnf = o.Warnf
		store.Token = o.Token
		return store, remote, nil
	default:
		return nil, "", fmt.Errorf("unknown transport %q (ssh or api)", o.Transport)
	}
}

// remote returns the git remote and remembers it in ~/.clipsync/remote
// (SSH) or ~/.clipsync/remote-https.
// With a token it is an HTTPS URL (user from the API); without one, an SSH
// URL (user from `ssh -T`).
func remote(ctx context.Context, o Options) (string, error) {
	if o.Remote != "" {
		return o.Remote, nil
	}
	https := o.Token != "" && o.CurrentUser != nil
	// Separate caches: the same Mac may run with a token (terminal) and
	// without one (LaunchAgent), and each needs its own remote.
	cache := filepath.Join(Dir(), "remote")
	if https {
		cache = filepath.Join(Dir(), "remote-https")
	}
	if data, err := os.ReadFile(cache); err == nil {
		if r := strings.TrimSpace(string(data)); r != "" {
			return r, nil
		}
	}
	scheme, host := "https", "gitlab.com"
	if u, err := url.Parse(o.BaseURL); err == nil && u.Hostname() != "" {
		host = u.Host
		if u.Scheme != "" {
			scheme = u.Scheme
		}
	}

	var r string
	if https {
		user, err := o.CurrentUser()
		if err != nil {
			return "", fmt.Errorf("cannot detect the GitLab user with the token: %w", err)
		}
		r = fmt.Sprintf("%s://%s/%s/%s.git", scheme, host, user, o.Project)
	} else {
		user, err := clipstore.GitLabUser(ctx, strings.Split(host, ":")[0])
		if err != nil {
			return "", fmt.Errorf("%w; if only HTTPS reaches GitLab, set GITLAB_TOKEN", err)
		}
		r = fmt.Sprintf("git@%s:%s/%s.git", strings.Split(host, ":")[0], user, o.Project)
	}
	_ = os.MkdirAll(Dir(), 0o700)
	_ = os.WriteFile(cache, []byte(r+"\n"), 0o600)
	return r, nil
}
