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

// PushBranchUseCase pushes only the files changed in a local branch
// (diff from merge-base to branch tip, excluding vendor) as a single
// new commit to an existing GitLab project branch.
type PushBranchUseCase struct {
	gitGateway    gateway.GitGateway
	gitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// PushBranchInput represents the input data for the push-branch use case.
type PushBranchInput struct {
	RepoPath      string
	SourceBranch  string // local branch to take changes from
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
		commitMessage = fmt.Sprintf("Push changes from branch %s", input.SourceBranch)
	}

	// Step 3: Find merge-base with master or main.
	mergeBase, err := uc.gitGateway.GetMergeBase(input.RepoPath, "master", input.SourceBranch)
	if err != nil {
		mergeBase, err = uc.gitGateway.GetMergeBase(input.RepoPath, "main", input.SourceBranch)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to find merge-base for branch %q against master/main: %w", input.SourceBranch, err)
		}
	}

	// Step 4: Get changed files from merge-base to source branch (vendor excluded by git).
	files, err := uc.gitGateway.GetBranchDiffFiles(input.RepoPath, mergeBase, input.SourceBranch)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get diff files: %w", err)
	}

	// Step 5: Build commit actions.
	var actions []gateway.CommitAction
	for _, f := range files {
		gitPath := filepath.ToSlash(f.Path)

		switch {
		case f.Status == "A" || f.Status == "M":
			content, err := uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.SourceBranch, f.Path)
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
			content, err := uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.SourceBranch, f.Path)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to get file content for %q: %w", f.Path, err)
			}

			actions = append(actions, gateway.CommitAction{
				Action:   "delete",
				FilePath: oldPath,
			})

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
		return 0, 0, fmt.Errorf("no file changes found in branch %s (vendor excluded)", input.SourceBranch)
	}

	// Step 6: Commit via GitLab API.
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
