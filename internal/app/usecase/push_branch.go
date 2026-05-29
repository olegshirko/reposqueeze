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

// PushBranchUseCase pushes all tracked files from a local branch
// as a single new commit to an existing GitLab project branch.
type PushBranchUseCase struct {
	gitGateway    gateway.GitGateway
	gitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// PushBranchInput represents the input data for the push-branch use case.
type PushBranchInput struct {
	RepoPath      string
	SourceBranch  string // local branch to take files from
	BranchName    string // target branch on GitLab
	CommitMessage string // optional
}

// NewPushBranchUseCase creates a new instance of PushBranchUseCase.
func NewPushBranchUseCase(gitGateway gateway.GitGateway, gitLabGateway gateway.GitLabGateway, log logger.Logger) *PushBranchUseCase {
	return &PushBranchUseCase{
		gitGateway:    gitGateway,
		gitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *PushBranchUseCase) Execute(ctx context.Context, input PushBranchInput) (time.Duration, int, error) {
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
		commitMessage = fmt.Sprintf("Push files from branch %s", input.SourceBranch)
	}

	// Step 3: Get all tracked files from the local source branch.
	files, err := uc.gitGateway.ListFilesInBranch(input.RepoPath, input.SourceBranch)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list files in branch: %w", err)
	}

	// Step 4: Build commit actions.
	var actions []gateway.CommitAction
	for _, fp := range files {
		content, err := uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.SourceBranch, fp)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to get file content for %q: %w", fp, err)
		}

		gitPath := filepath.ToSlash(fp)
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
	}

	if len(actions) == 0 {
		return 0, 0, fmt.Errorf("no files found in branch %s", input.SourceBranch)
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
