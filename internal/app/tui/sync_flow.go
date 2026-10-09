package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

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
		if in.Replay {
			spread, err := spreadFromForm(f)
			if err != nil {
				return m.showError(err)
			}
			in.Spread = spread
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
		spread, err := spreadFromForm(f)
		if err != nil {
			return m.showError(err)
		}
		in := usecase.PullCommitInput{
			RepoPath: f.GetString("repoPath"),
			Commits:  commits,
			Strategy: f.GetString("strategy"),
			Format:   formatFromForm(f),
			Spread:   spread,
		}
		m.pendingPick = &in
		m.state = statePlanning
		m.form = nil
		return m, m.pickPlanCmd(in)

	case cmdPickConfirm:
		in := m.pendingPick
		m.pendingPick = nil
		if in == nil || !f.GetBool("confirm") {
			m.state = stateMenu
			m.form = nil
			return m, nil
		}
		return m.startPick(*in)

	case cmdSyncLog:
		return m.showSyncLog(f.GetString("repoPath"))
	}
	return m, nil
}

// startPick runs pull-commit.
func (m *appModel) startPick(in usecase.PullCommitInput) (tea.Model, tea.Cmd) {
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
}

// showSyncLog prints the journal of every mirror.
func (m *appModel) showSyncLog(repoPath string) (tea.Model, tea.Cmd) {
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

// showError displays err on the result screen.
func (m *appModel) showError(err error) (tea.Model, tea.Cmd) {
	return m.startRunWithSummary(func(context.Context, deps) runResultMsg { return runResultMsg{err: err} })
}

// spreadFromForm reads the date-spreading answers. The random seed is fixed
// here, so the preview and the real run produce the same dates.
func spreadFromForm(f *huh.Form) (*usecase.DateSpread, error) {
	if !f.GetBool("spread") {
		return nil, nil
	}
	s, err := usecase.ParseDateSpread(f.GetString("spreadFrom"), f.GetString("spreadTo"),
		f.GetString("spreadHours"), f.GetBool("spreadWeekends"), 20*time.Minute)
	if err != nil {
		return nil, err
	}
	s.Seed = time.Now().UnixNano()
	return s, nil
}

// pickPlanMsg delivers the dry run of a pull-commit.
type pickPlanMsg struct {
	res *usecase.PullCommitResult
	err error
}

// pickPlanCmd runs pull-commit as a dry run in the background.
func (m *appModel) pickPlanCmd(in usecase.PullCommitInput) tea.Cmd {
	token, baseURL := m.gitlabToken, m.gitlabBaseURL
	in.DryRun = true
	return func() tea.Msg {
		quiet := logger.NewLoggerWithWriter(io.Discard)
		gitGW := git.NewOSExecGitGateway(quiet)
		gitlabGW := gitlab.NewHTTPGitLabGateway(token, quiet).WithBaseURL(baseURL)
		res, err := usecase.NewPullCommitUseCase(gitGW, gitlabGW, state.NewFileStore(gitGW), quiet).
			Execute(context.Background(), in)
		return pickPlanMsg{res: res, err: err}
	}
}

func (m *appModel) handlePickPlan(msg pickPlanMsg) (tea.Model, tea.Cmd) {
	if m.state != statePlanning || m.pendingPick == nil {
		return m, nil
	}
	if msg.err != nil {
		m.pendingPick = nil
		return m.showError(msg.err)
	}
	if len(msg.res.Plan) == 0 {
		m.pendingPick = nil
		return m.showError(fmt.Errorf("nothing to pick: all selected commits are already in the local branch"))
	}
	lines := []string{fmt.Sprintf("Local commits to create (%d):", len(msg.res.Plan))}
	for _, l := range msg.res.Plan {
		lines = append(lines, "  "+l)
	}
	if n := len(msg.res.Skipped); n > 0 {
		lines = append(lines, "", fmt.Sprintf("Skipped, already in the local branch: %d", n))
	}
	m.state = stateForm
	return m, m.showForm(formModel{cmd: cmdPickConfirm, form: buildForm(cmdPickConfirm, m.gitlabGateway, m.height),
		header: strings.Join(lines, "\n")})
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
	if sp := m.pendingSync.Spread; sp != nil && len(msg.plan.RemoteCommits) > 0 {
		dates, err := sp.Schedule(len(msg.plan.RemoteCommits), time.Now())
		if err != nil {
			m.pendingSync = nil
			return m.showError(err)
		}
		header += "\n\n  dates of replayed commits:"
		for i, c := range msg.plan.RemoteCommits {
			header += fmt.Sprintf("\n    %s  %s", dates[i].Format("Mon 2006-01-02 15:04"), c.Title)
		}
	}
	if len(msg.plan.Conflicts) > 0 {
		header += "\n\n" + helpStyle.Render("Strategy for files changed on both sides: "+m.pendingSync.Strategy)
	}
	m.state = stateForm
	return m, m.showForm(formModel{cmd: cmdSyncConfirm, form: buildForm(cmdSyncConfirm, m.gitlabGateway, m.height), header: header})
}
