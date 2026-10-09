package usecase

import (
	"fmt"
	"sort"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// Change kinds.
const (
	ChangeAdded    = "added"
	ChangeModified = "modified"
	ChangeDeleted  = "deleted"
)

// FileChange is one changed path on one side of a mirror.
type FileChange struct {
	Path string
	Kind string
}

// Conflict is a path changed on both sides since the last sync.
type Conflict struct {
	Path   string
	Local  string // change kind on the local side
	Remote string // change kind on the GitLab side
}

// RemoteCommit is a GitLab commit with the files it changed (used by replay).
type RemoteCommit struct {
	gateway.CommitInfo
	Changes []FileChange
}

// SyncPlan describes what a sync would transfer.
type SyncPlan struct {
	Mirror        entity.Mirror
	Base          entity.SyncPoint
	LocalHead     string
	RemoteHead    string
	LocalChanges  []FileChange // changed only locally -> pushed
	RemoteChanges []FileChange // changed only on GitLab -> pulled
	Conflicts     []Conflict   // changed on both sides
	PendingFiles  []string     // left unresolved by the previous sync
	// RemoteCommits are the GitLab commits since the last sync, oldest first.
	// Filled only when replay is requested.
	RemoteCommits []RemoteCommit
}

// Empty reports whether there is nothing to transfer.
func (p *SyncPlan) Empty() bool {
	return len(p.LocalChanges) == 0 && len(p.RemoteChanges) == 0 && len(p.Conflicts) == 0 && len(p.RemoteCommits) == 0
}

// Lines renders the plan for humans.
func (p *SyncPlan) Lines() []string {
	lines := []string{
		fmt.Sprintf("Mirror %s", p.Mirror.Name),
		fmt.Sprintf("  last sync:  local %s  <->  gitlab %s", short(p.Base.LocalSHA), short(p.Base.RemoteSHA)),
		fmt.Sprintf("  now:        local %s  <->  gitlab %s", short(p.LocalHead), short(p.RemoteHead)),
	}
	if p.Empty() {
		return append(lines, "  Up to date.")
	}
	section := func(title string, changes []FileChange) {
		if len(changes) == 0 {
			return
		}
		lines = append(lines, fmt.Sprintf("  %s (%d):", title, len(changes)))
		for _, c := range changes {
			lines = append(lines, fmt.Sprintf("    %-8s %s", c.Kind, c.Path))
		}
	}
	if len(p.RemoteCommits) > 0 {
		lines = append(lines, fmt.Sprintf("  replay GitLab commits one by one (%d):", len(p.RemoteCommits)))
		for _, c := range p.RemoteCommits {
			lines = append(lines, fmt.Sprintf("    %s %s  [%s, %d file(s)]", short(c.ID), commitTitle(c.CommitInfo), c.AuthorName, len(c.Changes)))
		}
	}
	section("pull from GitLab", p.RemoteChanges)
	section("push to GitLab", p.LocalChanges)
	if len(p.Conflicts) > 0 {
		lines = append(lines, fmt.Sprintf("  changed on both sides (%d):", len(p.Conflicts)))
		for _, c := range p.Conflicts {
			lines = append(lines, fmt.Sprintf("    %s (local: %s, gitlab: %s)", c.Path, c.Local, c.Remote))
		}
	}
	if len(p.PendingFiles) > 0 {
		lines = append(lines, fmt.Sprintf("  resolved after previous sync, will be pushed: %s", strings.Join(p.PendingFiles, ", ")))
	}
	return lines
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

// localChangesFromDiff converts `git diff --name-status --no-renames` output.
func localChangesFromDiff(files []gateway.CommitFileInfo) []FileChange {
	var out []FileChange
	for _, f := range files {
		kind := ChangeModified
		switch {
		case f.Status == "A":
			kind = ChangeAdded
		case f.Status == "D":
			kind = ChangeDeleted
		}
		out = append(out, FileChange{Path: f.Path, Kind: kind})
	}
	return out
}

// remoteChangesFromDiff converts GitLab diff entries; renames become delete + add.
func remoteChangesFromDiff(diffs []gateway.DiffEntry) []FileChange {
	var out []FileChange
	for _, d := range diffs {
		switch {
		case d.DeletedFile:
			out = append(out, FileChange{Path: d.NewPath, Kind: ChangeDeleted})
		case d.RenamedFile && d.OldPath != d.NewPath:
			out = append(out, FileChange{Path: d.OldPath, Kind: ChangeDeleted})
			out = append(out, FileChange{Path: d.NewPath, Kind: ChangeAdded})
		case d.NewFile:
			out = append(out, FileChange{Path: d.NewPath, Kind: ChangeAdded})
		default:
			out = append(out, FileChange{Path: d.NewPath, Kind: ChangeModified})
		}
	}
	return out
}

// splitChanges separates one-sided changes from paths touched on both sides.
// Paths deleted on both sides need no action and are dropped.
func splitChanges(local, remote []FileChange) (onlyLocal, onlyRemote []FileChange, conflicts []Conflict) {
	remoteByPath := make(map[string]FileChange, len(remote))
	for _, c := range remote {
		remoteByPath[c.Path] = c
	}
	localByPath := make(map[string]FileChange, len(local))
	for _, c := range local {
		localByPath[c.Path] = c
	}

	for _, l := range local {
		r, both := remoteByPath[l.Path]
		switch {
		case !both:
			onlyLocal = append(onlyLocal, l)
		case l.Kind == ChangeDeleted && r.Kind == ChangeDeleted:
			// same outcome on both sides
		default:
			conflicts = append(conflicts, Conflict{Path: l.Path, Local: l.Kind, Remote: r.Kind})
		}
	}
	for _, r := range remote {
		if _, both := localByPath[r.Path]; !both {
			onlyRemote = append(onlyRemote, r)
		}
	}

	sortChanges(onlyLocal)
	sortChanges(onlyRemote)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	return onlyLocal, onlyRemote, conflicts
}

func sortChanges(c []FileChange) {
	sort.Slice(c, func(i, j int) bool { return c[i].Path < c[j].Path })
}

// commitTitle returns the first line of a commit message.
func commitTitle(c gateway.CommitInfo) string {
	if c.Title != "" {
		return c.Title
	}
	title, _, _ := strings.Cut(c.Message, "\n")
	return title
}
