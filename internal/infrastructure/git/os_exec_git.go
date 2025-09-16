package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
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
func (g *OSExecGitGateway) CreateOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) (string, error) {
	// Command 1: Create the orphan branch
	args := []string{"checkout", "--orphan", branch.Name}
	if sourceBranch != "" {
		args = append(args, sourceBranch)
	}
	cmdCheckout := exec.Command("git", args...)
	cmdCheckout.Dir = repository.Path
	if output, err := cmdCheckout.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to create orphan branch: %w, output: %s", err, string(output))
		return "", err
	}

	// Command 2: Stage all current files for the initial commit
	cmdAdd := exec.Command("git", "add", ".")
	cmdAdd.Dir = repository.Path
	if output, err := cmdAdd.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to stage files for commit: %w, output: %s", err, string(output))
		return "", err
	}

	// Command 3: Make an initial commit with the current files
	cmdCommit := exec.Command("git", "commit", "-m", "Initial commit on orphan branch")
	cmdCommit.Dir = repository.Path
	if output, err := cmdCommit.CombinedOutput(); err != nil {
		g.logger.Errorf("failed to make initial commit: %w, output: %s", err, string(output))
		return "", err
	}

	// Command 4: Get the SHA of the new commit
	cmdRevParse := exec.Command("git", "rev-parse", "HEAD")
	cmdRevParse.Dir = repository.Path
	output, err := cmdRevParse.CombinedOutput()
	if err != nil {
		g.logger.Errorf("failed to get new commit SHA: %w, output: %s", err, string(output))
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
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
