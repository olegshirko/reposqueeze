package controller

import (
	"context"
	"flag"
	"fmt"
	"os"
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
	fmt.Printf("Mirror %s  (local %s  <->  %s/%s, project id %d)\n", m.Name, m.LocalBranch, m.ProjectName, m.RemoteBranch, m.ProjectID)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  WHEN\tDIR\tLOCAL\tGITLAB\tPULLED\tPUSHED\tNOTES")
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\t\torigin\n", m.Origin.At.Local().Format("2006-01-02 15:04"), entity.SyncInit, shortSHA(m.Origin.LocalSHA), shortSHA(m.Origin.RemoteSHA))
	for _, j := range m.Journal {
		notes := ""
		if len(j.Merged) > 0 {
			notes += fmt.Sprintf("merged %d ", len(j.Merged))
		}
		if len(j.Conflicts) > 0 {
			notes += fmt.Sprintf("conflicts: %v ", j.Conflicts)
		}
		if j.RemoteMoved {
			notes += "gitlab moved during sync"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%d\t%d\t%s\n", j.At.Local().Format("2006-01-02 15:04"), j.Direction,
			shortSHA(j.LocalSHA), shortSHA(j.RemoteSHA), j.Pulled, j.Pushed, notes)
	}
	w.Flush()
	if m.PendingMerge != nil {
		fmt.Printf("  pending conflicts: %v\n", m.PendingMerge.Files)
	}
	fmt.Println()
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
