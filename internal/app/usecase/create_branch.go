package usecase

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
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
	SourceBranch    string
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
func (uc *CreateAndPushOrphanBranchUseCase) Execute(ctx context.Context, input Input) (time.Duration, int, error) {
	// Step 1: Find and delete the project if it exists.
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	fmt.Println(projectName)
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find project by name: %w", err)
	}

	if project != nil {
		err = uc.GitLabGateway.DeleteProject(project.ID)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to delete project: %w", err)
		}
	}

	project, err = uc.GitLabGateway.CreateProject(projectName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create project: %w", err)
	}

	// Step 2: Create the orphan branch locally and commit all files.
	repo := &entity.Repository{Path: input.RepoPath}
	branch := &entity.Branch{Name: input.BranchName}
	_, err = uc.GitGateway.CreateOrphanBranch(ctx, repo, branch, input.SourceBranch)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create local orphan branch: %w", err)
	}

	if err := uc.GitGateway.RemoveDirectory(input.RepoPath, "vendor"); err != nil {
		return 0, 0, fmt.Errorf("failed to remove vendor directory: %w", err)
	}

	defer func() {
		// Switch back to the original branch first
		if err := uc.GitGateway.CheckoutBranch(input.RepoPath, input.SourceBranch); err != nil {
			fmt.Printf("Warning: failed to switch back to branch %s: %v\n", input.SourceBranch, err)
		}
		// Then delete the orphan branch
		if err := uc.GitGateway.DeleteLocalBranch(input.RepoPath, input.BranchName); err != nil {
			fmt.Printf("Warning: failed to clean up local orphan branch %s: %v\n", input.BranchName, err)
		}
	}()

	// // Step 2: Create the branch on the remote using the commit SHA.
	// err = uc.GitLabGateway.CreateRemoteBranch(ctx, input.GitLabProjectID, input.BranchName, commitSHA, input.GitLabToken)
	// if err != nil {
	// 	return fmt.Errorf("failed to create remote branch: %w", err)
	// }

	// Step 3: Get a list of all files in the repository.
	files, err := uc.GitGateway.ListFiles(input.RepoPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list files in repo: %w", err)
	}

	if len(files) == 0 {
		return 0, 0, fmt.Errorf("no files found in the repository to commit")
	}

	// Step 4: Prepare the file actions for the GitLab API commit.
	var actions []gateway.CommitAction
	for _, file := range files {
		f, err := os.Open(filepath.Join(input.RepoPath, file))
		if err != nil {
			return 0, 0, fmt.Errorf("failed to open file %s: %w", file, err)
		}

		content, err := io.ReadAll(f)
		if err != nil {
			f.Close()
			return 0, 0, fmt.Errorf("failed to read file %s: %w", file, err)
		}
		f.Close()

		actions = append(actions, gateway.CommitAction{
			Action:   "create",
			FilePath: file,
			Content:  string(content),
			Encoding: "text",
		})
	}

	// Step 5: Commit the files via the GitLab API. This will be the second commit.
	commitMessage := fmt.Sprintf("Add project files to orphan branch %s", input.BranchName)
	startTime := time.Now()
	err = uc.GitLabGateway.CommitFilesViaAPI(
		strconv.Itoa(project.ID),
		input.BranchName,
		commitMessage,
		actions,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to commit files via GitLab API: %w", err)
	}
	duration := time.Since(startTime)

	return duration, len(files), nil
}
