package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// OSExecGitGateway is an implementation of the GitGateway that uses os/exec.
type OSExecGitGateway struct {
	logger logger.Logger
}

// NewOSExecGitGateway creates a new instance of OSExecGitGateway.
func NewOSExecGitGateway(log logger.Logger) *OSExecGitGateway {
	return &OSExecGitGateway{logger: log}
}

// CreateOrphanBranch creates a new orphan branch in the given repository.
// It only switches to the orphan branch; staging and committing is left to the caller.
func (g *OSExecGitGateway) CreateOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) error {
	// Command 1: Create the orphan branch
	args := []string{"checkout", "--orphan", branch.Name}
	if sourceBranch != "" {
		args = append(args, sourceBranch)
	}
	cmdCheckout := exec.Command("git", args...)
	cmdCheckout.Dir = repository.Path
	if output, err := cmdCheckout.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to create orphan branch: %w, output: %s", err, string(output))
		return err
	}

	return nil
}

// CreateEmptyOrphanBranch creates a new orphan branch in the given repository.
func (g *OSExecGitGateway) CreateEmptyOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) error {
	// Command 1: Create the orphan branch
	args := []string{"checkout", "--orphan", branch.Name}
	if sourceBranch != "" {
		args = append(args, sourceBranch)
	}
	cmdCheckout := exec.Command("git", args...)
	cmdCheckout.Dir = repository.Path
	if output, err := cmdCheckout.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to create orphan branch: %w, output: %s", err, string(output))
		return err
	}

	// Command 2: Remove all files from the index to make the branch truly empty
	cmdRm := exec.Command("git", "rm", "-rf", "--cached", ".")
	cmdRm.Dir = repository.Path
	if output, err := cmdRm.CombinedOutput(); err != nil {
		// This command can fail if there are no files, which is fine for an orphan branch
		g.logger.Infof("cleaning index on orphan branch (non-fatal error is ok): %s", string(output))
	}

	return nil
}

// ListFiles lists all tracked files in the repository.
func (g *OSExecGitGateway) ListFiles(repoPath string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to list files: %w, output: %s", err, string(output))
		return nil, err
	}
	// The output is a newline-separated list of files.
	// We need to split it into a slice of strings.
	// Note: This will produce an empty string at the end if the output ends with a newline.
	files := strings.Split(string(output), "\n")
	// Filter out empty strings from the result.
	var result []string
	for _, file := range files {
		if file != "" {
			result = append(result, file)
		}
	}
	return result, nil
}

// DeleteLocalBranch deletes a local branch.
func (g *OSExecGitGateway) DeleteLocalBranch(repoPath, branchName string) error {
	cmd := exec.Command("git", "-C", repoPath, "branch", "-D", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to delete local branch '%s': %w, output: %s", branchName, err, string(output))
		return err
	}
	return nil
}

func (g *OSExecGitGateway) CleanWorkdir(repoPath string) error {
	cmd := exec.Command("git", "clean", "-fdx")
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to clean workdir: %w, output: %s", err, string(output))
		return err
	}
	return nil
}

// RemoveDirectory removes a directory from the repository.
func (g *OSExecGitGateway) RemoveDirectory(repoPath, dirName string) error {
	dirPath := filepath.Join(repoPath, dirName)
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		return os.RemoveAll(dirPath)
	}
	return nil
}

// CheckoutBranch switches to a different local branch.
func (g *OSExecGitGateway) CheckoutBranch(repoPath, branchName string) error {
	cmd := exec.Command("git", "-C", repoPath, "checkout", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to checkout branch '%s': %w, output: %s", branchName, err, string(output))
		return err
	}
	return nil
}
func (g *OSExecGitGateway) AddAll(repoPath string) error {
	cmdAdd := exec.Command("git", "add", ".")
	cmdAdd.Dir = repoPath
	if output, err := cmdAdd.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to stage files: %w, output: %s", err, string(output))
		return err
	}
	return nil
}

func (g *OSExecGitGateway) Commit(repoPath, message string) error {
	if err := g.AddAll(repoPath); err != nil {
		return err
	}

	cmdCommit := exec.Command("git", "commit", "-m", message)
	cmdCommit.Dir = repoPath
	if output, err := cmdCommit.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to make commit: %w, output: %s", err, string(output))
		return err
	}

	return nil
}

// BranchExists checks whether a local branch with the given name exists.
func (g *OSExecGitGateway) BranchExists(repoPath, branchName string) (bool, error) {
	cmd := exec.Command("git", "branch", "--list", branchName)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to list branch '%s': %w, output: %s", branchName, err, string(output))
		return false, err
	}
	return strings.TrimSpace(string(output)) != "", nil
}

// GetCommitMessage returns the full message of a commit.
func (g *OSExecGitGateway) GetCommitMessage(repoPath, commitHash string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "show", "-s", "--format=%B", commitHash)
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to get commit message for %s: %w, output: %s", commitHash, err, string(output))
		return "", err
	}
	return string(output), nil
}

// GetCommitFiles returns the list of changed files in a commit with their status.
func (g *OSExecGitGateway) GetCommitFiles(repoPath, commitHash string) ([]gateway.CommitFileInfo, error) {
	cmd := exec.Command("git", "-C", repoPath, "diff-tree", "--no-commit-id", "--name-status", "-r", commitHash)
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to get commit files for %s: %w, output: %s", commitHash, err, string(output))
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	var result []gateway.CommitFileInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		oldPath := ""
		// Handle renames: "R100\told\tnew"
		if strings.HasPrefix(status, "R") && len(parts) >= 3 {
			oldPath = parts[1]
			path = parts[2]
		}
		result = append(result, gateway.CommitFileInfo{
			Status:  status,
			Path:    path,
			OldPath: oldPath,
		})
	}
	return result, nil
}

// GetFileContentFromCommit returns the content of a file at a specific commit.
func (g *OSExecGitGateway) GetFileContentFromCommit(repoPath, commitHash, filePath string) ([]byte, error) {
	cmd := exec.Command("git", "-C", repoPath, "show", commitHash+":"+filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to get file content %s from commit %s: %w, output: %s", filePath, commitHash, err, string(output))
		return nil, err
	}
	return output, nil
}

// GetBranchDiffFiles returns the list of changed files between two branches.
func (g *OSExecGitGateway) GetBranchDiffFiles(repoPath, baseBranch, sourceBranch string) ([]gateway.CommitFileInfo, error) {
	cmd := exec.Command("git", "-C", repoPath, "diff", "--name-status", baseBranch+".."+sourceBranch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to get diff between %s and %s: %w, output: %s", baseBranch, sourceBranch, err, string(output))
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	var result []gateway.CommitFileInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		oldPath := ""
		if strings.HasPrefix(status, "R") && len(parts) >= 3 {
			oldPath = parts[1]
			path = parts[2]
		}
		result = append(result, gateway.CommitFileInfo{
			Status:  status,
			Path:    path,
			OldPath: oldPath,
		})
	}
	return result, nil
}

// ListFilesInBranch returns all tracked file paths in the given branch.
func (g *OSExecGitGateway) ListFilesInBranch(repoPath, branchName string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "ls-tree", "-r", "--name-only", branchName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to list files in branch %s: %w, output: %s", branchName, err, string(output))
		return nil, err
	}
	files := strings.Split(string(output), "\n")
	var result []string
	for _, file := range files {
		if file != "" {
			result = append(result, file)
		}
	}
	return result, nil
}
