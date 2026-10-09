package controller

import (
	"context"
	"errors"
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
	syncUseCase             *usecase.SyncUseCase
	pullCommitUseCase       *usecase.PullCommitUseCase
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
	syncUseCase *usecase.SyncUseCase,
	pullCommitUseCase *usecase.PullCommitUseCase,
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
		syncUseCase:             syncUseCase,
		pullCommitUseCase:       pullCommitUseCase,
		gitlabGateway:           gitlabGateway,
		logger:                  log,
	}
}

// Exit codes returned by Run.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
	// ExitConflicts means sync completed but left files to resolve manually.
	ExitConflicts = 3
)

// errUsage signals that the command was called with missing or invalid arguments.
var errUsage = errors.New("invalid usage")

// errConflicts signals a finished sync that left conflicts to resolve.
var errConflicts = errors.New("sync finished with conflicts")

// Run executes the controller logic and returns a process exit code.
func (c *CLIController) Run(args []string) int {
	if len(args) < 1 {
		c.printUsage()
		return ExitUsage
	}

	command := args[0]
	remainingArgs := args[1:]

	var err error
	switch command {
	case "create-from-local":
		err = c.handleCreateFromLocal(remainingArgs)
	case "create-from-gitlab":
		err = c.handleCreateFromGitlab(remainingArgs)
	case "push-files":
		err = c.handlePushFiles(remainingArgs)
	case "pull-files":
		err = c.handlePullFiles(remainingArgs)
	case "push-folder":
		err = c.handlePushFolder(remainingArgs)
	case "cherry-pick-commit":
		err = c.handleCherryPickCommit(remainingArgs)
	case "push-branch":
		err = c.handlePushBranch(remainingArgs)
	case "sync-init":
		err = c.handleSyncInit(remainingArgs)
	case "status", "sync-status":
		err = c.handleSyncStatus(remainingArgs)
	case "sync":
		err = c.handleSync(remainingArgs)
	case "sync-log":
		err = c.handleSyncLog(remainingArgs)
	case "pull-commit":
		err = c.handlePullCommit(remainingArgs)
	case "help", "-h", "--help":
		c.printUsage()
		return ExitOK
	default:
		c.logger.Errorf("Unknown command: %s", command)
		c.printUsage()
		return ExitUsage
	}

	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errUsage):
		return ExitUsage
	case errors.Is(err, errConflicts):
		return ExitConflicts
	default:
		c.logger.Errorf("Error: %v", err)
		return ExitError
	}
}

func (c *CLIController) handleCreateFromLocal(args []string) error {
	fs := flag.NewFlagSet("create-from-local", flag.ExitOnError)
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "", "Name of the new orphan branch")
	sourceBranch := fs.String("from", "master", "Source branch to create orphan from")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *branchName == "" {
		fs.Usage()
		return errUsage
	}

	input := usecase.Input{
		RepoPath:     fs.Args()[0],
		BranchName:   *branchName,
		SourceBranch: *sourceBranch,
	}

	c.logger.Infof("Starting process for repository: %s", input.RepoPath)
	duration, filesCount, err := c.createFromLocalUseCase.Execute(context.Background(), input)
	if err != nil {
		return err
	}

	c.logger.Infof("Successfully created and pushed orphan branch '%s'.", input.BranchName)
	c.logger.Infof("Copied %d files in %s.", filesCount, duration)
	return nil
}

func (c *CLIController) handleCreateFromGitlab(args []string) error {
	fs := flag.NewFlagSet("create-from-gitlab", flag.ExitOnError)
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "", "Name of the target branch (existing or new orphan)")
	ref := fs.String("ref", "", "GitLab branch or commit SHA to download archive from (default: repository default branch)")
	commit := fs.Bool("commit", false, "Auto-commit after unpacking")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *branchName == "" {
		fs.Usage()
		return errUsage
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
		return err
	}

	c.logger.Infof("Successfully created and pushed orphan branch '%s'.", input.BranchName)
	c.logger.Infof("Copied %d files in %s.", filesCount, duration)
	return nil
}

func (c *CLIController) handlePushFiles(args []string) error {
	fs := flag.NewFlagSet("push-files", flag.ExitOnError)
	setFlagSetUsage(fs)
	branchName := fs.String("branch-name", "", "Target branch on GitLab")
	files := fs.String("files", "", "Comma-separated list of relative file paths (e.g. README.md,docs/guide.md,src/main.go)")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *branchName == "" || *files == "" {
		fs.Usage()
		return errUsage
	}

	input := usecase.PushFilesInput{
		RepoPath:   fs.Args()[0],
		BranchName: *branchName,
		Files:      *files,
	}

	c.logger.Infof("Pushing files to project derived from: %s", input.RepoPath)
	duration, filesCount, err := c.pushFilesUseCase.Execute(context.Background(), input)
	if err != nil {
		return err
	}

	c.logger.Infof("Successfully pushed %d file(s) to branch '%s'.", filesCount, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
	return nil
}

func (c *CLIController) handlePullFiles(args []string) error {
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
		return errUsage
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
		return err
	}

	c.logger.Infof("Successfully pulled %d file(s) from branch '%s'.", filesCount, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
	return nil
}

func (c *CLIController) handlePushFolder(args []string) error {
	fs := flag.NewFlagSet("push-folder", flag.ExitOnError)
	setFlagSetUsage(fs)
	projectName := fs.String("project-name", "", "GitLab project name (default: folder base name)")
	branchName := fs.String("branch-name", "master", "Target branch on GitLab")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 {
		fs.Usage()
		return errUsage
	}

	input := usecase.PushFolderInput{
		FolderPath:  fs.Args()[0],
		ProjectName: *projectName,
		BranchName:  *branchName,
	}

	c.logger.Infof("Pushing folder to GitLab: %s", input.FolderPath)
	duration, filesCount, err := c.pushFolderUseCase.Execute(context.Background(), input)
	if err != nil {
		return err
	}

	c.logger.Infof("Successfully pushed %d file(s) to project %q, branch %q.", filesCount, input.ProjectName, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
	return nil
}

func (c *CLIController) handleCherryPickCommit(args []string) error {
	fs := flag.NewFlagSet("cherry-pick-commit", flag.ExitOnError)
	setFlagSetUsage(fs)
	commitHash := fs.String("commit", "", "Local commit hash to cherry-pick")
	branchName := fs.String("branch-name", "master", "Target branch on GitLab")
	commitMessage := fs.String("message", "", "Custom commit message (default: use original commit message)")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *commitHash == "" {
		fs.Usage()
		return errUsage
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
		return err
	}

	c.logger.Infof("Successfully cherry-picked commit %s (%d file actions) to branch '%s'.", input.CommitHash, filesCount, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
	return nil
}

func (c *CLIController) handlePushBranch(args []string) error {
	fs := flag.NewFlagSet("push-branch", flag.ExitOnError)
	setFlagSetUsage(fs)
	sourceBranch := fs.String("source-branch", "", "Local source branch to push files from")
	branchName := fs.String("branch-name", "master", "Target branch on GitLab")
	commitMessage := fs.String("message", "", "Custom commit message (default: Push files from <source-branch>)")

	fs.Parse(reorderFlagsFirst(fs, args))

	if len(fs.Args()) == 0 || *sourceBranch == "" {
		fs.Usage()
		return errUsage
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
		return err
	}

	c.logger.Infof("Successfully pushed %d file(s) from branch '%s' to branch '%s'.", filesCount, input.SourceBranch, input.BranchName)
	c.logger.Infof("Operation took %s.", duration)
	return nil
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
	fmt.Println("Usage: reposqueeze <command> <path> [options]")
	fmt.Println("Commands:")
	fmt.Println("  create-from-local   <path> --branch-name <name> [--from <source>]")
	fmt.Println("  create-from-gitlab  <path> --branch-name <name>")
	fmt.Println("  push-files          <path> --branch-name <name> --files <rel/path/file1>,<rel/path/file2>,...")
	fmt.Println("                        Example: --files README.md,docs/guide.md,src/main.go")
	fmt.Println("  pull-files          <path> --branch-name <name> [--files <rel/path/file1>,<rel/path/file2>,...]")
	fmt.Println("                        Without --files: downloads changed files from the last N commit diffs (default N=1)")
	fmt.Println("                        Example: --files README.md,docs/guide.md")
	fmt.Println("                        Example: --commits 3 (pulls changed files from last 3 commits)")
	fmt.Println("  push-folder         <path> [--project-name <name>] [--branch-name <name>]")
	fmt.Println("  cherry-pick-commit  <path> --commit <hash> [--branch-name <name>] [--message <msg>]")
	fmt.Println("                        Pushes a single local commit's file changes to an existing GitLab project.")
	fmt.Println("  push-branch         <path> --source-branch <name> [--branch-name <name>] [--message <msg>]")
	fmt.Println("                        Pushes all tracked files from a local branch to GitLab as one commit.")
	fmt.Println("")
	fmt.Println("Two-way sync (a mirror = local branch <-> GitLab branch, with a journal of matching commits):")
	fmt.Println("  sync-init           <path> [--remote-branch <name>] [--local-branch <name>] [--local-sha <sha>] [--remote-sha <sha>] [--name <mirror>] [--force]")
	fmt.Println("                        Records which local commit corresponds to which GitLab commit (default: both heads).")
	fmt.Println("  status              <path> [--mirror <name>] [--replay]")
	fmt.Println("                        Shows what sync would pull, push and merge.")
	fmt.Println("  sync                <path> [--mirror <name>] [--strategy merge|local|remote|abort] [--autostash] [--dry-run] [--message <msg>] [--replay] [--type <fix|feat|...>] [--task <TASK-1>]")
	fmt.Println("                        Pulls GitLab changes, pushes local commits, 3-way merges files changed on both sides.")
	fmt.Println("                        --replay: one local commit per GitLab commit, keeping message, author and date.")
	fmt.Println("  sync-log            <path> [--mirror <name>]")
	fmt.Println("                        Prints the local <-> GitLab commit correspondence journal.")
	fmt.Println("")
	fmt.Println("Selected GitLab commits:")
	fmt.Println("  pull-commit         <path> --list [--branch-name <name>] [--limit <n>]")
	fmt.Println("                        Lists GitLab commits to choose from (* = already brought in).")
	fmt.Println("  pull-commit         <path> --commit <sha>[,<sha>...] [--strategy merge|local|remote|abort] [--type <type>] [--task <TASK-1>]")
	fmt.Println("                        Brings the chosen commits into the current branch, one local commit each (oldest first).")
	fmt.Println("")
	fmt.Println("  --type/--task make local commit messages look like \"fix: <title> TASK-1\"; sync remembers them per mirror.")
	fmt.Println("  --from/--to YYYY-MM-DD spread the dates of brought-in commits evenly over working days")
	fmt.Println("    (pull-commit, sync --replay); tune with --hours 10-19, --weekends, --jitter 20m; preview with --dry-run.")
	fmt.Println("")
	fmt.Println("  tui                 Interactive mode")
}
