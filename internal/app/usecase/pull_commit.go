package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// PullCommitUseCase brings selected GitLab commits into the current local
// branch, one local commit each, like `git cherry-pick` across the API.
type PullCommitUseCase struct {
	git    gateway.SyncGit
	gitlab gateway.GitLabGateway
	store  gateway.MirrorStore // optional: supplies the remembered commit format
	logger logger.Logger
}

// NewPullCommitUseCase creates a new PullCommitUseCase.
func NewPullCommitUseCase(git gateway.SyncGit, gitlab gateway.GitLabGateway, store gateway.MirrorStore, log logger.Logger) *PullCommitUseCase {
	return &PullCommitUseCase{git: git, gitlab: gitlab, store: store, logger: log}
}

// PullCommitInput selects the GitLab commits to bring in.
type PullCommitInput struct {
	RepoPath string
	Commits  []string // GitLab SHAs (full or abbreviated); applied oldest first
	Strategy string   // merge (default), local, remote, abort
	Format   entity.CommitFormat
}

// PullCommitResult reports what was brought in.
type PullCommitResult struct {
	Picked    []entity.CommitPair
	Skipped   []entity.CommitPair // already present locally
	Conflicts []string            // files with conflicts in StoppedAt
	StoppedAt *gateway.CommitInfo // commit left in the working tree for manual resolution
	Remaining []string            // selected commits not applied because of the stop
	// CommitCommand completes StoppedAt after the conflicts are resolved.
	CommitCommand string
}

// Summary is a one-line result description.
func (r *PullCommitResult) Summary() string {
	s := fmt.Sprintf("picked %d commit(s)", len(r.Picked))
	if len(r.Skipped) > 0 {
		s += fmt.Sprintf(", skipped %d already present", len(r.Skipped))
	}
	if r.StoppedAt != nil {
		s += fmt.Sprintf(", stopped at %s with conflicts in %s", short(r.StoppedAt.ID), strings.Join(r.Conflicts, ", "))
	}
	return s
}

// ListCommits returns recent GitLab commits of a branch, newest first,
// marking the ones already brought into the local branch.
func (uc *PullCommitUseCase) ListCommits(repoPath, branch string, limit int) ([]gateway.CommitInfo, map[string]string, error) {
	project, err := resolveProject(uc.gitlab, repoPath)
	if err != nil {
		return nil, nil, err
	}
	commits, err := uc.gitlab.GetCommits(project.ID, branch, limit)
	if err != nil {
		return nil, nil, err
	}
	picked, err := alreadyPicked(uc.git, repoPath, "HEAD")
	if err != nil {
		return nil, nil, err
	}
	return commits, picked, nil
}

// Execute applies the selected commits.
func (uc *PullCommitUseCase) Execute(ctx context.Context, in PullCommitInput) (*PullCommitResult, error) {
	strategy := in.Strategy
	if strategy == "" {
		strategy = StrategyMerge
	}
	switch strategy {
	case StrategyMerge, StrategyLocal, StrategyRemote, StrategyAbort:
	default:
		return nil, fmt.Errorf("unknown strategy %q (merge, local, remote, abort)", strategy)
	}
	if len(in.Commits) == 0 {
		return nil, fmt.Errorf("no commits selected")
	}
	if err := in.Format.Validate(); err != nil {
		return nil, err
	}

	project, err := resolveProject(uc.gitlab, in.RepoPath)
	if err != nil {
		return nil, err
	}
	branch, err := uc.git.CurrentBranch(in.RepoPath)
	if err != nil {
		return nil, err
	}
	in.Format = in.Format.Merge(uc.rememberedFormat(in.RepoPath, branch))

	clean, err := uc.git.IsClean(in.RepoPath)
	if err != nil {
		return nil, err
	}
	if !clean {
		return nil, fmt.Errorf("working tree has uncommitted changes; commit or stash them first")
	}
	head, err := uc.git.RevParse(in.RepoPath, "HEAD")
	if err != nil {
		return nil, err
	}

	// Resolve the selection, oldest first, with each commit's own diff.
	commits, err := uc.resolve(ctx, project.ID, in.Commits)
	if err != nil {
		return nil, err
	}

	res := &PullCommitResult{}
	picked, err := alreadyPicked(uc.git, in.RepoPath, "HEAD")
	if err != nil {
		return nil, err
	}
	var todo []RemoteCommit
	for _, c := range commits {
		if local, ok := picked[c.ID]; ok {
			res.Skipped = append(res.Skipped, entity.CommitPair{RemoteSHA: c.ID, LocalSHA: local, Title: commitTitle(c.CommitInfo)})
			uc.logger.Infof("Already present: %s %s (local %s)", short(c.ID), commitTitle(c.CommitInfo), short(local))
			continue
		}
		todo = append(todo, c)
	}

	messages := make([]string, len(todo))
	for i, c := range todo {
		messages[i] = in.Format.Apply(replayMessage(c.CommitInfo, project.Name))
	}
	if err := checkMessages(uc.git, in.RepoPath, messages); err != nil {
		return nil, err
	}

	picker := newCherryPicker(uc.git, uc.gitlab, in.RepoPath, project.ID, head, strategy,
		[3]string{"local " + branch, "gitlab before", "gitlab commit"})

	for i, c := range todo {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		parent := ""
		if len(c.ParentIDs) > 0 {
			parent = c.ParentIDs[0]
		}
		step, conflicts, _, err := picker.apply(c, parent, nil)
		if err != nil {
			return res, err
		}
		if err := writeStep(in.RepoPath, step); err != nil {
			return res, err
		}

		if len(conflicts) > 0 {
			if strategy == StrategyAbort {
				if rerr := uc.git.RestorePaths(in.RepoPath, step.paths); rerr != nil {
					uc.logger.Errorf("failed to restore working tree: %v", rerr)
				}
				return res, fmt.Errorf("commit %s %s conflicts with local changes in %s; %d commit(s) picked before it",
					short(c.ID), commitTitle(c.CommitInfo), conflictPaths(conflicts), len(res.Picked))
			}
			return uc.stop(in.RepoPath, res, c, conflicts, messages[i], todo[i+1:])
		}

		sha, err := uc.git.CommitPaths(in.RepoPath, messages[i], step.paths, authorOptions(c.CommitInfo))
		if err != nil {
			return res, err
		}
		res.Picked = append(res.Picked, entity.CommitPair{RemoteSHA: c.ID, LocalSHA: sha, Title: commitTitle(c.CommitInfo)})
		uc.logger.Infof("Picked %s -> %s %s", short(c.ID), short(sha), firstLine(messages[i]))
	}
	return res, nil
}

// resolve turns user-supplied SHAs into commits with diffs, sorted oldest first.
func (uc *PullCommitUseCase) resolve(ctx context.Context, projectID int, shas []string) ([]RemoteCommit, error) {
	var out []RemoteCommit
	seen := map[string]bool{}
	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		list, err := uc.gitlab.GetCommits(projectID, sha, 1)
		if err != nil || len(list) == 0 || !strings.HasPrefix(list[0].ID, sha) {
			return nil, fmt.Errorf("GitLab commit %q not found", sha)
		}
		c := list[0]
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true

		var diffs []gateway.DiffEntry
		if len(c.ParentIDs) > 0 {
			diffs, err = uc.gitlab.GetCompareDiff(projectID, c.ParentIDs[0], c.ID)
		} else {
			diffs, err = uc.gitlab.GetCommitDiff(projectID, c.ID)
		}
		if err != nil {
			return nil, fmt.Errorf("diff of GitLab commit %s: %w", short(c.ID), err)
		}
		out = append(out, RemoteCommit{CommitInfo: c, Changes: remoteChangesFromDiff(diffs)})
	}
	sortOldestFirst(out)
	return out, nil
}

// sortOldestFirst orders commits by authored date, keeping the given order
// for ties and unparsable dates.
func sortOldestFirst(c []RemoteCommit) {
	at := func(rc RemoteCommit) time.Time {
		t, _ := time.Parse(time.RFC3339, rc.AuthoredDate)
		return t
	}
	sort.SliceStable(c, func(i, j int) bool {
		ti, tj := at(c[i]), at(c[j])
		return !ti.IsZero() && !tj.IsZero() && ti.Before(tj)
	})
}

// stop leaves a conflicting commit in the working tree, like `git cherry-pick`.
func (uc *PullCommitUseCase) stop(repoPath string, res *PullCommitResult, c RemoteCommit, conflicts []pickConflict,
	msg string, rest []RemoteCommit) (*PullCommitResult, error) {

	for _, cf := range conflicts {
		if cf.content == nil {
			continue // keep the local file
		}
		if err := writeLocalFile(repoPath, cf.path, cf.content); err != nil {
			return res, err
		}
	}

	msgFile, err := uc.saveMessage(repoPath, msg)
	if err != nil {
		return res, err
	}
	info := c.CommitInfo
	res.StoppedAt = &info
	res.Conflicts = strings.Split(conflictPaths(conflicts), ", ")
	for _, r := range rest {
		res.Remaining = append(res.Remaining, r.ID)
	}
	res.CommitCommand = fmt.Sprintf("git add -A && git commit -F %s --author %q --date %q",
		shellQuote(msgFile), fmt.Sprintf("%s <%s>", info.AuthorName, info.AuthorEmail), info.AuthoredDate)
	uc.logger.Warnf("Conflicts in %s while picking %s %s", conflictPaths(conflicts), short(info.ID), commitTitle(info))
	return res, nil
}

func (uc *PullCommitUseCase) saveMessage(repoPath, msg string) (string, error) {
	gitDir, err := uc.git.GitDir(repoPath)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(gitDir, "reposqueeze")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "PICK_MSG")
	return path, os.WriteFile(path, []byte(msg+"\n"), 0o644)
}

// rememberedFormat returns the commit format stored in the branch's mirror, if any.
func (uc *PullCommitUseCase) rememberedFormat(repoPath, branch string) entity.CommitFormat {
	if uc.store == nil {
		return entity.CommitFormat{}
	}
	set, err := uc.store.Load(repoPath)
	if err != nil {
		return entity.CommitFormat{}
	}
	for _, m := range set.Mirrors {
		if m.LocalBranch == branch && !m.CommitFormat.IsZero() {
			return m.CommitFormat
		}
	}
	return entity.CommitFormat{}
}

func conflictPaths(c []pickConflict) string {
	paths := make([]string, len(c))
	for i, cf := range c {
		paths[i] = cf.path
	}
	return strings.Join(paths, ", ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
