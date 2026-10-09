package entity

import (
	"fmt"
	"time"
)

// SyncPoint pairs a local commit with the GitLab commit that has the same content.
type SyncPoint struct {
	LocalSHA  string    `json:"local_sha"`
	RemoteSHA string    `json:"remote_sha"`
	At        time.Time `json:"at"`
}

// Journal entry directions.
const (
	SyncInit     = "init"
	SyncPull     = "pull"
	SyncPush     = "push"
	SyncBoth     = "both"
	SyncConflict = "conflict" // nothing transferred, only conflicts left to resolve
)

// CommitPair links a GitLab commit to the local commit it was replayed as.
type CommitPair struct {
	RemoteSHA string `json:"remote_sha"`
	LocalSHA  string `json:"local_sha"`
	Title     string `json:"title,omitempty"`
}

// JournalEntry records one synchronisation. LocalSHA/RemoteSHA are the
// resulting matched pair; the *From fields describe what was transferred.
type JournalEntry struct {
	SyncPoint
	Direction   string   `json:"dir"`
	LocalFrom   string   `json:"local_from,omitempty"`
	LocalTo     string   `json:"local_to,omitempty"`
	RemoteFrom  string   `json:"remote_from,omitempty"`
	RemoteTo    string   `json:"remote_to,omitempty"`
	Pulled      int      `json:"pulled"`
	Pushed      int      `json:"pushed"`
	Merged      []string `json:"merged,omitempty"`
	Conflicts   []string `json:"conflicts,omitempty"`
	RemoteMoved bool     `json:"remote_moved,omitempty"`
	// Replayed lists GitLab commits pulled one by one (sync --replay).
	Replayed []CommitPair `json:"replayed,omitempty"`
}

// PendingMerge lists files left with conflict markers by the last sync.
type PendingMerge struct {
	RemoteSHA string    `json:"remote_sha"`
	Files     []string  `json:"files"`
	At        time.Time `json:"at"`
}

// Mirror links a local branch to a branch of a GitLab project and remembers
// which commits on both sides correspond to each other. Commits created on
// GitLab via the API never share SHAs with local ones, so the correspondence
// is kept explicitly.
type Mirror struct {
	Name         string         `json:"name"`
	LocalBranch  string         `json:"local_branch"`
	ProjectID    int            `json:"project_id"`
	ProjectName  string         `json:"project_name"`
	RemoteBranch string         `json:"remote_branch"`
	Origin       SyncPoint      `json:"origin"`
	Journal      []JournalEntry `json:"journal,omitempty"`
	PendingMerge *PendingMerge  `json:"pending_merge,omitempty"`
	// CommitFormat is the last commit type/task used for local sync commits.
	CommitFormat CommitFormat `json:"commit_format,omitempty"`
}

// DefaultMirrorName builds the conventional mirror name.
func DefaultMirrorName(localBranch, project, remoteBranch string) string {
	return fmt.Sprintf("%s->%s:%s", localBranch, project, remoteBranch)
}

// Current returns the latest matched pair of commits.
func (m *Mirror) Current() SyncPoint {
	if n := len(m.Journal); n > 0 {
		return m.Journal[n-1].SyncPoint
	}
	return m.Origin
}

// PendingPick is a GitLab commit whose pull stopped on conflicts; the first
// commit the user makes after Base is taken as its local counterpart.
type PendingPick struct {
	CommitPair
	Base string `json:"base"` // local HEAD when the pick stopped
}

// MirrorSet is everything stored for one local repository.
type MirrorSet struct {
	Version int      `json:"version"`
	Mirrors []Mirror `json:"mirrors"`
	// Picked records GitLab commits brought in as local commits
	// (pull-commit, sync --replay), so they are not brought in twice.
	Picked      []CommitPair `json:"picked,omitempty"`
	PendingPick *PendingPick `json:"pending_pick,omitempty"`
}

// AddPicked records GitLab -> local commit pairs, replacing older records
// of the same GitLab commit.
func (s *MirrorSet) AddPicked(pairs ...CommitPair) {
	for _, p := range pairs {
		replaced := false
		for i := range s.Picked {
			if s.Picked[i].RemoteSHA == p.RemoteSHA {
				s.Picked[i] = p
				replaced = true
			}
		}
		if !replaced {
			s.Picked = append(s.Picked, p)
		}
	}
}

// Find returns the mirror with the given name, or, when name is empty, the only
// mirror of localBranch (or the only mirror at all when localBranch is empty).
func (s *MirrorSet) Find(name, localBranch string) (*Mirror, error) {
	if name != "" {
		for i := range s.Mirrors {
			if s.Mirrors[i].Name == name {
				return &s.Mirrors[i], nil
			}
		}
		return nil, fmt.Errorf("mirror %q not found", name)
	}

	var candidates []*Mirror
	for i := range s.Mirrors {
		if localBranch == "" || s.Mirrors[i].LocalBranch == localBranch {
			candidates = append(candidates, &s.Mirrors[i])
		}
	}
	switch len(candidates) {
	case 0:
		if localBranch != "" {
			return nil, fmt.Errorf("no mirror configured for branch %q; run sync-init first", localBranch)
		}
		return nil, fmt.Errorf("no mirrors configured; run sync-init first")
	case 1:
		return candidates[0], nil
	default:
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = c.Name
		}
		return nil, fmt.Errorf("several mirrors match, choose one with --mirror: %v", names)
	}
}

// Upsert inserts the mirror or replaces the one with the same name.
func (s *MirrorSet) Upsert(m Mirror) {
	for i := range s.Mirrors {
		if s.Mirrors[i].Name == m.Name {
			s.Mirrors[i] = m
			return
		}
	}
	s.Mirrors = append(s.Mirrors, m)
}
