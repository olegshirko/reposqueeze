package tui

import (
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// getGitBranches returns a sorted list of local branch names for the given repo.
func getGitBranches(repoPath string) []string {
	cmd := exec.Command("git", "branch", "--format=%(refname:short)")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	branches := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, b := range branches {
		b = strings.TrimSpace(b)
		if b != "" {
			result = append(result, b)
		}
	}
	sort.Strings(result)
	return result
}

// getGitFiles returns a sorted list of tracked files for the given repo.
func getGitFiles(repoPath string) []string {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f != "" {
			result = append(result, f)
		}
	}
	sort.Strings(result)
	return result
}

// toCommaSeparated joins a slice into a comma-separated string.
func toCommaSeparated(s []string) string {
	return strings.Join(s, ",")
}

// safeAtoi parses an int with a fallback.
func safeAtoi(s string, def int) int {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// getGitLabBranches returns a sorted list of branch names from GitLab.
func getGitLabBranches(gw gateway.GitLabGateway, repoPath string) []string {
	projectName := usecase.ProjectNameFromPath(repoPath)
	project, err := gw.FindProjectByName(projectName)
	if err != nil || project == nil {
		return nil
	}
	branches, err := gw.GetBranches(project.ID)
	if err != nil {
		return nil
	}
	var result []string
	for _, b := range branches {
		result = append(result, b.Name)
	}
	sort.Strings(result)
	return result
}

// getFilesFromGitLabCommits returns a deduplicated sorted list of files that
// were touched in the last N commits of the given GitLab branch.
func getFilesFromGitLabCommits(gw gateway.GitLabGateway, repoPath, branchName string, commits int) ([]string, error) {
	projectName := usecase.ProjectNameFromPath(repoPath)
	project, err := gw.FindProjectByName(projectName)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("project %q not found on GitLab", projectName)
	}
	commitList, err := gw.GetCommits(project.ID, branchName, commits)
	if err != nil {
		return nil, err
	}
	fileSet := make(map[string]struct{})
	// Process from oldest to newest so newer commits overwrite.
	for i := len(commitList) - 1; i >= 0; i-- {
		diffs, err := gw.GetCommitDiff(project.ID, commitList[i].ID)
		if err != nil {
			return nil, err
		}
		for _, d := range diffs {
			if !d.DeletedFile {
				fileSet[d.NewPath] = struct{}{}
			}
		}
	}
	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// getGitCommits returns "sha subject" options for the latest commits of ref.
func getGitCommits(repoPath, ref string, limit int) []huh.Option[string] {
	if ref == "" {
		ref = "HEAD"
	}
	cmd := exec.Command("git", "log", "-n", fmt.Sprint(limit), "--format=%H%x09%h %s", ref)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var opts []huh.Option[string]
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sha, label, ok := strings.Cut(line, "\t")
		if ok {
			opts = append(opts, huh.NewOption(label, sha))
		}
	}
	return opts
}

// getGitLabCommits returns "sha subject" options for the latest commits of a GitLab branch.
func getGitLabCommits(gw gateway.GitLabGateway, repoPath, branch string, limit int) []huh.Option[string] {
	projectName := usecase.ProjectNameFromPath(repoPath)
	project, err := gw.FindProjectByName(projectName)
	if err != nil || project == nil || branch == "" {
		return nil
	}
	commits, err := gw.GetCommits(project.ID, branch, limit)
	if err != nil {
		return nil
	}
	var opts []huh.Option[string]
	for _, c := range commits {
		subject, _, _ := strings.Cut(c.Message, "\n")
		short := c.ID
		if len(short) > 8 {
			short = short[:8]
		}
		opts = append(opts, huh.NewOption(short+" "+subject, c.ID))
	}
	return opts
}

// getMirrorNames lists sync mirrors configured for the repository.
func getMirrorNames(repoPath string) []string {
	if repoPath == "" {
		return nil
	}
	gw := quietGit()
	set, err := state.NewFileStore(gw).Load(repoPath)
	if err != nil {
		return nil
	}
	var names []string
	for _, m := range set.Mirrors {
		names = append(names, m.Name)
	}
	return names
}

// quietGit returns a git gateway that logs nowhere (safe while the TUI owns stdout).
func quietGit() *git.OSExecGitGateway {
	return git.NewOSExecGitGateway(logger.NewLoggerWithWriter(io.Discard))
}

// getRememberedFormat returns the commit type/task stored in the mirror of
// the repository's current branch.
func getRememberedFormat(repoPath string) entity.CommitFormat {
	if repoPath == "" {
		return entity.CommitFormat{}
	}
	gw := quietGit()
	branch, err := gw.CurrentBranch(repoPath)
	if err != nil {
		return entity.CommitFormat{}
	}
	set, err := state.NewFileStore(gw).Load(repoPath)
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

// getPickableCommits lists GitLab commits of branch as options; commits
// already brought into the local branch are marked "[in local]".
func getPickableCommits(gw gateway.GitLabGateway, repoPath, branch string, limit int) []huh.Option[string] {
	if repoPath == "" || branch == "" {
		return nil
	}
	gitGW := quietGit()
	uc := usecase.NewPullCommitUseCase(gitGW, gw, state.NewFileStore(gitGW), logger.NewLoggerWithWriter(io.Discard))
	commits, picked, err := uc.ListCommits(repoPath, branch, limit)
	if err != nil {
		return []huh.Option[string]{huh.NewOption("(cannot list commits: "+err.Error()+")", "")}
	}
	var opts []huh.Option[string]
	for _, c := range commits {
		mark := ""
		if _, ok := picked[c.ID]; ok {
			mark = "  [in local]"
		}
		date := c.AuthoredDate
		if len(date) >= 10 {
			date = date[:10]
		}
		title := c.Title
		if title == "" {
			title, _, _ = strings.Cut(c.Message, "\n")
		}
		label := fmt.Sprintf("%.8s  %s  %-14.14s  %s%s", c.ID, date, c.AuthorName, title, mark)
		opts = append(opts, huh.NewOption(label, c.ID))
	}
	return opts
}
