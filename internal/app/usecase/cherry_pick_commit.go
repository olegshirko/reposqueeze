package usecase

import (
	"context"
	"fmt"
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
	project, err := resolveProject(uc.gitLabGateway, input.RepoPath)
	if err != nil {
		return 0, 0, err
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
	actions, err := buildCommitActions(files,
		func(path string) ([]byte, error) {
			return uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.CommitHash, path)
		},
		func(path string) bool {
			exists, _ := uc.gitLabGateway.FileExists(project.ID, path, input.BranchName)
			return exists
		},
	)
	if err != nil {
		return 0, 0, err
	}

	if len(actions) == 0 {
		return 0, 0, fmt.Errorf("no file changes found in commit %s", input.CommitHash)
	}

	// Step 5: Commit via GitLab API.
	startTime := time.Now()
	_, err = uc.gitLabGateway.CommitFilesViaAPI(
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
