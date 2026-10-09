package usecase

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// resetHint tells how to re-establish the sync point manually.
const resetHint = "set the sync point again with `sync-init --local-sha <local commit> --remote-sha <gitlab commit> --force`"

// Conflict strategies.
const (
	StrategyMerge  = "merge"
	StrategyLocal  = "local"
	StrategyRemote = "remote"
	StrategyAbort  = "abort"
)

var conflictMarker = []byte("<<<<<<< ")

// SyncUseCase keeps a local branch and a GitLab branch in sync in both directions.
type SyncUseCase struct {
	git    gateway.SyncGit
	gitlab gateway.GitLabGateway
	store  gateway.MirrorStore
	logger logger.Logger
	now    func() time.Time
}

// NewSyncUseCase creates a new SyncUseCase.
func NewSyncUseCase(git gateway.SyncGit, gitlab gateway.GitLabGateway, store gateway.MirrorStore, log logger.Logger) *SyncUseCase {
	return &SyncUseCase{git: git, gitlab: gitlab, store: store, logger: log, now: func() time.Time { return time.Now().UTC() }}
}

// ---------------------------------------------------------------- init

// SyncInitInput configures a new mirror.
type SyncInitInput struct {
	RepoPath     string
	Name         string // default: <local>-><project>:<remote>
	LocalBranch  string // default: current branch
	RemoteBranch string // default: same as local branch
	LocalSHA     string // local commit the mirroring starts from (default: local branch head)
	RemoteSHA    string // GitLab commit with the same content (default: remote branch head)
	Force        bool   // overwrite an existing mirror with the same name
}

// Init registers a mirror: which local commit on which branch corresponds to
// which GitLab commit. Every later sync starts from the latest such pair.
func (uc *SyncUseCase) Init(ctx context.Context, in SyncInitInput) (*entity.Mirror, error) {
	project, err := resolveProject(uc.gitlab, in.RepoPath)
	if err != nil {
		return nil, err
	}

	localBranch := in.LocalBranch
	if localBranch == "" {
		if localBranch, err = uc.git.CurrentBranch(in.RepoPath); err != nil {
			return nil, fmt.Errorf("cannot detect current branch, pass --local-branch: %w", err)
		}
	}
	remoteBranch := in.RemoteBranch
	if remoteBranch == "" {
		remoteBranch = localBranch
	}
	name := in.Name
	if name == "" {
		name = entity.DefaultMirrorName(localBranch, project.Name, remoteBranch)
	}

	set, err := uc.store.Load(in.RepoPath)
	if err != nil {
		return nil, err
	}
	if existing, _ := set.Find(name, ""); existing != nil && !in.Force {
		return nil, fmt.Errorf("mirror %q already exists (last sync local %s <-> gitlab %s); use --force to reset it",
			name, short(existing.Current().LocalSHA), short(existing.Current().RemoteSHA))
	}

	localRef, remoteRef := in.LocalSHA, in.RemoteSHA
	if localRef == "" {
		localRef = localBranch
	}

	localSHA, err := uc.git.RevParse(in.RepoPath, localRef)
	if err != nil {
		return nil, fmt.Errorf("unknown local commit %q: %w", localRef, err)
	}
	if remoteRef == "" {
		remoteRef = remoteBranch
	}
	remoteSHA, err := uc.resolveRemoteCommit(project.ID, remoteRef)
	if err != nil {
		return nil, err
	}

	m := entity.Mirror{
		Name:         name,
		LocalBranch:  localBranch,
		ProjectID:    project.ID,
		ProjectName:  project.Name,
		RemoteBranch: remoteBranch,
		Origin:       entity.SyncPoint{LocalSHA: localSHA, RemoteSHA: remoteSHA, At: uc.now()},
	}
	set.Upsert(m)
	if err := uc.store.Save(in.RepoPath, set); err != nil {
		return nil, err
	}
	uc.logger.Infof("Mirror %s: local %s@%s <-> gitlab %s@%s", name, localBranch, short(localSHA), remoteBranch, short(remoteSHA))
	return &m, nil
}

// resolveRemoteCommit turns a branch name or (short) SHA into a full GitLab commit SHA.
func (uc *SyncUseCase) resolveRemoteCommit(projectID int, ref string) (string, error) {
	commits, err := uc.gitlab.GetCommits(projectID, ref, 1)
	if err != nil {
		return "", fmt.Errorf("cannot resolve GitLab ref %q: %w", ref, err)
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("GitLab ref %q has no commits", ref)
	}
	return commits[0].ID, nil
}

// Mirrors returns all configured mirrors of a repository.
func (uc *SyncUseCase) Mirrors(repoPath string) ([]entity.Mirror, error) {
	set, err := uc.store.Load(repoPath)
	if err != nil {
		return nil, err
	}
	return set.Mirrors, nil
}

// ---------------------------------------------------------------- plan

// SyncInput selects a mirror and controls a sync run.
type SyncInput struct {
	RepoPath  string
	Mirror    string // mirror name; may be empty when the branch has a single mirror
	Strategy  string // merge (default), local, remote, abort
	DryRun    bool
	Autostash bool
	Message   string // custom GitLab commit message
	// Replay pulls GitLab commits one by one, each as its own local commit
	// with the original message, author and date.
	Replay bool
	// Format shapes local commit messages ("fix: title TASK-1"). Empty fields
	// fall back to the values remembered in the mirror.
	Format entity.CommitFormat
}

func (uc *SyncUseCase) loadMirror(in SyncInput) (*entity.MirrorSet, *entity.Mirror, error) {
	set, err := uc.store.Load(in.RepoPath)
	if err != nil {
		return nil, nil, err
	}
	branch := ""
	if in.Mirror == "" {
		branch, _ = uc.git.CurrentBranch(in.RepoPath)
	}
	m, err := set.Find(in.Mirror, branch)
	if err != nil {
		return nil, nil, err
	}
	return set, m, nil
}

// Plan computes what a sync would do without changing anything.
func (uc *SyncUseCase) Plan(ctx context.Context, in SyncInput) (*SyncPlan, error) {
	_, m, err := uc.loadMirror(in)
	if err != nil {
		return nil, err
	}
	return uc.plan(ctx, in.RepoPath, m, in.Replay)
}

func (uc *SyncUseCase) plan(ctx context.Context, repoPath string, m *entity.Mirror, replay bool) (*SyncPlan, error) {
	base := m.Current()
	p := &SyncPlan{Mirror: *m, Base: base}

	var err error
	if p.LocalHead, err = uc.git.RevParse(repoPath, m.LocalBranch); err != nil {
		return nil, fmt.Errorf("local branch %q: %w", m.LocalBranch, err)
	}
	if p.RemoteHead, err = uc.gitlab.GetBranchHead(m.ProjectID, m.RemoteBranch); err != nil {
		return nil, fmt.Errorf("gitlab branch %q: %w", m.RemoteBranch, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var local []FileChange
	if p.LocalHead != base.LocalSHA {
		files, err := uc.git.DiffFiles(repoPath, base.LocalSHA, p.LocalHead)
		if err != nil {
			return nil, err
		}
		local = localChangesFromDiff(files)
	}

	// Files left unresolved by the previous sync are pushed in their current state.
	if m.PendingMerge != nil {
		seen := make(map[string]bool, len(local))
		for _, c := range local {
			seen[c.Path] = true
		}
		for _, f := range m.PendingMerge.Files {
			p.PendingFiles = append(p.PendingFiles, f)
			if seen[f] {
				continue
			}
			kind := ChangeModified
			if _, found, _ := uc.git.FileAtRef(repoPath, p.LocalHead, f); !found {
				kind = ChangeDeleted
			}
			local = append(local, FileChange{Path: f, Kind: kind})
		}
	}

	var remote []FileChange
	if p.RemoteHead != base.RemoteSHA {
		diffs, err := uc.gitlab.GetCompareDiff(m.ProjectID, base.RemoteSHA, p.RemoteHead)
		if err != nil {
			return nil, err
		}
		remote = remoteChangesFromDiff(diffs)
	}

	p.LocalChanges, p.RemoteChanges, p.Conflicts = splitChanges(local, remote)

	if replay && p.RemoteHead != base.RemoteSHA {
		if p.RemoteCommits, err = uc.remoteCommits(ctx, m, base.RemoteSHA, p.RemoteHead); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// ---------------------------------------------------------------- sync

// SyncResult reports what a sync did.
type SyncResult struct {
	Plan         *SyncPlan
	DryRun       bool
	Pulled       int
	Pushed       int
	Merged       []string // merged cleanly
	Conflicts    []string // left for manual resolution
	LocalCommit  string
	RemoteCommit string
	RemoteMoved  bool
	Warnings     []string
	Replayed     []entity.CommitPair // GitLab commits replayed as local commits
}

// Summary is a one-line result description.
func (r *SyncResult) Summary() string {
	if r.DryRun {
		return "dry run, nothing changed"
	}
	if r.Plan != nil && r.Plan.Empty() {
		return "already up to date"
	}
	s := fmt.Sprintf("pulled %d, pushed %d, merged %d", r.Pulled, r.Pushed, len(r.Merged))
	if len(r.Replayed) > 0 {
		s = fmt.Sprintf("replayed %d commit(s), ", len(r.Replayed)) + s
	}
	if len(r.Conflicts) > 0 {
		s += fmt.Sprintf(", %d conflict(s) to resolve: %s", len(r.Conflicts), strings.Join(r.Conflicts, ", "))
	}
	return s
}

// pendingWrite is a file written to the working tree after the sync commit,
// for the user to resolve.
type pendingWrite struct {
	path    string
	content []byte
}

// Sync pulls GitLab changes into the local branch and pushes local changes to
// GitLab, merging files changed on both sides according to the strategy.
//
// Order of operations keeps both sides consistent on failure:
// remote changes are written to the working tree, local changes are pushed,
// and only then the local sync commit is created. If the push fails the
// working tree is restored.
func (uc *SyncUseCase) Sync(ctx context.Context, in SyncInput) (res *SyncResult, err error) {
	strategy := in.Strategy
	if strategy == "" {
		strategy = StrategyMerge
	}
	switch strategy {
	case StrategyMerge, StrategyLocal, StrategyRemote, StrategyAbort:
	default:
		return nil, fmt.Errorf("unknown strategy %q (merge, local, remote, abort)", strategy)
	}

	set, m, err := uc.loadMirror(in)
	if err != nil {
		return nil, err
	}

	branch, err := uc.git.CurrentBranch(in.RepoPath)
	if err != nil {
		return nil, err
	}
	if branch != m.LocalBranch {
		return nil, fmt.Errorf("mirror %s syncs branch %q, but %q is checked out", m.Name, m.LocalBranch, branch)
	}

	if err := uc.checkPendingResolved(in.RepoPath, m); err != nil {
		return nil, err
	}

	if err := in.Format.Validate(); err != nil {
		return nil, err
	}
	in.Format = in.Format.Merge(m.CommitFormat)
	m.CommitFormat = in.Format // remembered when the sync is saved

	res = &SyncResult{DryRun: in.DryRun}

	if !in.DryRun {
		wt, err := uc.git.Status(in.RepoPath)
		if err != nil {
			return nil, err
		}
		// Untracked files do not block a sync; only files it would overwrite do.
		if len(wt.Changed) > 0 {
			if !in.Autostash {
				return nil, uncommittedError(wt.Changed, "commit them or use --autostash")
			}
			stashed, err := uc.git.StashPush(in.RepoPath, "reposqueeze sync autostash")
			if err != nil {
				return nil, fmt.Errorf("autostash failed: %w", err)
			}
			if stashed {
				uc.logger.Info("Stashed local changes")
				defer func() {
					if popErr := uc.git.StashPop(in.RepoPath); popErr != nil {
						msg := fmt.Sprintf("could not re-apply stashed changes, they are kept in `git stash list`: %v", popErr)
						if res != nil {
							res.Warnings = append(res.Warnings, msg)
						}
						uc.logger.Warn(msg)
					} else {
						uc.logger.Info("Re-applied stashed changes")
					}
				}()
			}
		}
	}

	plan, err := uc.plan(ctx, in.RepoPath, m, in.Replay)
	if err != nil {
		return nil, err
	}
	res.Plan = plan
	for _, line := range plan.Lines() {
		uc.logger.Info(line)
	}

	if in.DryRun || plan.Empty() {
		return res, nil
	}
	if len(plan.RemoteCommits) > 0 {
		return uc.replay(ctx, in, set, m, plan, strategy, res)
	}
	if strategy == StrategyAbort && len(plan.Conflicts) > 0 {
		return res, fmt.Errorf("%d file(s) changed on both sides; rerun with --strategy merge, local or remote", len(plan.Conflicts))
	}

	base := plan.Base
	pullSet := append([]FileChange(nil), plan.RemoteChanges...)
	pushSet := append([]FileChange(nil), plan.LocalChanges...)
	merged := map[string][]byte{}
	var conflictWrites []pendingWrite

	// Decide what to do with files changed on both sides.
	for _, c := range plan.Conflicts {
		switch strategy {
		case StrategyLocal:
			pushSet = append(pushSet, FileChange{Path: c.Path, Kind: c.Local})
		case StrategyRemote:
			pullSet = append(pullSet, FileChange{Path: c.Path, Kind: c.Remote})
		case StrategyMerge:
			outcome, err := uc.mergeOne(in.RepoPath, m, base, plan, c)
			if err != nil {
				return res, err
			}
			switch {
			case outcome.identical:
				uc.logger.Infof("Same content on both sides: %s", c.Path)
			case outcome.clean:
				merged[c.Path] = outcome.content
				res.Merged = append(res.Merged, c.Path)
				uc.logger.Infof("Merged: %s", c.Path)
			default:
				res.Conflicts = append(res.Conflicts, c.Path)
				if outcome.content != nil {
					conflictWrites = append(conflictWrites, pendingWrite{path: c.Path, content: outcome.content})
				}
				uc.logger.Warnf("Conflict: %s (local: %s, gitlab: %s)", c.Path, c.Local, c.Remote)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}

	writes := append([]string(nil), res.Merged...)
	for _, c := range pullSet {
		writes = append(writes, c.Path)
	}
	for _, w := range conflictWrites {
		writes = append(writes, w.path)
	}
	if err := checkUntracked(uc.git, in.RepoPath, writes); err != nil {
		return res, err
	}

	// 1. Write GitLab changes and merge results into the working tree.
	var touched []string
	restore := func() {
		if len(touched) == 0 {
			return
		}
		if rerr := uc.git.RestorePaths(in.RepoPath, touched); rerr != nil {
			uc.logger.Errorf("failed to restore working tree, check `git status`: %v", rerr)
		}
	}
	for _, c := range pullSet {
		touched = append(touched, c.Path)
		if c.Kind == ChangeDeleted {
			if err := removeLocal(in.RepoPath, c.Path); err != nil {
				restore()
				return res, err
			}
			uc.logger.Infof("Deleted: %s", c.Path)
			continue
		}
		if err := downloadRemoteFile(uc.gitlab, uc.logger, m.ProjectID, c.Path, plan.RemoteHead, in.RepoPath); err != nil {
			restore()
			return res, err
		}
		res.Pulled++
		if err := ctx.Err(); err != nil {
			restore()
			return res, err
		}
	}
	for _, path := range res.Merged {
		touched = append(touched, path)
		if err := writeLocalFile(in.RepoPath, path, merged[path]); err != nil {
			restore()
			return res, err
		}
	}

	// The local sync commit message is checked by the commit-msg hook before
	// anything is pushed, so a rejected message cannot strand a pushed change.
	localMsg := in.Format.Apply(fmt.Sprintf("sync %s with %s/%s\n\n%s..%s from GitLab",
		m.LocalBranch, m.ProjectName, m.RemoteBranch, short(base.RemoteSHA), short(plan.RemoteHead)))
	if len(touched) > 0 {
		if err := checkMessages(uc.git, in.RepoPath, []string{localMsg}); err != nil {
			restore()
			return res, err
		}
	}

	// 2. Push local changes and merge results to GitLab.
	newRemote := plan.RemoteHead
	actions, err := uc.pushActions(in.RepoPath, m, base, plan, pushSet, merged)
	if err != nil {
		restore()
		return res, err
	}
	if len(actions) > 0 {
		if err := ctx.Err(); err != nil {
			restore()
			return res, err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("Sync from %s %s..%s", m.LocalBranch, short(base.LocalSHA), short(plan.LocalHead))
		}
		created, err := uc.gitlab.CommitFilesViaAPI(strconv.Itoa(m.ProjectID), m.RemoteBranch, msg, actions)
		if err != nil {
			restore()
			return res, fmt.Errorf("push to GitLab failed, local working tree restored: %w", err)
		}
		res.Pushed = len(actions)
		res.RemoteCommit = created.ID
		newRemote = created.ID
		if len(created.ParentIDs) > 0 && created.ParentIDs[0] != plan.RemoteHead {
			// Someone pushed to GitLab while we were syncing. Record the head we
			// actually pulled, so their changes (and ours, harmlessly) are pulled next time.
			res.RemoteMoved = true
			newRemote = plan.RemoteHead
			res.Warnings = append(res.Warnings, "GitLab branch moved during sync; run sync again to pull the new commits")
		}
		uc.logger.Infof("Pushed %d change(s) to %s as %s", res.Pushed, m.RemoteBranch, short(created.ID))
	}

	// 3. Commit what came from GitLab. When only pushing, the current local
	// commit already matches GitLab and becomes the sync point.
	localCommit := plan.LocalHead
	if len(touched) > 0 {
		localCommit, err = uc.git.CommitPaths(in.RepoPath, localMsg, touched, gateway.CommitOptions{})
		if err != nil {
			return res, fmt.Errorf("pushed to GitLab (%s) but the local commit failed; commit the working tree manually, then %s: %w", short(newRemote), resetHint, err)
		}
		res.LocalCommit = localCommit
	}

	// 4. Leave unresolved conflicts in the working tree.
	for _, w := range conflictWrites {
		if err := writeLocalFile(in.RepoPath, w.path, w.content); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not write conflict for %s: %v", w.path, err))
		}
	}

	// 5. Journal.
	entry := entity.JournalEntry{
		SyncPoint:   entity.SyncPoint{LocalSHA: localCommit, RemoteSHA: newRemote, At: uc.now()},
		Direction:   direction(res),
		LocalFrom:   base.LocalSHA,
		LocalTo:     plan.LocalHead,
		RemoteFrom:  base.RemoteSHA,
		RemoteTo:    plan.RemoteHead,
		Pulled:      res.Pulled,
		Pushed:      res.Pushed,
		Merged:      res.Merged,
		Conflicts:   res.Conflicts,
		RemoteMoved: res.RemoteMoved,
	}
	m.Journal = append(m.Journal, entry)
	m.PendingMerge = nil
	if len(res.Conflicts) > 0 {
		m.PendingMerge = &entity.PendingMerge{RemoteSHA: plan.RemoteHead, Files: res.Conflicts, At: entry.At}
	}
	if err := uc.store.Save(in.RepoPath, set); err != nil {
		return res, fmt.Errorf("sync done but saving mirror state failed; %s: %w", resetHint, err)
	}
	return res, nil
}

func direction(r *SyncResult) string {
	pulled := r.Pulled > 0 || len(r.Merged) > 0
	switch {
	case pulled && r.Pushed > 0:
		return entity.SyncBoth
	case r.Pushed > 0:
		return entity.SyncPush
	case !pulled && len(r.Conflicts) > 0:
		return entity.SyncConflict
	default:
		return entity.SyncPull
	}
}

// checkPendingResolved refuses to sync while conflict markers from the previous
// sync are still in the working tree or were committed as-is.
func (uc *SyncUseCase) checkPendingResolved(repoPath string, m *entity.Mirror) error {
	if m.PendingMerge == nil {
		return nil
	}
	var unresolved []string
	for _, f := range m.PendingMerge.Files {
		local, err := safeJoin(repoPath, f)
		if err != nil {
			continue
		}
		if data, err := os.ReadFile(local); err == nil && bytes.Contains(data, conflictMarker) {
			unresolved = append(unresolved, f)
			continue
		}
		if data, found, _ := uc.git.FileAtRef(repoPath, "HEAD", f); found && bytes.Contains(data, conflictMarker) {
			unresolved = append(unresolved, f)
		}
	}
	if len(unresolved) > 0 {
		return fmt.Errorf("resolve conflicts from the previous sync and commit them first: %s", strings.Join(unresolved, ", "))
	}
	return nil
}

type mergeOutcome struct {
	identical bool   // both sides already have the same content
	clean     bool   // merged without conflicts; content is the result
	content   []byte // merged result, conflict-marked text, or GitLab version to review
}

// mergeOne resolves a path changed on both sides.
func (uc *SyncUseCase) mergeOne(repoPath string, m *entity.Mirror, base entity.SyncPoint, plan *SyncPlan, c Conflict) (mergeOutcome, error) {
	var theirs []byte
	if c.Remote != ChangeDeleted {
		data, err := uc.gitlab.GetRawFile(m.ProjectID, c.Path, plan.RemoteHead)
		if err != nil {
			return mergeOutcome{}, fmt.Errorf("failed to download %q for merge: %w", c.Path, err)
		}
		theirs = data
	}
	ours, oursFound, err := uc.git.FileAtRef(repoPath, plan.LocalHead, c.Path)
	if err != nil {
		return mergeOutcome{}, err
	}

	switch {
	case c.Local == ChangeDeleted && c.Remote == ChangeDeleted:
		return mergeOutcome{identical: true}, nil
	case c.Local == ChangeDeleted:
		// Deleted here, changed on GitLab: show GitLab's version in the working tree.
		return mergeOutcome{content: theirs}, nil
	case c.Remote == ChangeDeleted:
		// Changed here, deleted on GitLab: keep the local file as is.
		return mergeOutcome{}, nil
	case oursFound && bytes.Equal(ours, theirs):
		return mergeOutcome{identical: true}, nil
	}

	baseContent, _, err := uc.git.FileAtRef(repoPath, base.LocalSHA, c.Path)
	if err != nil {
		return mergeOutcome{}, err
	}
	if isBinary(ours) || isBinary(theirs) || isBinary(baseContent) {
		// Cannot merge binaries: put GitLab's version in the working tree for review.
		return mergeOutcome{content: theirs}, nil
	}

	result, err := uc.git.MergeFile(ours, baseContent, theirs, [3]string{
		"local " + m.LocalBranch, "last sync", "gitlab " + m.RemoteBranch,
	})
	if err != nil {
		return mergeOutcome{}, err
	}
	return mergeOutcome{clean: !result.Conflicts, content: result.Content}, nil
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(b[:n], 0) >= 0
}

// pushActions builds GitLab commit actions for local changes and merge results.
func (uc *SyncUseCase) pushActions(repoPath string, m *entity.Mirror, base entity.SyncPoint, plan *SyncPlan,
	pushSet []FileChange, merged map[string][]byte) ([]gateway.CommitAction, error) {

	remoteKind := make(map[string]string, len(plan.RemoteChanges)+len(plan.Conflicts))
	for _, c := range plan.RemoteChanges {
		remoteKind[c.Path] = c.Kind
	}
	for _, c := range plan.Conflicts {
		remoteKind[c.Path] = c.Remote
	}

	// Files left unresolved last time differ between the two sides of the sync point.
	pending := make(map[string]bool, len(plan.PendingFiles))
	for _, f := range plan.PendingFiles {
		pending[f] = true
	}

	// existsOnRemote infers presence on GitLab from the last sync point
	// (whose content matches the local base commit) plus GitLab's changes,
	// and only asks the API when the inference says "absent".
	existsOnRemote := func(path string) bool {
		if pending[path] {
			exists, _ := uc.gitlab.FileExists(m.ProjectID, path, plan.RemoteHead)
			return exists
		}
		if kind, changed := remoteKind[path]; changed {
			if kind != ChangeDeleted {
				return true
			}
		} else if _, found, _ := uc.git.FileAtRef(repoPath, base.LocalSHA, path); found {
			return true
		}
		exists, _ := uc.gitlab.FileExists(m.ProjectID, path, plan.RemoteHead)
		return exists
	}

	files := make([]gateway.CommitFileInfo, 0, len(pushSet)+len(merged))
	for _, c := range pushSet {
		status := "M"
		if c.Kind == ChangeDeleted {
			status = "D"
		}
		files = append(files, gateway.CommitFileInfo{Status: status, Path: c.Path})
	}
	for path := range merged {
		files = append(files, gateway.CommitFileInfo{Status: "M", Path: path})
	}

	return buildCommitActions(files,
		func(path string) ([]byte, error) {
			if content, ok := merged[path]; ok {
				return content, nil
			}
			content, found, err := uc.git.FileAtRef(repoPath, plan.LocalHead, path)
			if err == nil && !found {
				err = fmt.Errorf("file %q not found at %s", path, short(plan.LocalHead))
			}
			return content, err
		},
		existsOnRemote,
	)
}

// checkMessages runs the commit-msg hook on every distinct message.
func checkMessages(git gateway.SyncGit, repoPath string, msgs []string) error {
	seen := map[string]bool{}
	for _, msg := range msgs {
		title := firstLine(msg)
		if seen[title] {
			continue
		}
		seen[title] = true
		if err := git.CheckCommitMessage(repoPath, msg); err != nil {
			return fmt.Errorf("%w\nSet the commit type and task with --type and --task", err)
		}
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
