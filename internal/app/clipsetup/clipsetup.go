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
		return clipstore.NewGitStore(remote, "clip", filepath.Join(Dir(), "git")), remote, nil
	default:
		return nil, "", fmt.Errorf("unknown transport %q (ssh or api)", o.Transport)
	}
}

// remote returns the git remote, detecting the GitLab user over SSH once and
// remembering it in ~/.clipsync/remote.
func remote(ctx context.Context, o Options) (string, error) {
	if o.Remote != "" {
		return o.Remote, nil
	}
	cache := filepath.Join(Dir(), "remote")
	if data, err := os.ReadFile(cache); err == nil {
		if r := strings.TrimSpace(string(data)); r != "" {
			return r, nil
		}
	}
	host := "gitlab.com"
	if u, err := url.Parse(o.BaseURL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	user, err := clipstore.GitLabUser(ctx, host)
	if err != nil {
		return "", err
	}
	r := fmt.Sprintf("git@%s:%s/%s.git", host, user, o.Project)
	_ = os.MkdirAll(Dir(), 0o700)
	_ = os.WriteFile(cache, []byte(r+"\n"), 0o600)
	return r, nil
}
