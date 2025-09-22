package controller

import (
	"context"
	"flag"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway" // Добавлено
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// CLIController handles the command-line interface logic.
type CLIController struct {
	createFromLocalUseCase  *usecase.CreateAndPushOrphanBranchUseCase
	createFromGitlabUseCase *usecase.CreateOrphanBranchFromGitlabUseCase
	gitlabGateway           gateway.GitLabGateway // Изменено
	logger                  logger.Logger
}

// NewCLIController creates a new instance of CLIController.
func NewCLIController(
	createFromLocalUseCase *usecase.CreateAndPushOrphanBranchUseCase,
	createFromGitlabUseCase *usecase.CreateOrphanBranchFromGitlabUseCase,
	gitlabGateway gateway.GitLabGateway, // Изменено
	log logger.Logger,
) *CLIController {
	return &CLIController{
		createFromLocalUseCase:  createFromLocalUseCase,
		createFromGitlabUseCase: createFromGitlabUseCase,
		gitlabGateway:           gitlabGateway, // Добавлено
		logger:                  log,
	}
}

// Run executes the controller logic.
func (c *CLIController) Run(args []string) {
	if len(args) < 1 {
		c.printUsage()
		return
	}

	command := args[0]
	remainingArgs := args[1:]

	switch command {
	case "create-from-local":
		c.handleCreateFromLocal(remainingArgs)
	case "create-from-gitlab":
		c.handleCreateFromGitlab(remainingArgs)
	default:
		c.logger.Errorf("Unknown command: %s", command)
		c.printUsage()
	}
}

func (c *CLIController) handleCreateFromLocal(args []string) {
	fs := flag.NewFlagSet("create-from-local", flag.ExitOnError)
	repoPath := fs.String("repo-path", "", "Path to the repository")
	branchName := fs.String("branch-name", "", "Name of the new orphan branch")
	sourceBranch := fs.String("from", "master", "Source branch to create orphan from")

	fs.Parse(args)

	if *repoPath == "" || *branchName == "" {
		fs.Usage()
		return
	}

	input := usecase.Input{
		RepoPath:     *repoPath,
		BranchName:   *branchName,
		SourceBranch: *sourceBranch,
	}

	c.logger.Infof("Starting process for repository: %s", input.RepoPath)
	duration, filesCount, err := c.createFromLocalUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully created and pushed orphan branch '%s'.", input.BranchName)
	c.logger.Infof("Copied %d files in %s.", filesCount, duration)
}

func (c *CLIController) handleCreateFromGitlab(args []string) {
	fs := flag.NewFlagSet("create-from-gitlab", flag.ExitOnError)
	repoPath := fs.String("repo-path", "", "Path to the repository")
	branchName := fs.String("branch-name", "", "Name of the new orphan branch")

	fs.Parse(args)

	if *repoPath == "" || *branchName == "" {
		fs.Usage()
		return
	}

	input := usecase.CreateOrphanBranchFromGitlabInput{
		RepoPath:   *repoPath,
		BranchName: *branchName,
	}

	c.logger.Infof("Starting process for repository: %s", input.RepoPath)
	duration, filesCount, err := c.createFromGitlabUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully created and pushed orphan branch '%s'.", input.BranchName)
	c.logger.Infof("Copied %d files in %s.", filesCount, duration)
}

func (c *CLIController) printUsage() {
	c.logger.Info("Usage: go run cmd/app/main.go <command> [options]")
	c.logger.Info("Commands:")
	c.logger.Info("  create-from-local   --repo-path <path> --branch-name <name> [--from <source>]")
	c.logger.Info("  create-from-gitlab  --repo-path <path> --branch-name <name>")
}
