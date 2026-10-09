package usecase

import (
	"bytes"
	"fmt"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// fileState is the content of a file at some point; nil means "absent".
type fileState = []byte

func sameState(a, b fileState) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return bytes.Equal(a, b)
}

// pickStep is one GitLab commit turned into new local file states.
type pickStep struct {
	commit RemoteCommit
	paths  []string
	states map[string]fileState
}

// pickConflict is a file whose GitLab change could not be applied cleanly.
type pickConflict struct {
	commit RemoteCommit
	path   string
	reason string
	// content is what to leave in the working tree for manual resolution
	// (conflict-marked text or GitLab's version); nil keeps the local file.
	content fileState
}

// cherryPicker applies GitLab commits, in memory, on top of local files the
// way `git cherry-pick` would: GitLab's change (parent -> commit) is
// 3-way-merged into the current local version of each file.
type cherryPicker struct {
	git       gateway.SyncGit
	gitlab    gateway.GitLabGateway
	repoPath  string
	projectID int
	localHead string
	strategy  string
	labels    [3]string // ours, base, theirs markers

	remote  map[string]fileState // "<sha>\x00<path>" -> content
	current map[string]fileState // local state as commits are applied
}

func newCherryPicker(git gateway.SyncGit, gitlab gateway.GitLabGateway, repoPath string, projectID int,
	localHead, strategy string, labels [3]string) *cherryPicker {
	return &cherryPicker{
		git: git, gitlab: gitlab, repoPath: repoPath, projectID: projectID,
		localHead: localHead, strategy: strategy, labels: labels,
		remote: map[string]fileState{}, current: map[string]fileState{},
	}
}

// remoteContent fetches path at sha. When known is true, exists tells whether
// the file is there (taken from a diff), saving a request.
func (p *cherryPicker) remoteContent(sha, path string, known, exists bool) (fileState, error) {
	key := sha + "\x00" + path
	if st, ok := p.remote[key]; ok {
		return st, nil
	}
	var err error
	if !known {
		if exists, err = p.gitlab.FileExists(p.projectID, path, sha); err != nil {
			return nil, err
		}
	}
	var st fileState
	if exists {
		if st, err = p.gitlab.GetRawFile(p.projectID, path, sha); err != nil {
			return nil, fmt.Errorf("failed to download %q at %s: %w", path, short(sha), err)
		}
		if st == nil {
			st = []byte{}
		}
	}
	p.remote[key] = st
	return st, nil
}

// localState returns the local content of path, including changes applied so far.
func (p *cherryPicker) localState(path string) (fileState, error) {
	if st, ok := p.current[path]; ok {
		return st, nil
	}
	content, found, err := p.git.FileAtRef(p.repoPath, p.localHead, path)
	if err != nil {
		return nil, err
	}
	var st fileState
	if found {
		st = content
		if st == nil {
			st = []byte{}
		}
	}
	p.current[path] = st
	return st, nil
}

// apply computes the local file states after applying rc, whose parent is
// `parent`. trusted(path) may report that the local file is known to equal
// GitLab's version at parent, so GitLab's new version is taken directly.
// Conflicting paths are returned separately and left out of the step.
func (p *cherryPicker) apply(rc RemoteCommit, parent string, trusted func(string) bool) (pickStep, []pickConflict, []string, error) {
	step := pickStep{commit: rc, states: map[string]fileState{}}
	var conflicts []pickConflict
	var merged []string

	for _, ch := range rc.Changes {
		theirs, err := p.remoteContent(rc.ID, ch.Path, true, ch.Kind != ChangeDeleted)
		if err != nil {
			return step, nil, nil, err
		}
		ours, err := p.localState(ch.Path)
		if err != nil {
			return step, nil, nil, err
		}

		next := theirs
		if trusted == nil || !trusted(ch.Path) {
			// Only added files did not exist at the parent.
			before, err := p.remoteContent(parent, ch.Path, true, ch.Kind != ChangeAdded)
			if err != nil {
				return step, nil, nil, err
			}
			res, err := p.pickFile(ours, before, theirs)
			if err != nil {
				return step, nil, nil, err
			}
			if res.reason != "" {
				conflicts = append(conflicts, pickConflict{commit: rc, path: ch.Path, reason: res.reason, content: res.content})
				continue
			}
			next = res.content
			if res.merged {
				merged = append(merged, ch.Path)
			}
		}
		if sameState(next, ours) {
			continue
		}
		p.current[ch.Path] = next
		step.states[ch.Path] = next
		step.paths = append(step.paths, ch.Path)
	}
	return step, conflicts, merged, nil
}

type pickResult struct {
	content fileState // new state, or what to leave for resolution on conflict
	merged  bool      // both sides contributed
	reason  string    // non-empty on conflict
}

// pickFile applies GitLab's change before->theirs to the local version ours.
func (p *cherryPicker) pickFile(ours, before, theirs fileState) (pickResult, error) {
	switch {
	case sameState(ours, theirs):
		return pickResult{content: ours}, nil
	case sameState(ours, before):
		return pickResult{content: theirs}, nil
	}

	switch p.strategy {
	case StrategyLocal:
		return pickResult{content: ours}, nil
	case StrategyRemote:
		return pickResult{content: theirs}, nil
	case StrategyAbort:
		return pickResult{reason: "changed on both sides", content: theirs}, nil
	}

	switch {
	case ours == nil:
		return pickResult{reason: "deleted locally, changed on GitLab", content: theirs}, nil
	case theirs == nil:
		return pickResult{reason: "changed locally, deleted on GitLab"}, nil
	case isBinary(ours) || isBinary(theirs) || isBinary(before):
		return pickResult{reason: "binary file changed on both sides", content: theirs}, nil
	}
	baseContent := before
	if baseContent == nil {
		baseContent = []byte{}
	}
	result, err := p.git.MergeFile(ours, baseContent, theirs, p.labels)
	if err != nil {
		return pickResult{}, err
	}
	if result.Conflicts {
		return pickResult{reason: "merge conflict", content: result.Content}, nil
	}
	return pickResult{content: result.Content, merged: true}, nil
}

// writeStep puts a step's file states into the working tree.
func writeStep(repoPath string, st pickStep) error {
	for _, p := range st.paths {
		var err error
		if st.states[p] == nil {
			err = removeLocal(repoPath, p)
		} else {
			err = writeLocalFile(repoPath, p, st.states[p])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// replayMessage is the original GitLab commit message.
func replayMessage(c gateway.CommitInfo) string {
	if msg := trimMessage(c.Message); msg != "" {
		return msg
	}
	return commitTitle(c)
}

func trimMessage(s string) string {
	return string(bytes.TrimRight([]byte(s), "\n"))
}

// dateOptions keeps the GitLab commit's date; a non-zero `at` replaces it
// (author and committer) for spread-out history. The author itself is not
// carried over: commits are authored by the local git config user.
func dateOptions(c gateway.CommitInfo, at time.Time) gateway.CommitOptions {
	opts := gateway.CommitOptions{
		AllowEmpty: true,
		AuthorDate: c.AuthoredDate,
	}
	if !at.IsZero() {
		opts.AuthorDate = at.Format(time.RFC3339)
		opts.CommitterDate = opts.AuthorDate
	}
	return opts
}

// spreadDates returns n dates from spread, or n zero times without one.
func spreadDates(spread *DateSpread, n int) ([]time.Time, error) {
	if spread == nil {
		return make([]time.Time, n), nil
	}
	return spread.Schedule(n, time.Now())
}
