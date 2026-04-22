package usecase

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// PushFilesUseCase commits specific local files to an existing GitLab project branch.
type PushFilesUseCase struct {
	GitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// PushFilesInput represents the input data for the push-files use case.
type PushFilesInput struct {
	RepoPath   string
	BranchName string
	Files      string // comma-separated list of relative file paths
}

// NewPushFilesUseCase creates a new instance of PushFilesUseCase.
func NewPushFilesUseCase(gitLabGateway gateway.GitLabGateway, log logger.Logger) *PushFilesUseCase {
	return &PushFilesUseCase{
		GitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *PushFilesUseCase) Execute(ctx context.Context, input PushFilesInput) (time.Duration, int, error) {
	// Step 1: Find the project by name.
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find project: %w", err)
	}
	if project == nil {
		return 0, 0, fmt.Errorf("project %q not found on GitLab", projectName)
	}

	// Step 2: Parse the comma-separated file list.
	filePaths := strings.Split(input.Files, ",")
	if len(filePaths) == 0 {
		return 0, 0, fmt.Errorf("no files specified")
	}

	// Step 3: Build commit actions for each file.
	var actions []gateway.CommitAction
	for _, fp := range filePaths {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			continue
		}

		fullPath := filepath.Join(input.RepoPath, fp)
		info, err := os.Stat(fullPath)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to stat file %q: %w", fp, err)
		}
		if info.IsDir() {
			return 0, 0, fmt.Errorf("path %q is a directory, only files are supported", fp)
		}

		f, err := os.Open(fullPath)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to open file %q: %w", fp, err)
		}

		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to read file %q: %w", fp, err)
		}

		// Normalize path separators to forward slashes for GitLab.
		gitPath := filepath.ToSlash(fp)

		actions = append(actions, gateway.CommitAction{
			Action:   "create",
			FilePath: gitPath,
			Content:  string(content),
			Encoding: "text",
		})
	}

	if len(actions) == 0 {
		return 0, 0, fmt.Errorf("no valid files to commit")
	}

	// Step 4: Commit via GitLab API.
	commitMessage := fmt.Sprintf("Add %d file(s) via reposqueeze", len(actions))
	startTime := time.Now()
	err = uc.GitLabGateway.CommitFilesViaAPI(
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
