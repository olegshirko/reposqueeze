package controller

import (
	"context"
	"flag"
	"fmt"
	"github.com/olegshirko/reposqueeze/internal/app/usecase"
)

// CLIController handles the command-line interface logic.
type CLIController struct {
	useCase *usecase.CreateAndPushOrphanBranchUseCase
}

// NewCLIController creates a new instance of CLIController.
func NewCLIController(uc *usecase.CreateAndPushOrphanBranchUseCase) *CLIController {
	return &CLIController{useCase: uc}
}

// Run executes the controller logic.
// It expects command-line arguments in a specific order:
// 1. Repository Path
// 2. Branch Name
// 3. GitLab Token
// An optional --from flag can be provided to specify a source branch.
func (c *CLIController) Run(args []string) {
	fs := flag.NewFlagSet("reposqueeze", flag.ExitOnError)
	repoPath := fs.String("repo-path", "", "Path to the repository")
	branchName := fs.String("branch-name", "", "Name of the new orphan branch")
	token := fs.String("token", "", "GitLab personal access token")
	sourceBranch := fs.String("from", "master", "Source branch to create orphan from")

	fs.Usage = func() {
		fmt.Println("Usage: go run cmd/app/main.go --repo-path <path> --branch-name <name> --token <token> [--from <source>]")
	}

	fs.Parse(args)

	if *repoPath == "" || *branchName == "" || *token == "" {
		fs.Usage()
		return
	}

	input := usecase.Input{
		RepoPath:     *repoPath,
		BranchName:   *branchName,
		GitLabToken:  *token,
		SourceBranch: *sourceBranch,
	}

	fmt.Printf("Starting process for repository: %s\n", input.RepoPath)
	duration, filesCount, err := c.useCase.Execute(context.Background(), input)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Successfully created and pushed orphan branch '%s'.\n", input.BranchName)
	fmt.Printf("Copied %d files in %s.\n", filesCount, duration)
}