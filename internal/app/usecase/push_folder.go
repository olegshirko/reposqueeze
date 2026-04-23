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

// PushFolderUseCase uploads an arbitrary local folder to GitLab.
type PushFolderUseCase struct {
	GitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// PushFolderInput represents the input data for the push-folder use case.
type PushFolderInput struct {
	FolderPath  string
	ProjectName string
	BranchName  string
}

// NewPushFolderUseCase creates a new instance of PushFolderUseCase.
func NewPushFolderUseCase(gitLabGateway gateway.GitLabGateway, log logger.Logger) *PushFolderUseCase {
	return &PushFolderUseCase{
		GitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *PushFolderUseCase) Execute(ctx context.Context, input PushFolderInput) (time.Duration, int, error) {
	projectName := input.ProjectName
	if projectName == "" {
		projectName = filepath.Base(strings.TrimSuffix(input.FolderPath, string(os.PathSeparator)))
	}

	// Step 1: Find and delete the project if it exists.
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find project: %w", err)
	}

	if project != nil {
		uc.logger.Infof("Deleting existing project %q", projectName)
		if err := uc.GitLabGateway.DeleteProject(project.ID); err != nil {
			return 0, 0, fmt.Errorf("failed to delete project: %w", err)
		}
	}

	project, err = uc.GitLabGateway.CreateProject(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create project: %w", err)
	}

	// Step 2: Collect all files recursively.
	var actions []gateway.CommitAction
	err = filepath.WalkDir(input.FolderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Skip .git and common dependency folders.
			name := filepath.Base(path)
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(input.FolderPath, path)
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file %q: %w", relPath, err)
		}

		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to read file %q: %w", relPath, err)
		}

		gitPath := filepath.ToSlash(relPath)
		actions = append(actions, gateway.CommitAction{
			Action:   "create",
			FilePath: gitPath,
			Content:  string(content),
			Encoding: "text",
		})

		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to walk folder: %w", err)
	}

	if len(actions) == 0 {
		return 0, 0, fmt.Errorf("no files found in folder %q", input.FolderPath)
	}

	uc.logger.Infof("Uploading %d file(s) to project %q, branch %q", len(actions), projectName, input.BranchName)

	// Step 3: Commit files via GitLab API.
	commitMessage := fmt.Sprintf("Add %d file(s) from folder via reposqueeze", len(actions))
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
