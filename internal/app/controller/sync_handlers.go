package controller

import (
	"context"
	"flag"
	"fmt"

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

	fs.Parse(reorderFlagsFirst(fs, args))
	if len(fs.Args()) == 0 {
		fs.Usage()
		return errUsage
	}

	plan, err := c.syncUseCase.Plan(context.Background(), usecase.SyncInput{RepoPath: fs.Args()[0], Mirror: *mirror})
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
