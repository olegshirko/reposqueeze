package controller

import (
	"context"
	"fmt"
	"reposqueeze/internal/app/usecase"
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
// 3. GitLab Project ID
// 4. GitLab Token
func (c *CLIController) Run(args []string) {
	if len(args) < 4 {
		fmt.Println("Usage: go run cmd/app/main.go <repoPath> <branchName> <projectID> <token>")
		return
	}

	input := usecase.Input{
		RepoPath:        args[0],
		BranchName:      args[1],
		GitLabProjectID: args[2],
		GitLabToken:     args[3],
	}

	fmt.Printf("Starting process for repository: %s\n", input.RepoPath)
	err := c.useCase.Execute(context.Background(), input)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Successfully created and pushed orphan branch '%s' to project '%s'.\n", input.BranchName, input.GitLabProjectID)
}