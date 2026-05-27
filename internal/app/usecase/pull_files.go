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
	GitGateway    gateway.GitGateway
	GitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// PullFilesInput represents the input data for the pull-files use case.
type PullFilesInput struct {
	RepoPath    string
	BranchName  string
	Files       string // comma-separated list; if empty, pulls files from commit diffs
	Commits     int    // number of latest commits to process (default 1)
	GitAdd      bool   // if true, runs git add + commit after downloading
	SinceCommit string // if set, pulls all changes from this commit to HEAD via compare API
}

// NewPullFilesUseCase creates a new instance of PullFilesUseCase.
func NewPullFilesUseCase(gitGateway gateway.GitGateway, gitLabGateway gateway.GitLabGateway, log logger.Logger) *PullFilesUseCase {
	return &PullFilesUseCase{
		GitGateway:    gitGateway,
		GitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *PullFilesUseCase) Execute(ctx context.Context, input PullFilesInput) (time.Duration, int, error) {
	if input.Commits <= 0 {
		input.Commits = 1
	}

	// Step 1: Find the project by name.
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find project: %w", err)
	}
	if project == nil {
		return 0, 0, fmt.Errorf("project %q not found on GitLab", projectName)
	}

	// Ensure target directory exists.
	if err := os.MkdirAll(input.RepoPath, 0o755); err != nil {
		return 0, 0, fmt.Errorf("failed to create repo path: %w", err)
	}

	uc.logger.Infof("Saving files to: %s", input.RepoPath)

	startTime := time.Now()
	downloaded := 0

	if input.Files != "" {
		// Explicit file list provided.
		parts := strings.Split(input.Files, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if err := uc.downloadFile(project.ID, p, input.BranchName, input.RepoPath); err != nil {
				return 0, 0, err
			}
			downloaded++
		}
	} else if input.SinceCommit != "" {
		// Pull all changes from SinceCommit to HEAD via compare API.
		uc.logger.Infof("Comparing %s..%s", input.SinceCommit, input.BranchName)

		diffs, err := uc.GitLabGateway.GetCompareDiff(project.ID, input.SinceCommit, input.BranchName)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to get compare diff: %w", err)
		}

		for _, d := range diffs {
			if d.DeletedFile {
				localPath := filepath.Join(input.RepoPath, filepath.FromSlash(d.NewPath))
				if err := os.RemoveAll(localPath); err != nil {
					return 0, 0, fmt.Errorf("failed to delete file %q: %w", d.NewPath, err)
				}
				uc.logger.Infof("Deleted: %s", d.NewPath)
				continue
			}

			if d.RenamedFile {
				oldLocalPath := filepath.Join(input.RepoPath, filepath.FromSlash(d.OldPath))
				if err := os.RemoveAll(oldLocalPath); err != nil {
					return 0, 0, fmt.Errorf("failed to delete old file %q: %w", d.OldPath, err)
				}
				uc.logger.Infof("Deleted (rename): %s", d.OldPath)
			}

			if err := uc.downloadFile(project.ID, d.NewPath, input.BranchName, input.RepoPath); err != nil {
				return 0, 0, err
			}
			downloaded++
		}
	} else {
		// Resolve files from the latest N commits.
		commits, err := uc.GitLabGateway.GetCommits(project.ID, input.BranchName, input.Commits)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to get commits: %w", err)
		}
		if len(commits) == 0 {
			return 0, 0, fmt.Errorf("no commits found for branch %q", input.BranchName)
		}

		uc.logger.Infof("Processing %d commit(s) from branch %s", len(commits), input.BranchName)

		// Process from oldest to newest so newer commits overwrite older ones.
		for i := len(commits) - 1; i >= 0; i-- {
			commit := commits[i]
			uc.logger.Infof("Commit %s: %s", commit.ID, strings.Split(commit.Message, "\n")[0])

			diffs, err := uc.GitLabGateway.GetCommitDiff(project.ID, commit.ID)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to get diff for commit %s: %w", commit.ID, err)
			}

			for _, d := range diffs {
				if d.DeletedFile {
					localPath := filepath.Join(input.RepoPath, filepath.FromSlash(d.NewPath))
					if err := os.RemoveAll(localPath); err != nil {
						return 0, 0, fmt.Errorf("failed to delete file %q: %w", d.NewPath, err)
					}
					uc.logger.Infof("Deleted: %s", d.NewPath)
					continue
				}

				if d.RenamedFile {
					oldLocalPath := filepath.Join(input.RepoPath, filepath.FromSlash(d.OldPath))
					if err := os.RemoveAll(oldLocalPath); err != nil {
						return 0, 0, fmt.Errorf("failed to delete old file %q: %w", d.OldPath, err)
					}
					uc.logger.Infof("Deleted (rename): %s", d.OldPath)
				}

				if err := uc.downloadFile(project.ID, d.NewPath, commit.ID, input.RepoPath); err != nil {
					return 0, 0, err
				}
				downloaded++
			}
		}
	}

	if input.GitAdd {
		if err := uc.GitGateway.AddAll(input.RepoPath); err != nil {
			return 0, 0, fmt.Errorf("failed to stage files locally: %w", err)
		}
		uc.logger.Info("Files staged locally (git add)")
	}

	duration := time.Since(startTime)
	return duration, downloaded, nil
}

func (uc *PullFilesUseCase) downloadFile(projectID int, filePath, ref, repoPath string) error {
	content, err := uc.GitLabGateway.GetRawFile(projectID, filePath, ref)
	if err != nil {
		return fmt.Errorf("failed to download file %q: %w", filePath, err)
	}

	localPath := filepath.Join(repoPath, filepath.FromSlash(filePath))
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %q: %w", filePath, err)
	}

	if err := os.WriteFile(localPath, content, 0o644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filePath, err)
	}

	uc.logger.Infof("Pulled: %s -> %s", filePath, localPath)
	return nil
}
