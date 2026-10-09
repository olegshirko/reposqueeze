package usecase

import (
	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// pickedCommits returns GitLab commits already brought into the local branch
// (GitLab SHA -> local SHA). Only records whose local commit is reachable
// from head count, so commits dropped by a reset or rebase can be picked again.
//
// A pick that stopped on conflicts is completed here: the first commit made
// after it stopped is taken as the resolved pick. The set is updated in
// memory; callers save it with their own changes.
func pickedCommits(git gateway.SyncGit, set *entity.MirrorSet, repoPath, head string) (map[string]string, error) {
	if pp := set.PendingPick; pp != nil {
		after, err := git.LogCommits(repoPath, pp.Base+".."+head, 1000)
		if err == nil && len(after) > 0 {
			pair := pp.CommitPair
			pair.LocalSHA = after[len(after)-1].ID // oldest commit after the stop
			set.AddPicked(pair)
			set.PendingPick = nil
		}
	}

	picked := make(map[string]string, len(set.Picked))
	for _, p := range set.Picked {
		if p.LocalSHA == "" {
			continue
		}
		if ok, err := git.IsAncestor(repoPath, p.LocalSHA, head); err == nil && ok {
			picked[p.RemoteSHA] = p.LocalSHA
		}
	}
	return picked, nil
}
