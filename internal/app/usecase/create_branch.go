package usecase

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// CreateAndPushOrphanBranchUseCase is the use case for creating and pushing an orphan branch via API.
type CreateAndPushOrphanBranchUseCase struct {
	GitGateway    gateway.GitGateway
	GitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

// Input represents the input data for the use case.
type Input struct {
	RepoPath     string
	BranchName   string
	SourceBranch string
}

// NewCreateAndPushOrphanBranchUseCase creates a new instance of the use case.
func NewCreateAndPushOrphanBranchUseCase(
	gitGateway gateway.GitGateway,
	gitLabGateway gateway.GitLabGateway,
	log logger.Logger,
) *CreateAndPushOrphanBranchUseCase {
	return &CreateAndPushOrphanBranchUseCase{
		GitGateway:    gitGateway,
		GitLabGateway: gitLabGateway,
		logger:        log,
	}
}

// Execute runs the use case.
func (uc *CreateAndPushOrphanBranchUseCase) Execute(ctx context.Context, input Input) (time.Duration, int, error) {
	// Step 1: Find and delete the project if it exists.
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	uc.logger.Info(projectName)
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, err
	}

	if project != nil {
		err = uc.GitLabGateway.DeleteProject(project.ID)
		if err != nil {
			return 0, 0, err
		}
	}

	project, err = uc.GitLabGateway.CreateProject(projectName)
	if err != nil {
		return 0, 0, err
	}

	// Step 2: Create the orphan branch locally and commit all files.
	repo := &entity.Repository{Path: input.RepoPath}
	branch := &entity.Branch{Name: input.BranchName}
	_, err = uc.GitGateway.CreateOrphanBranch(ctx, repo, branch, input.SourceBranch)
	if err != nil {
		return 0, 0, err
	}

	if err := uc.GitGateway.RemoveDirectory(input.RepoPath, "vendor"); err != nil {
		return 0, 0, err
	}

	// Step 3: Get a list of all files in the repository.
	files, err := uc.GitGateway.ListFiles(input.RepoPath)
	if err != nil {
		return 0, 0, err
	}

	defer func() {
		// Switch back to the original branch first
		if err := uc.GitGateway.CheckoutBranch(input.RepoPath, input.SourceBranch); err != nil {
			uc.logger.Warnf("Warning: failed to switch back to branch %s: %v\n", input.SourceBranch, err)
		}
		// Then delete the orphan branch
		if err := uc.GitGateway.DeleteLocalBranch(input.RepoPath, input.BranchName); err != nil {
			uc.logger.Warnf("Warning: failed to clean up local orphan branch %s: %v\n", input.BranchName, err)
		}
	}()

	// // Step 2: Create the branch on the remote using the commit SHA.
	// err = uc.GitLabGateway.CreateRemoteBranch(ctx, input.GitLabProjectID, input.BranchName, commitSHA)
	// if err != nil {
	// 	return fmt.Errorf("failed to create remote branch: %w", err)
	// }

	if len(files) == 0 {
		return 0, 0, err
	}

	// Step 4: Prepare the file actions for the GitLab API commit.
	var actions []gateway.CommitAction
	for _, file := range files {
		f, err := os.Open(filepath.Join(input.RepoPath, file))
		if err != nil {
			return 0, 0, err
		}

		content, err := io.ReadAll(f)
		if err != nil {
			f.Close()
			return 0, 0, err
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
	commitMessage := "Add project files to orphan branch " + input.BranchName
	startTime := time.Now()
	err = uc.GitLabGateway.CommitFilesViaAPI(
		strconv.Itoa(project.ID),
		input.BranchName,
		commitMessage,
		actions,
	)
	if err != nil {
		return 0, 0, err
	}
	duration := time.Since(startTime)

	return duration, len(files), nil
}
