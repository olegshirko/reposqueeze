// Package clipstore keeps the encrypted shared clipboard in a git branch.
package clipstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

const (
	blobFile = "clip.bin"
	metaFile = "meta.json"
)

// GitStore keeps the clipboard as the only commit of one branch of a git
// remote, reached with the user's SSH key (no API token). Every Put
// force-pushes a fresh parentless commit, so no history accumulates. On
// GitLab, pushing to a project that does not exist yet creates it as private.
type GitStore struct {
	Remote string // e.g. git@gitlab.com:user/clipboard.git
	Branch string
	Dir    string // local bare repository used as a cache
	// Warnf reports non-fatal problems (optional).
	Warnf func(format string, args ...interface{})
}

var _ gateway.ClipStore = (*GitStore)(nil)

// NewGitStore creates the store; dir is created on first use.
func NewGitStore(remote, branch, dir string) *GitStore {
	if branch == "" {
		branch = "clip"
	}
	return &GitStore{Remote: remote, Branch: branch, Dir: dir}
}

func (s *GitStore) git(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--git-dir", s.Dir}, args...)...)
	// Fixed identity: the commit only carries the clipboard.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=clipsync", "GIT_AUTHOR_EMAIL=clipsync@localhost",
		"GIT_COMMITTER_NAME=clipsync", "GIT_COMMITTER_EMAIL=clipsync@localhost",
		"GIT_TERMINAL_PROMPT=0")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (s *GitStore) init(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(s.Dir, "HEAD")); err == nil {
		return nil
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "init", "-q", "--bare", s.Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %v: %s", err, out)
	}
	return nil
}

// Put force-pushes a single commit holding the clipboard.
func (s *GitStore) Put(ctx context.Context, blob, meta []byte) error {
	if err := s.init(ctx); err != nil {
		return err
	}
	blobSHA, err := s.git(ctx, blob, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	metaSHA, err := s.git(ctx, meta, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	tree := fmt.Sprintf("100644 blob %s\t%s\n100644 blob %s\t%s\n",
		strings.TrimSpace(string(blobSHA)), blobFile, strings.TrimSpace(string(metaSHA)), metaFile)
	treeSHA, err := s.git(ctx, []byte(tree), "mktree")
	if err != nil {
		return err
	}
	tree = strings.TrimSpace(string(treeSHA))
	if err := s.ensureDefaultBranch(ctx); err != nil {
		return err
	}
	commit, err := s.git(ctx, nil, "commit-tree", tree, "-m", "clip")
	if err != nil {
		return err
	}
	_, err = s.git(ctx, nil, "push", "--quiet", "--force", s.Remote,
		strings.TrimSpace(string(commit))+":refs/heads/"+s.Branch)
	if err != nil && protectedRe.MatchString(err.Error()) {
		err = s.pushOnTop(ctx, tree)
	}
	if err != nil {
		return err
	}
	// Local objects are only a cache: keep the cache from growing.
	_, _ = s.git(ctx, nil, "gc", "--quiet", "--prune=now")
	return nil
}

var protectedRe = regexp.MustCompile(`(?i)protected branch|not allowed to force push|non-fast-forward`)

// ensureDefaultBranch gives a new, empty project a "main" branch first, so
// that GitLab makes it the default (protected) branch instead of the clip
// branch, which must accept force pushes.
func (s *GitStore) ensureDefaultBranch(ctx context.Context) error {
	out, err := s.git(ctx, nil, "ls-remote", "--heads", s.Remote)
	if err != nil && !notThere.MatchString(err.Error()) {
		return err
	}
	if strings.TrimSpace(string(out)) != "" {
		return nil // the project already has branches
	}
	emptyTree, err := s.git(ctx, []byte{}, "mktree")
	if err != nil {
		return err
	}
	init, err := s.git(ctx, nil, "commit-tree", strings.TrimSpace(string(emptyTree)),
		"-m", "Shared clipboard storage (data lives in branch "+s.Branch+", encrypted)")
	if err != nil {
		return err
	}
	_, err = s.git(ctx, nil, "push", "--quiet", s.Remote, strings.TrimSpace(string(init))+":refs/heads/main")
	return err
}

// pushOnTop is the fallback for a branch that refuses force pushes: the new
// commit gets the current one as its parent. History then accumulates.
func (s *GitStore) pushOnTop(ctx context.Context, tree string) error {
	ref := "refs/clip/latest"
	if _, err := s.git(ctx, nil, "fetch", "--quiet", "--depth", "1", "--force", s.Remote,
		"refs/heads/"+s.Branch+":"+ref); err != nil {
		return err
	}
	commit, err := s.git(ctx, nil, "commit-tree", tree, "-p", ref, "-m", "clip")
	if err != nil {
		return err
	}
	if _, err := s.git(ctx, nil, "push", "--quiet", s.Remote,
		strings.TrimSpace(string(commit))+":refs/heads/"+s.Branch); err != nil {
		return err
	}
	if s.Warnf != nil {
		s.Warnf("branch %s is protected from force pushes, so old clipboards stay in its history; "+
			"unprotect it in GitLab (Settings > Repository > Protected branches)", s.Branch)
	}
	return nil
}

var notThere = regexp.MustCompile(`(?i)(could not read from remote|not found|does not appear to be a git repository|couldn't find remote ref|does not exist)`)

// Version returns the commit the branch points to ("" if nothing pushed).
func (s *GitStore) Version(ctx context.Context) (string, error) {
	if err := s.init(ctx); err != nil {
		return "", err
	}
	out, err := s.git(ctx, nil, "ls-remote", s.Remote, "refs/heads/"+s.Branch)
	if err != nil {
		if notThere.MatchString(err.Error()) {
			return "", nil
		}
		return "", err
	}
	sha, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	return sha, nil
}

// Get fetches the branch and reads the clipboard from it.
func (s *GitStore) Get(ctx context.Context) ([]byte, []byte, error) {
	if err := s.init(ctx); err != nil {
		return nil, nil, err
	}
	ref := "refs/clip/latest"
	if _, err := s.git(ctx, nil, "fetch", "--quiet", "--depth", "1", "--force", s.Remote,
		"refs/heads/"+s.Branch+":"+ref); err != nil {
		if notThere.MatchString(err.Error()) {
			return nil, nil, fmt.Errorf("%w (%s, branch %s)", gateway.ErrNoClip, s.Remote, s.Branch)
		}
		return nil, nil, err
	}
	blob, err := s.git(ctx, nil, "cat-file", "blob", ref+":"+blobFile)
	if err != nil {
		return nil, nil, err
	}
	meta, _ := s.git(ctx, nil, "cat-file", "blob", ref+":"+metaFile)
	return blob, meta, nil
}

var welcomeRe = regexp.MustCompile(`Welcome to GitLab, @([^!\s]+)!`)

// GitLabUser asks `ssh -T git@<host>` who the SSH key belongs to.
func GitLabUser(ctx context.Context, host string) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-T", "git@"+host)
	out, _ := cmd.CombinedOutput() // exits non-zero even on success
	if m := welcomeRe.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = "no answer"
	}
	return "", errors.New("cannot log in to " + host + " over SSH: " + msg)
}
