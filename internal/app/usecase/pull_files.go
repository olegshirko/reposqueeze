package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// PullFilesUseCase downloads files from an existing GitLab project branch.
type PullFilesUseCase struct {
	GitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// PullFilesInput represents the input data for the pull-files use case.
type PullFilesInput struct {
	RepoPath   string
	BranchName string
	Files      string // comma-separated list; if empty, pulls files from the latest commit diff
}

// NewPullFilesUseCase creates a new instance of PullFilesUseCase.
func NewPullFilesUseCase(gitLabGateway gateway.GitLabGateway, log logger.Logger) *PullFilesUseCase {
	return &PullFilesUseCase{
		GitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *PullFilesUseCase) Execute(ctx context.Context, input PullFilesInput) (time.Duration, int, error) {
	// Step 1: Find the project by name.
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find project: %w", err)
	}
	if project == nil {
		return 0, 0, fmt.Errorf("project %q not found on GitLab", projectName)
	}

	var filePaths []string

	if input.Files != "" {
		// Explicit file list provided.
		parts := strings.Split(input.Files, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				filePaths = append(filePaths, p)
			}
		}
	} else {
		// Resolve files from the latest commit diff.
		sha, err := uc.GitLabGateway.GetLastCommitSHA(project.ID, input.BranchName)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to get last commit: %w", err)
		}
		uc.logger.Infof("Latest commit on %s: %s", input.BranchName, sha)

		diffFiles, err := uc.GitLabGateway.GetCommitDiff(project.ID, sha)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to get commit diff: %w", err)
		}
		filePaths = diffFiles
	}

	if len(filePaths) == 0 {
		return 0, 0, fmt.Errorf("no files to pull")
	}

	// Ensure target directory exists.
	if err := os.MkdirAll(input.RepoPath, 0o755); err != nil {
		return 0, 0, fmt.Errorf("failed to create repo path: %w", err)
	}

	// Step 2: Download each file.
	startTime := time.Now()
	downloaded := 0
	for _, fp := range filePaths {
		content, err := uc.GitLabGateway.GetRawFile(project.ID, fp, input.BranchName)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to download file %q: %w", fp, err)
		}

		localPath := filepath.Join(input.RepoPath, filepath.FromSlash(fp))
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return 0, 0, fmt.Errorf("failed to create directory for %q: %w", fp, err)
		}

		if err := os.WriteFile(localPath, content, 0o644); err != nil {
			return 0, 0, fmt.Errorf("failed to write file %q: %w", fp, err)
		}

		downloaded++
		uc.logger.Infof("Pulled: %s", fp)
	}
	duration := time.Since(startTime)

	return duration, downloaded, nil
}
