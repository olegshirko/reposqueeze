package usecase

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// CherryPickCommitUseCase pushes a specific local commit's file changes
// as a new commit to an existing GitLab project branch.
type CherryPickCommitUseCase struct {
	gitGateway    gateway.GitGateway
	gitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// CherryPickCommitInput represents the input data for the cherry-pick-commit use case.
type CherryPickCommitInput struct {
	RepoPath      string
	CommitHash    string
	BranchName    string
	CommitMessage string // optional; if empty, the original commit message is used
}

// NewCherryPickCommitUseCase creates a new instance of CherryPickCommitUseCase.
func NewCherryPickCommitUseCase(gitGateway gateway.GitGateway, gitLabGateway gateway.GitLabGateway, log logger.Logger) *CherryPickCommitUseCase {
	return &CherryPickCommitUseCase{
		gitGateway:    gitGateway,
		gitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *CherryPickCommitUseCase) Execute(ctx context.Context, input CherryPickCommitInput) (time.Duration, int, error) {
	// Step 1: Find the project by name.
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	project, err := uc.gitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find project: %w", err)
	}
	if project == nil {
		return 0, 0, fmt.Errorf("project %q not found on GitLab", projectName)
	}

	// Step 2: Resolve commit message.
	commitMessage := input.CommitMessage
	if commitMessage == "" {
		commitMessage, err = uc.gitGateway.GetCommitMessage(input.RepoPath, input.CommitHash)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to get commit message: %w", err)
		}
		commitMessage = strings.TrimSpace(commitMessage)
	}

	// Step 3: Get changed files from the local commit.
	files, err := uc.gitGateway.GetCommitFiles(input.RepoPath, input.CommitHash)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get commit files: %w", err)
	}

	// Step 4: Build commit actions.
	var actions []gateway.CommitAction
	for _, f := range files {
		gitPath := filepath.ToSlash(f.Path)

		switch {
		case f.Status == "A" || f.Status == "M":
			content, err := uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.CommitHash, f.Path)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to get file content for %q: %w", f.Path, err)
			}

			exists, _ := uc.gitLabGateway.FileExists(project.ID, gitPath, input.BranchName)
			action := "create"
			if exists {
				action = "update"
			}

			actions = append(actions, gateway.CommitAction{
				Action:   action,
				FilePath: gitPath,
				Content:  string(content),
				Encoding: "text",
			})

		case f.Status == "D":
			actions = append(actions, gateway.CommitAction{
				Action:   "delete",
				FilePath: gitPath,
			})

		case strings.HasPrefix(f.Status, "R"):
			oldPath := filepath.ToSlash(f.OldPath)
			content, err := uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.CommitHash, f.Path)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to get file content for %q: %w", f.Path, err)
			}

			// Delete the old path.
			actions = append(actions, gateway.CommitAction{
				Action:   "delete",
				FilePath: oldPath,
			})

			// Create or update the new path.
			exists, _ := uc.gitLabGateway.FileExists(project.ID, gitPath, input.BranchName)
			action := "create"
			if exists {
				action = "update"
			}
			actions = append(actions, gateway.CommitAction{
				Action:   action,
				FilePath: gitPath,
				Content:  string(content),
				Encoding: "text",
			})

		default:
			return 0, 0, fmt.Errorf("unsupported file change status %q for file %q", f.Status, f.Path)
		}
	}

	if len(actions) == 0 {
		return 0, 0, fmt.Errorf("no file changes found in commit %s", input.CommitHash)
	}

	// Step 5: Commit via GitLab API.
	startTime := time.Now()
	err = uc.gitLabGateway.CommitFilesViaAPI(
		fmt.Sprintf("%d", project.ID),
		input.BranchName,
		commitMessage,
		actions,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to commit files via API: %w", err)
	}
	duration := time.Since(startTime)

	return duration, len(actions), nil
}
