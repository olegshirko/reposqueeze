package controller

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// CLIController handles the command-line interface logic.
type CLIController struct {
	createFromLocalUseCase  *usecase.CreateAndPushOrphanBranchUseCase
	createFromGitlabUseCase *usecase.CreateOrphanBranchFromGitlabUseCase
	pushFilesUseCase        *usecase.PushFilesUseCase
	pullFilesUseCase        *usecase.PullFilesUseCase
	pushFolderUseCase       *usecase.PushFolderUseCase
	cherryPickCommitUseCase *usecase.CherryPickCommitUseCase
	pushBranchUseCase       *usecase.PushBranchUseCase
	gitlabGateway           gateway.GitLabGateway
	logger                  logger.Logger
}

// NewCLIController creates a new instance of CLIController.
func NewCLIController(
	createFromLocalUseCase *usecase.CreateAndPushOrphanBranchUseCase,
	createFromGitlabUseCase *usecase.CreateOrphanBranchFromGitlabUseCase,
	pushFilesUseCase *usecase.PushFilesUseCase,
	pullFilesUseCase *usecase.PullFilesUseCase,
	pushFolderUseCase *usecase.PushFolderUseCase,
	cherryPickCommitUseCase *usecase.CherryPickCommitUseCase,
	pushBranchUseCase *usecase.PushBranchUseCase,
	gitlabGateway gateway.GitLabGateway,
	log logger.Logger,
) *CLIController {
	return &CLIController{
		createFromLocalUseCase:  createFromLocalUseCase,
		createFromGitlabUseCase: createFromGitlabUseCase,
		pushFilesUseCase:        pushFilesUseCase,
		pullFilesUseCase:        pullFilesUseCase,
		pushFolderUseCase:       pushFolderUseCase,
		cherryPickCommitUseCase: cherryPickCommitUseCase,
		pushBranchUseCase:       pushBranchUseCase,
		gitlabGateway:           gitlabGateway,
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
	case "push-files":
		c.handlePushFiles(remainingArgs)
	case "pull-files":
		c.handlePullFiles(remainingArgs)
	case "push-folder":
		c.handlePushFolder(remainingArgs)
	case "cherry-pick-commit":
		c.handleCherryPickCommit(remainingArgs)
	case "push-branch":
		c.handlePushBranch(remainingArgs)
	default:
		c.logger.Errorf("Unknown command: %s", command)
		c.printUsage()
	}
}

func (c *CLIController) handleCreateFromLocal(args []string) {
	fs := flag.NewFlagSet("create-from-local", flag.ExitOnError)
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "", "Name of the new orphan branch")
	sourceBranch := fs.String("from", "master", "Source branch to create orphan from")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *branchName == "" {
		fs.Usage()
		return
	}

	input := usecase.Input{
		RepoPath:     fs.Args()[0],
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
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "", "Name of the target branch (existing or new orphan)")
	ref := fs.String("ref", "", "GitLab branch or commit SHA to download archive from (default: repository default branch)")
	commit := fs.Bool("commit", false, "Auto-commit after unpacking")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *branchName == "" {
		fs.Usage()
		return
	}

	input := usecase.CreateOrphanBranchFromGitlabInput{
		RepoPath:   fs.Args()[0],
		BranchName: *branchName,
		Ref:        *ref,
		Commit:     *commit,
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

func (c *CLIController) handlePushFiles(args []string) {
	fs := flag.NewFlagSet("push-files", flag.ExitOnError)
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "", "Target branch on GitLab")
	files := fs.String("files", "", "Comma-separated list of relative file paths (e.g. README.md,docs/guide.md,src/main.go)")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *branchName == "" || *files == "" {
		fs.Usage()
		return
	}

	input := usecase.PushFilesInput{
		RepoPath:   fs.Args()[0],
		BranchName: *branchName,
		Files:      *files,
	}

	c.logger.Infof("Pushing files to project derived from: %s", input.RepoPath)
	duration, filesCount, err := c.pushFilesUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully pushed %d file(s) to branch '%s'.", filesCount, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
}

func (c *CLIController) handlePullFiles(args []string) {
	fs := flag.NewFlagSet("pull-files", flag.ExitOnError)
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "master", "Source branch on GitLab")
	files := fs.String("files", "", "Comma-separated list of relative file paths, e.g. README.md,docs/guide.md (optional)")
	commits := fs.Int("commits", 1, "How many latest commits to inspect for changed files when --files is omitted")
	gitAdd := fs.Bool("git-add", false, "Stage all downloaded files in local git (git add)")
	sinceCommit := fs.String("since-commit", "", "Pull all changes from this commit (SHA) up to HEAD of the branch")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 {
		fs.Usage()
		return
	}

	input := usecase.PullFilesInput{
		RepoPath:    fs.Args()[0],
		BranchName:  *branchName,
		Files:       *files,
		Commits:     *commits,
		GitAdd:      *gitAdd,
		SinceCommit: *sinceCommit,
	}

	c.logger.Infof("Pulling files from project derived from: %s", input.RepoPath)
	duration, filesCount, err := c.pullFilesUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully pulled %d file(s) from branch '%s'.", filesCount, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
}

func (c *CLIController) handlePushFolder(args []string) {
	fs := flag.NewFlagSet("push-folder", flag.ExitOnError)
	setFlagSetUsage(fs)
	projectName := fs.String("project-name", "", "GitLab project name (default: folder base name)")
	branchName := fs.String("branch-name", "master", "Target branch on GitLab")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 {
		fs.Usage()
		return
	}

	input := usecase.PushFolderInput{
		FolderPath:  fs.Args()[0],
		ProjectName: *projectName,
		BranchName:  *branchName,
	}

	c.logger.Infof("Pushing folder to GitLab: %s", input.FolderPath)
	duration, filesCount, err := c.pushFolderUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully pushed %d file(s) to project %q, branch %q.", filesCount, input.ProjectName, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
}

func (c *CLIController) handleCherryPickCommit(args []string) {
	fs := flag.NewFlagSet("cherry-pick-commit", flag.ExitOnError)
	setFlagSetUsage(fs)
	commitHash := fs.String("commit", "", "Local commit hash to cherry-pick")
	branchName := fs.String("branch-name", "master", "Target branch on GitLab")
	commitMessage := fs.String("message", "", "Custom commit message (default: use original commit message)")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *commitHash == "" {
		fs.Usage()
		return
	}

	input := usecase.CherryPickCommitInput{
		RepoPath:      fs.Args()[0],
		CommitHash:    *commitHash,
		BranchName:    *branchName,
		CommitMessage: *commitMessage,
	}

	c.logger.Infof("Cherry-picking commit %s from %s to branch %s", input.CommitHash, input.RepoPath, input.BranchName)
	duration, filesCount, err := c.cherryPickCommitUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully cherry-picked commit %s (%d file actions) to branch '%s'.", input.CommitHash, filesCount, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
}

func (c *CLIController) handlePushBranch(args []string) {
	fs := flag.NewFlagSet("push-branch", flag.ExitOnError)
	setFlagSetUsage(fs)
	sourceBranch := fs.String("source-branch", "", "Local source branch to push files from")
	branchName := fs.String("branch-name", "master", "Target branch on GitLab")
	commitMessage := fs.String("message", "", "Custom commit message (default: Push files from <source-branch>)")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *sourceBranch == "" {
		fs.Usage()
		return
	}

	input := usecase.PushBranchInput{
		RepoPath:      fs.Args()[0],
		SourceBranch:  *sourceBranch,
		BranchName:    *branchName,
		CommitMessage: *commitMessage,
	}

	c.logger.Infof("Pushing all files from local branch %s to GitLab branch %s", input.SourceBranch, input.BranchName)
	duration, filesCount, err := c.pushBranchUseCase.Execute(context.Background(), input)
	if err != nil {
		c.logger.Errorf("Error: %v", err)
		return
	}

	c.logger.Infof("Successfully pushed %d file(s) from branch '%s' to branch '%s'.", filesCount, input.SourceBranch, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
}

// reorderFlagsFirst moves all flags (and their values) to the front of args,
// so that positional arguments can appear before flags.
// This makes commands like "pull-files ../repo --branch-name main" work
// with the standard Go flag package.
func setFlagSetUsage(fs *flag.FlagSet) {
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage of %s:\n", fs.Name())
		var buf strings.Builder
		oldOutput := fs.Output()
		fs.SetOutput(&buf)
		fs.PrintDefaults()
		fs.SetOutput(oldOutput)

		output := buf.String()
		lines := strings.Split(output, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "  -") && !strings.HasPrefix(line, "  --") {
				lines[i] = "  --" + strings.TrimPrefix(line, "  -")
			}
		}
		fmt.Fprint(oldOutput, strings.Join(lines, "\n"))
	}
}

func reorderFlagsFirst(fs *flag.FlagSet, args []string) []string {
	var flags []string
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)

		// If the flag is in --key=value form, the value is already part of the flag.
		if strings.Contains(arg, "=") {
			continue
		}

		// Determine whether the flag takes a value or is a boolean flag.
		name := strings.TrimLeft(arg, "-")
		f := fs.Lookup(name)
		isBool := false
		if f != nil {
			_, isBool = f.Value.(interface{ IsBoolFlag() bool })
		}

		if !isBool && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags = append(flags, args[i+1])
			i++
		}
	}

	return append(flags, positional...)
}

func (c *CLIController) printUsage() {
	c.logger.Info("Usage: reposqueeze <command> <path> [options]")
	c.logger.Info("Commands:")
	c.logger.Info("  create-from-local   <path> --branch-name <name> [--from <source>]")
	c.logger.Info("  create-from-gitlab  <path> --branch-name <name>")
	c.logger.Info("  push-files          <path> --branch-name <name> --files <rel/path/file1>,<rel/path/file2>,...")
	c.logger.Info("                        Example: --files README.md,docs/guide.md,src/main.go")
	c.logger.Info("  pull-files          <path> --branch-name <name> [--files <rel/path/file1>,<rel/path/file2>,...]")
	c.logger.Info("                        Without --files: downloads changed files from the last N commit diffs (default N=1)")
	c.logger.Info("                        Example: --files README.md,docs/guide.md")
	c.logger.Info("                        Example: --commits 3 (pulls changed files from last 3 commits)")
	c.logger.Info("  push-folder         <path> [--project-name <name>] [--branch-name <name>]")
	c.logger.Info("  cherry-pick-commit  <path> --commit <hash> [--branch-name <name>] [--message <msg>]")
	c.logger.Info("                        Pushes a single local commit's file changes to an existing GitLab project.")
	c.logger.Info("  push-branch         <path> --source-branch <name> [--branch-name <name>] [--message <msg>]")
	c.logger.Info("                        Pushes all tracked files from a local branch to GitLab as one commit.")
}
