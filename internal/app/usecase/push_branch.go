package usecase

import (
	"context"
	"fmt"
	"strconv"
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
	// CreateFrom, when set, creates BranchName on GitLab from this GitLab
	// branch first; BranchName must not exist yet.
	CreateFrom string
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
	project, err := resolveProject(uc.gitLabGateway, input.RepoPath)
	if err != nil {
		return 0, 0, err
	}
	if input.CreateFrom != "" {
		if err := uc.checkNewBranch(project.ID, input.BranchName, input.CreateFrom); err != nil {
			return 0, 0, err
		}
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

	// Step 5: Build commit actions. A new branch starts as a copy of
	// CreateFrom, so that is where files are looked up.
	existsRef := input.BranchName
	if input.CreateFrom != "" {
		existsRef = input.CreateFrom
	}
	actions, err := buildCommitActions(files,
		func(path string) ([]byte, error) {
			return uc.gitGateway.GetFileContentFromCommit(input.RepoPath, input.SourceBranch, path)
		},
		func(path string) bool {
			exists, _ := uc.gitLabGateway.FileExists(project.ID, path, existsRef)
			return exists
		},
	)
	if err != nil {
		return 0, 0, err
	}

	if len(actions) == 0 {
		return 0, 0, fmt.Errorf("no file changes found in branch %s (vendor excluded)", input.SourceBranch)
	}

	// The new branch is created only now, when there is something to put in it.
	if input.CreateFrom != "" {
		if err := uc.gitLabGateway.CreateRemoteBranch(ctx, strconv.Itoa(project.ID), input.BranchName, input.CreateFrom); err != nil {
			return 0, 0, fmt.Errorf("failed to create branch %q from %q on GitLab: %w", input.BranchName, input.CreateFrom, err)
		}
		uc.logger.Infof("Created GitLab branch %s from %s", input.BranchName, input.CreateFrom)
	}

	// Step 6: Commit via GitLab API.
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

// checkNewBranch makes sure the branch to create does not exist yet and that
// the branch it starts from does.
func (uc *PushBranchUseCase) checkNewBranch(projectID int, name, from string) error {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, " \t") {
		return fmt.Errorf("invalid new branch name %q", name)
	}
	branches, err := uc.gitLabGateway.GetBranches(projectID)
	if err != nil {
		return fmt.Errorf("failed to list GitLab branches: %w", err)
	}
	fromFound := false
	for _, b := range branches {
		if b.Name == name {
			return fmt.Errorf("branch %q already exists on GitLab; push to it without --create-from", name)
		}
		if b.Name == from {
			fromFound = true
		}
	}
	if !fromFound {
		return fmt.Errorf("branch %q to create %q from does not exist on GitLab", from, name)
	}
	return nil
}
