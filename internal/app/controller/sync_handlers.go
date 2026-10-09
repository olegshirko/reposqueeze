package controller

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

func (c *CLIController) handleSyncInit(args []string) error {
	fs := flag.NewFlagSet("sync-init", flag.ExitOnError)
	setFlagSetUsage(fs)
	name := fs.String("name", "", "Mirror name (default: <local>-><project>:<remote>)")
	localBranch := fs.String("local-branch", "", "Local branch to mirror (default: current branch)")
	remoteBranch := fs.String("remote-branch", "", "GitLab branch to mirror to (default: same as local branch)")
	localSHA := fs.String("local-sha", "", "Local commit mirroring starts from (default: local branch head)")
	remoteSHA := fs.String("remote-sha", "", "GitLab commit with the same content (default: GitLab branch head)")
	recoverPoint := fs.Bool("recover", false, "Rebuild the sync point from the latest Reposqueeze-Remote commit trailer")
	force := fs.Bool("force", false, "Overwrite an existing mirror with the same name")

	fs.Parse(reorderFlagsFirst(fs, args))
	if len(fs.Args()) == 0 {
		fs.Usage()
		return errUsage
	}

	_, err := c.syncUseCase.Init(context.Background(), usecase.SyncInitInput{
		RepoPath:     fs.Args()[0],
		Name:         *name,
		LocalBranch:  *localBranch,
		RemoteBranch: *remoteBranch,
		LocalSHA:     *localSHA,
		RemoteSHA:    *remoteSHA,
		Recover:      *recoverPoint,
		Force:        *force,
	})
	return err
}

func (c *CLIController) handleSyncStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	setFlagSetUsage(fs)
	mirror := fs.String("mirror", "", "Mirror name (needed when the branch has several mirrors)")
	replay := fs.Bool("replay", false, "Also list GitLab commits that sync --replay would pull one by one")

	fs.Parse(reorderFlagsFirst(fs, args))
	if len(fs.Args()) == 0 {
		fs.Usage()
		return errUsage
	}

	plan, err := c.syncUseCase.Plan(context.Background(), usecase.SyncInput{RepoPath: fs.Args()[0], Mirror: *mirror, Replay: *replay})
	if err != nil {
		return err
	}
	for _, line := range plan.Lines() {
		fmt.Println(line)
	}
	return nil
}

func (c *CLIController) handleSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	setFlagSetUsage(fs)
	mirror := fs.String("mirror", "", "Mirror name (needed when the branch has several mirrors)")
	strategy := fs.String("strategy", usecase.StrategyMerge, "Files changed on both sides: merge (3-way), local, remote or abort")
	autostash := fs.Bool("autostash", false, "Stash uncommitted changes before syncing and re-apply them afterwards")
	dryRun := fs.Bool("dry-run", false, "Only show what would be done")
	message := fs.String("message", "", "Commit message for the GitLab commit")
	replay := fs.Bool("replay", false, "Pull GitLab commits one by one as separate local commits (original message, author, date)")
	commitType, task := formatFlags(fs)

	fs.Parse(reorderFlagsFirst(fs, args))
	if len(fs.Args()) == 0 {
		fs.Usage()
		return errUsage
	}

	res, err := c.syncUseCase.Sync(context.Background(), usecase.SyncInput{
		RepoPath:  fs.Args()[0],
		Mirror:    *mirror,
		Strategy:  *strategy,
		DryRun:    *dryRun,
		Autostash: *autostash,
		Message:   *message,
		Replay:    *replay,
		Format:    entity.CommitFormat{Type: *commitType, Task: *task},
	})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		c.logger.Warn(w)
	}
	c.logger.Infof("Sync: %s", res.Summary())
	if len(res.Conflicts) > 0 {
		c.logger.Warn("Resolve the conflicts, commit, and run sync again to push the result.")
		return errConflicts
	}
	return nil
}

func (c *CLIController) handleSyncLog(args []string) error {
	fs := flag.NewFlagSet("sync-log", flag.ExitOnError)
	setFlagSetUsage(fs)
	mirror := fs.String("mirror", "", "Show only this mirror")

	fs.Parse(reorderFlagsFirst(fs, args))
	if len(fs.Args()) == 0 {
		fs.Usage()
		return errUsage
	}

	mirrors, err := c.syncUseCase.Mirrors(fs.Args()[0])
	if err != nil {
		return err
	}
	if len(mirrors) == 0 {
		fmt.Println("No mirrors configured; run sync-init first.")
		return nil
	}
	for _, m := range mirrors {
		if *mirror != "" && m.Name != *mirror {
			continue
		}
		printMirrorLog(m)
	}
	return nil
}

func printMirrorLog(m entity.Mirror) {
	for _, line := range usecase.MirrorLogLines(m) {
		fmt.Println(line)
	}
	fmt.Println()
}

// formatFlags registers --type and --task.
func formatFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("type", "", "Commit type for local commits: fix, feat, test, ... (remembered per mirror)"),
		fs.String("task", "", "Task reference appended to local commit titles, e.g. TASK-123 (remembered per mirror)")
}

func (c *CLIController) handlePullCommit(args []string) error {
	fs := flag.NewFlagSet("pull-commit", flag.ExitOnError)
	setFlagSetUsage(fs)
	commits := fs.String("commit", "", "GitLab commit SHA(s) to bring in, comma-separated")
	list := fs.Bool("list", false, "List GitLab commits of --branch-name to choose from")
	branch := fs.String("branch-name", "master", "GitLab branch for --list")
	limit := fs.Int("limit", 30, "How many commits --list shows")
	strategy := fs.String("strategy", usecase.StrategyMerge, "Local changes in the same files: merge (stop on conflict like cherry-pick), local, remote or abort")
	commitType, task := formatFlags(fs)

	fs.Parse(reorderFlagsFirst(fs, args))
	if len(fs.Args()) == 0 || (!*list && *commits == "") {
		fs.Usage()
		return errUsage
	}
	repoPath := fs.Args()[0]

	if *list {
		found, picked, err := c.pullCommitUseCase.ListCommits(repoPath, *branch, *limit)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, cm := range found {
			mark := " "
			if _, ok := picked[cm.ID]; ok {
				mark = "*"
			}
			date := cm.AuthoredDate
			if len(date) >= 10 {
				date = date[:10]
			}
			title := cm.Title
			if title == "" {
				title = strings.SplitN(cm.Message, "\n", 2)[0]
			}
			fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\n", mark, shortSHA(cm.ID), date, cm.AuthorName, title)
		}
		w.Flush()
		fmt.Println("\nBring commits in with: reposqueeze pull-commit", repoPath, "--commit <sha>,<sha> [--type fix --task TASK-1]")
		return nil
	}

	res, err := c.pullCommitUseCase.Execute(context.Background(), usecase.PullCommitInput{
		RepoPath: repoPath,
		Commits:  strings.Split(*commits, ","),
		Strategy: *strategy,
		Format:   entity.CommitFormat{Type: *commitType, Task: *task},
	})
	if err != nil {
		return err
	}
	c.logger.Infof("pull-commit: %s", res.Summary())
	if res.StoppedAt != nil {
		c.logger.Warnf("Resolve conflicts in: %s", strings.Join(res.Conflicts, ", "))
		c.logger.Warnf("Then commit:  %s", res.CommitCommand)
		if len(res.Remaining) > 0 {
			c.logger.Warnf("Then continue: reposqueeze pull-commit %s --commit %s", repoPath, strings.Join(shortAll(res.Remaining), ","))
		}
		return errConflicts
	}
	return nil
}

func shortAll(shas []string) []string {
	out := make([]string, len(shas))
	for i, s := range shas {
		out[i] = shortSHA(s)
	}
	return out
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
