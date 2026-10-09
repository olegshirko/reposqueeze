package usecase

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// maxReplayCommits bounds how far back replay walks the GitLab history.
const maxReplayCommits = 1000

// remoteCommits lists GitLab commits after `from` up to `to` (first-parent,
// oldest first) together with the files each of them changed.
func (uc *SyncUseCase) remoteCommits(ctx context.Context, m *entity.Mirror, from, to string) ([]RemoteCommit, error) {
	commits, err := uc.gitlab.ListCommitsAfter(m.ProjectID, to, from, maxReplayCommits)
	if err != nil {
		return nil, fmt.Errorf("cannot list GitLab commits to replay: %w", err)
	}
	out := make([]RemoteCommit, 0, len(commits))
	prev := from
	for _, c := range commits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Diff against the previous first-parent commit, so merge commits
		// bring in everything they merged.
		diffs, err := uc.gitlab.GetCompareDiff(m.ProjectID, prev, c.ID)
		if err != nil {
			return nil, fmt.Errorf("diff of GitLab commit %s: %w", short(c.ID), err)
		}
		out = append(out, RemoteCommit{CommitInfo: c, Changes: remoteChangesFromDiff(diffs)})
		prev = c.ID
	}
	return out, nil
}

// alreadyPicked maps GitLab commit SHAs to local commits created from them
// (e.g. by pull-commit) in the given local range.
func alreadyPicked(git gateway.SyncGit, repoPath, revRange string) (map[string]string, error) {
	refs, err := git.TrailerValues(repoPath, revRange, TrailerReplayedFrom)
	if err != nil {
		return nil, err
	}
	picked := make(map[string]string, len(refs))
	for _, r := range refs {
		if at := strings.LastIndex(r.Value, "@"); at >= 0 {
			picked[r.Value[at+1:]] = r.Commit
		}
	}
	return picked, nil
}

// replay pulls GitLab commits one by one as separate local commits and pushes
// local changes as one GitLab commit.
//
// It is all-or-nothing: every commit is first applied in memory on top of the
// local version of each file (like a cherry-pick). If any step conflicts,
// nothing is changed. Otherwise local changes are pushed first, then the local
// commits are created, so a failed push leaves the repository untouched.
// GitLab commits already brought in with pull-commit are not replayed again.
func (uc *SyncUseCase) replay(ctx context.Context, in SyncInput, set *entity.MirrorSet, m *entity.Mirror,
	plan *SyncPlan, strategy string, res *SyncResult) (*SyncResult, error) {

	base := plan.Base

	// Paths changed locally since the last sync: GitLab changes to them are
	// merged into the local version. Other paths equal GitLab's version at
	// every step, so GitLab's content is taken as is.
	localTouched := map[string]bool{}
	for _, c := range plan.LocalChanges {
		localTouched[c.Path] = true
	}
	for _, c := range plan.Conflicts {
		localTouched[c.Path] = true
	}
	trusted := func(path string) bool { return !localTouched[path] }

	picked, err := alreadyPicked(uc.git, in.RepoPath, base.LocalSHA+".."+plan.LocalHead)
	if err != nil {
		return res, err
	}

	picker := newCherryPicker(uc.git, uc.gitlab, in.RepoPath, m.ProjectID, plan.LocalHead, strategy,
		[3]string{"local " + m.LocalBranch, "gitlab before", "gitlab " + m.RemoteBranch})

	// 1. Apply every GitLab commit in memory.
	var steps []pickStep
	var conflicts []pickConflict
	var earlier []entity.CommitPair
	merged := map[string]bool{}
	prev := base.RemoteSHA
	for _, rc := range plan.RemoteCommits {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if local, ok := picked[rc.ID]; ok {
			earlier = append(earlier, entity.CommitPair{RemoteSHA: rc.ID, LocalSHA: local, Title: commitTitle(rc.CommitInfo) + " (picked earlier)"})
			prev = rc.ID
			continue
		}
		step, stepConflicts, stepMerged, err := picker.apply(rc, prev, trusted)
		if err != nil {
			return res, err
		}
		conflicts = append(conflicts, stepConflicts...)
		for _, p := range stepMerged {
			merged[p] = true
		}
		steps = append(steps, step)
		prev = rc.ID
	}

	if len(conflicts) > 0 {
		var b strings.Builder
		for _, c := range conflicts {
			fmt.Fprintf(&b, "\n  %s %s: %s (%s)", short(c.commit.ID), commitTitle(c.commit.CommitInfo), c.path, c.reason)
		}
		return res, fmt.Errorf("replay stopped before changing anything, GitLab commits conflict with local changes:%s\n"+
			"Use `sync` without --replay to merge them in one commit with conflict markers, "+
			"`pull-commit` to take them one at a time, or --strategy local/remote", b.String())
	}

	// 2. Push local changes: the final local state of every locally touched
	// path that differs from GitLab's head.
	newRemote := plan.RemoteHead
	var touchedPaths []string
	for p := range localTouched {
		touchedPaths = append(touchedPaths, p)
	}
	sort.Strings(touchedPaths)
	var actions []gateway.CommitAction
	for _, p := range touchedPaths {
		final, err := picker.localState(p)
		if err != nil {
			return res, err
		}
		remoteFinal, err := picker.remoteContent(plan.RemoteHead, p, false, false)
		if err != nil {
			return res, err
		}
		switch {
		case sameState(final, remoteFinal):
		case final == nil:
			actions = append(actions, gateway.CommitAction{Action: "delete", FilePath: p})
		case remoteFinal == nil:
			actions = append(actions, gateway.CommitAction{Action: "create", FilePath: p, Content: string(final), Encoding: "text"})
		default:
			actions = append(actions, gateway.CommitAction{Action: "update", FilePath: p, Content: string(final), Encoding: "text"})
		}
	}
	pushed := len(actions) > 0

	// Local commit messages are prepared (and checked by the commit-msg hook)
	// before anything changes.
	origin := m.ProjectName + "/" + m.RemoteBranch
	messages := make([]string, len(steps))
	for i, st := range steps {
		msg := in.Format.Apply(replayMessage(st.commit.CommitInfo, origin))
		if i == len(steps)-1 && !pushed {
			// Without a push this commit matches GitLab exactly: it is the new sync point.
			msg += fmt.Sprintf("\n%s: %s@%s", TrailerRemote, origin, plan.RemoteHead)
		}
		messages[i] = msg
	}
	needMarker := pushed || len(steps) == 0
	markerMsg := func(remote string) string {
		return in.Format.Apply(fmt.Sprintf("sync %s with %s\n\n%s..%s <-> %s..%s\n\n%s: %s@%s",
			m.LocalBranch, origin, short(base.LocalSHA), short(plan.LocalHead), short(base.RemoteSHA), short(plan.RemoteHead),
			TrailerRemote, origin, remote))
	}
	toCheck := append([]string(nil), messages...)
	if needMarker {
		toCheck = append(toCheck, markerMsg(plan.RemoteHead))
	}
	if err := checkMessages(uc.git, in.RepoPath, toCheck); err != nil {
		return res, err
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}

	if pushed {
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("Sync from %s %s..%s", m.LocalBranch, short(base.LocalSHA), short(plan.LocalHead))
		}
		msg += fmt.Sprintf("\n\n%s: %s@%s", TrailerSource, m.LocalBranch, plan.LocalHead)
		created, err := uc.gitlab.CommitFilesViaAPI(strconv.Itoa(m.ProjectID), m.RemoteBranch, msg, actions)
		if err != nil {
			return res, fmt.Errorf("push to GitLab failed, nothing was changed: %w", err)
		}
		res.Pushed = len(actions)
		res.RemoteCommit = created.ID
		newRemote = created.ID
		if len(created.ParentIDs) > 0 && created.ParentIDs[0] != plan.RemoteHead {
			res.RemoteMoved = true
			newRemote = plan.RemoteHead
			res.Warnings = append(res.Warnings, "GitLab branch moved during sync; run sync again to pull the new commits")
		}
		uc.logger.Infof("Pushed %d change(s) to %s as %s", res.Pushed, m.RemoteBranch, short(created.ID))
	}

	// 3. One local commit per GitLab commit, with its message, author and date.
	res.Replayed = append(res.Replayed, earlier...)
	lastLocal := plan.LocalHead
	for i, st := range steps {
		if err := writeStep(in.RepoPath, st); err != nil {
			return res, uc.replayInterrupted(i, len(steps), newRemote, err)
		}
		sha, err := uc.git.CommitPaths(in.RepoPath, messages[i], st.paths, authorOptions(st.commit.CommitInfo))
		if err != nil {
			return res, uc.replayInterrupted(i, len(steps), newRemote, err)
		}
		lastLocal = sha
		res.Pulled += len(st.paths)
		res.Replayed = append(res.Replayed, entity.CommitPair{RemoteSHA: st.commit.ID, LocalSHA: sha, Title: commitTitle(st.commit.CommitInfo)})
		uc.logger.Infof("Replayed %s -> %s %s", short(st.commit.ID), short(sha), firstLine(messages[i]))
	}
	for p := range merged {
		res.Merged = append(res.Merged, p)
	}
	sort.Strings(res.Merged)

	if needMarker {
		// Local commits carry GitLab's changes; GitLab's new commit carries
		// local ones. An empty commit marks the point where both match.
		sha, err := uc.git.CommitPaths(in.RepoPath, markerMsg(newRemote), nil, gateway.CommitOptions{AllowEmpty: true})
		if err != nil {
			return res, uc.replayInterrupted(len(steps), len(steps), newRemote, err)
		}
		lastLocal = sha
	}
	res.LocalCommit = lastLocal

	// 4. Journal.
	entry := entity.JournalEntry{
		SyncPoint:   entity.SyncPoint{LocalSHA: lastLocal, RemoteSHA: newRemote, At: uc.now()},
		Direction:   direction(res),
		LocalFrom:   base.LocalSHA,
		LocalTo:     plan.LocalHead,
		RemoteFrom:  base.RemoteSHA,
		RemoteTo:    plan.RemoteHead,
		Pulled:      res.Pulled,
		Pushed:      res.Pushed,
		Merged:      res.Merged,
		RemoteMoved: res.RemoteMoved,
		Replayed:    res.Replayed,
	}
	m.Journal = append(m.Journal, entry)
	m.PendingMerge = nil
	if err := uc.store.Save(in.RepoPath, set); err != nil {
		return res, fmt.Errorf("sync done but saving mirror state failed (recover with `sync-init --recover --force`): %w", err)
	}
	return res, nil
}

// replayInterrupted reports a failure after GitLab may already have been updated.
func (uc *SyncUseCase) replayInterrupted(done, total int, remote string, err error) error {
	return fmt.Errorf("replay interrupted after %d of %d commit(s); GitLab is at %s. "+
		"Check `git status`, commit what is left and run `sync-init --recover --force`: %w",
		done, total, short(remote), err)
}
