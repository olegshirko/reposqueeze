package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
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

// getFolderFiles walks the given folder and returns relative file paths,
// skipping .git, vendor, and node_modules.
func getFolderFiles(folderPath string) []string {
	var result []string
	_ = filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := filepath.Base(path)
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(folderPath, path)
		if err != nil {
			return nil
		}
		result = append(result, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(result)
	return result
}

// toCommaSeparated joins a slice into a comma-separated string.
func toCommaSeparated(s []string) string {
	return strings.Join(s, ",")
}

// fromCommaSeparated splits a comma-separated string.
func fromCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	var res []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

// firstOr returns the first element of a slice or a default.
func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// emptyIfNone returns a default string if the slice is empty.
func emptyIfNone(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return toCommaSeparated(s)
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
	projectName := filepath.Base(strings.TrimSuffix(repoPath, ".git"))
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

// getGitLabDefaultBranch returns the default branch name from GitLab or "master".
func getGitLabDefaultBranch(gw gateway.GitLabGateway, repoPath string) string {
	projectName := filepath.Base(strings.TrimSuffix(repoPath, ".git"))
	project, err := gw.FindProjectByName(projectName)
	if err != nil || project == nil {
		return "master"
	}
	branches, err := gw.GetBranches(project.ID)
	if err != nil {
		return "master"
	}
	for _, b := range branches {
		if b.Default {
			return b.Name
		}
	}
	if len(branches) > 0 {
		return branches[0].Name
	}
	return "master"
}

// getFilesFromGitLabCommits returns a deduplicated sorted list of files that
// were touched in the last N commits of the given GitLab branch.
func getFilesFromGitLabCommits(gw gateway.GitLabGateway, repoPath, branchName string, commits int) ([]string, error) {
	projectName := filepath.Base(strings.TrimSuffix(repoPath, ".git"))
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
