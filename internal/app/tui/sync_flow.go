package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/gitlab"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// handleSyncForm drives the sync screens:
//
//	Sync:        form -> plan (background) -> confirm -> run
//	Sync status: form -> run (prints the plan)
//	Sync init:   form -> run
//	Sync log:    form -> run (prints the journal)
func (m *appModel) handleSyncForm(msg formSubmittedMsg) (tea.Model, tea.Cmd) {
	f := msg.form

	switch msg.cmd {
	case cmdSync:
		in := usecase.SyncInput{
			RepoPath:  f.GetString("repoPath"),
			Mirror:    f.GetString("mirror"),
			Strategy:  f.GetString("strategy"),
			Autostash: f.GetBool("autostash"),
			Replay:    f.GetBool("replay"),
			Format:    formatFromForm(f),
		}
		m.pendingSync = &in
		m.state = statePlanning
		m.form = nil
		return m, m.planCmd(in)

	case cmdSyncConfirm:
		in := m.pendingSync
		m.pendingSync = nil
		if in == nil || !f.GetBool("confirm") {
			m.state = stateMenu
			m.form = nil
			return m, nil
		}
		input := *in
		return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
			res, err := d.syncUseCase().Sync(ctx, input)
			if err != nil {
				return runResultMsg{err: err}
			}
			for _, w := range res.Warnings {
				d.log.Warn(w)
			}
			if len(res.Conflicts) > 0 {
				d.log.Warn("Resolve the conflicts, commit, and run Sync again to push the result.")
			}
			return runResultMsg{summary: res.Summary(), count: res.Pulled + res.Pushed}
		})

	case cmdSyncStatus:
		in := usecase.SyncInput{RepoPath: f.GetString("repoPath"), Mirror: f.GetString("mirror")}
		return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
			plan, err := d.syncUseCase().Plan(ctx, in)
			if err != nil {
				return runResultMsg{err: err}
			}
			for _, line := range plan.Lines() {
				d.log.Info(line)
			}
			if plan.Empty() {
				return runResultMsg{summary: "up to date"}
			}
			return runResultMsg{summary: fmt.Sprintf("%d to pull, %d to push, %d changed on both sides",
				len(plan.RemoteChanges), len(plan.LocalChanges), len(plan.Conflicts))}
		})

	case cmdSyncInit:
		in := usecase.SyncInitInput{
			RepoPath:     f.GetString("repoPath"),
			Name:         strings.TrimSpace(f.GetString("name")),
			LocalBranch:  f.GetString("localBranch"),
			RemoteBranch: f.GetString("remoteBranch"),
			LocalSHA:     f.GetString("localSHA"),
			RemoteSHA:    f.GetString("remoteSHA"),
			Force:        f.GetBool("force"),
		}
		return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
			mirror, err := d.syncUseCase().Init(ctx, in)
			if err != nil {
				return runResultMsg{err: err}
			}
			return runResultMsg{summary: "mirror " + mirror.Name + " created"}
		})

	case cmdPullCommit:
		commits, _ := f.Get("commits").([]string)
		in := usecase.PullCommitInput{
			RepoPath: f.GetString("repoPath"),
			Commits:  commits,
			Strategy: f.GetString("strategy"),
			Format:   formatFromForm(f),
		}
		return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
			uc := usecase.NewPullCommitUseCase(d.syncGit, d.gitlab, state.NewFileStore(d.syncGit), d.log)
			res, err := uc.Execute(ctx, in)
			if err != nil {
				return runResultMsg{err: err}
			}
			if res.StoppedAt != nil {
				d.log.Warnf("Resolve conflicts in: %s", strings.Join(res.Conflicts, ", "))
				d.log.Warnf("Then commit:  %s", res.CommitCommand)
				if len(res.Remaining) > 0 {
					d.log.Warnf("Then pick the remaining %d commit(s) again from Pull commits.", len(res.Remaining))
				}
			}
			return runResultMsg{summary: res.Summary(), count: len(res.Picked)}
		})

	case cmdSyncLog:
		repoPath := f.GetString("repoPath")
		return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
			mirrors, err := d.syncUseCase().Mirrors(repoPath)
			if err != nil {
				return runResultMsg{err: err}
			}
			if len(mirrors) == 0 {
				return runResultMsg{err: fmt.Errorf("no mirrors configured; run Sync init first")}
			}
			for _, mr := range mirrors {
				for _, line := range usecase.MirrorLogLines(mr) {
					d.log.Info(line)
				}
			}
			return runResultMsg{summary: fmt.Sprintf("%d mirror(s)", len(mirrors))}
		})
	}
	return m, nil
}

// formatFromForm reads the commit type and task fields.
func formatFromForm(f *huh.Form) entity.CommitFormat {
	return entity.CommitFormat{
		Type: f.GetString("commitType"),
		Task: strings.TrimSpace(f.GetString("task")),
	}
}

// planCmd computes the sync plan in the background with silent gateways.
func (m *appModel) planCmd(in usecase.SyncInput) tea.Cmd {
	token, baseURL := m.gitlabToken, m.gitlabBaseURL
	return func() tea.Msg {
		quiet := logger.NewLoggerWithWriter(io.Discard)
		gitGW := git.NewOSExecGitGateway(quiet)
		gitlabGW := gitlab.NewHTTPGitLabGateway(token, quiet).WithBaseURL(baseURL)
		uc := usecase.NewSyncUseCase(gitGW, gitlabGW, state.NewFileStore(gitGW), quiet)
		plan, err := uc.Plan(context.Background(), in)
		return syncPlanMsg{plan: plan, err: err}
	}
}

func (m *appModel) handleSyncPlan(msg syncPlanMsg) (tea.Model, tea.Cmd) {
	// The user may have left the planning screen already.
	if m.state != statePlanning || m.pendingSync == nil {
		return m, nil
	}
	if msg.err != nil {
		m.pendingSync = nil
		err := msg.err
		return m.startRunWithSummary(func(context.Context, deps) runResultMsg { return runResultMsg{err: err} })
	}
	if msg.plan.Empty() {
		m.pendingSync = nil
		lines := msg.plan.Lines()
		return m.startRunWithSummary(func(_ context.Context, d deps) runResultMsg {
			for _, l := range lines {
				d.log.Info(l)
			}
			return runResultMsg{summary: "already up to date"}
		})
	}

	header := strings.Join(msg.plan.Lines(), "\n")
	if len(msg.plan.Conflicts) > 0 {
		header += "\n\n" + helpStyle.Render("Strategy for files changed on both sides: "+m.pendingSync.Strategy)
	}
	m.state = stateForm
	return m, m.showForm(formModel{cmd: cmdSyncConfirm, form: buildForm(cmdSyncConfirm, m.gitlabGateway, m.height), header: header})
}
