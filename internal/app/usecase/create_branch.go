package usecase

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reposqueeze/internal/domain/entity"
	"reposqueeze/internal/domain/gateway"
)

// CreateAndPushOrphanBranchUseCase is the use case for creating and pushing an orphan branch via API.
type CreateAndPushOrphanBranchUseCase struct {
	GitGateway    gateway.GitGateway
	GitLabGateway gateway.GitLabGateway
}

// Input represents the input data for the use case.
type Input struct {
	RepoPath        string
	BranchName      string
	GitLabProjectID string
	GitLabToken     string
}

// NewCreateAndPushOrphanBranchUseCase creates a new instance of the use case.
func NewCreateAndPushOrphanBranchUseCase(
	gitGateway gateway.GitGateway,
	gitLabGateway gateway.GitLabGateway,
) *CreateAndPushOrphanBranchUseCase {
	return &CreateAndPushOrphanBranchUseCase{
		GitGateway:    gitGateway,
		GitLabGateway: gitLabGateway,
	}
}

// Execute runs the use case.
func (uc *CreateAndPushOrphanBranchUseCase) Execute(ctx context.Context, input Input) error {
	// Step 1: Create the orphan branch locally and commit all files.
	repo := &entity.Repository{Path: input.RepoPath}
	branch := &entity.Branch{Name: input.BranchName}
	_, err := uc.GitGateway.CreateOrphanBranch(ctx, repo, branch)
	if err != nil {
		return fmt.Errorf("failed to create local orphan branch: %w", err)
	}

	// // Step 2: Create the branch on the remote using the commit SHA.
	// err = uc.GitLabGateway.CreateRemoteBranch(ctx, input.GitLabProjectID, input.BranchName, commitSHA, input.GitLabToken)
	// if err != nil {
	// 	return fmt.Errorf("failed to create remote branch: %w", err)
	// }

	// Step 3: Get a list of all files in the repository.
	files, err := uc.GitGateway.ListFiles(input.RepoPath)
	if err != nil {
		return fmt.Errorf("failed to list files in repo: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files found in the repository to commit")
	}

	// Step 4: Prepare the file actions for the GitLab API commit.
	var actions []gateway.CommitAction
	for _, file := range files {
		f, err := os.Open(filepath.Join(input.RepoPath, file))
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", file, err)
		}

		content, err := io.ReadAll(f)
		if err != nil {
			f.Close()
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}
		f.Close()

		actions = append(actions, gateway.CommitAction{
			Action:   "create",
			FilePath: file,
			Content:  base64.StdEncoding.EncodeToString(content),
		})
	}

	// Step 5: Commit the files via the GitLab API. This will be the second commit.
	commitMessage := fmt.Sprintf("Add project files to orphan branch %s", input.BranchName)
	err = uc.GitLabGateway.CommitFilesViaAPI(
		input.GitLabProjectID,
		input.BranchName,
		commitMessage,
		input.GitLabToken,
		actions,
	)
	if err != nil {
		return fmt.Errorf("failed to commit files via GitLab API: %w", err)
	}

	return nil
}