package usecase

import (
	"fmt"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// listPaths renders up to max paths, e.g. "a.go, b.go (+3 more)".
func listPaths(paths []string, max int) string {
	if len(paths) <= max {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(paths[:max], ", "), len(paths)-max)
}

// uncommittedError names the tracked files that block an operation.
func uncommittedError(changed []string, hint string) error {
	return fmt.Errorf("uncommitted changes in %s; %s (see `git status`)", listPaths(changed, 10), hint)
}

// checkUntracked refuses to overwrite or delete untracked local files at the
// given paths. Untracked files elsewhere (IDE settings, build output) are fine.
func checkUntracked(git gateway.SyncGit, repoPath string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	wt, err := git.Status(repoPath)
	if err != nil {
		return err
	}
	untracked := make(map[string]bool, len(wt.Untracked))
	for _, p := range wt.Untracked {
		untracked[p] = true
	}
	var clash []string
	seen := map[string]bool{}
	for _, p := range paths {
		if untracked[p] && !seen[p] {
			seen[p] = true
			clash = append(clash, p)
		}
	}
	if len(clash) > 0 {
		return fmt.Errorf("untracked local files would be overwritten: %s; move, delete or commit them first", listPaths(clash, 10))
	}
	return nil
}
