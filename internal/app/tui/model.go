package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/gitlab"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

const (
	stateMenu       = "menu"
	stateForm       = "form"
	stateFileSelect = "file-select"
	stateRunning    = "running"
	stateResult     = "result"
	statePlanning   = "planning"
)

// syncPlanMsg delivers the plan computed for the sync wizard.
type syncPlanMsg struct {
	plan *usecase.SyncPlan
	err  error
}

// appModel is the root Bubble Tea model that orchestrates screens.
type appModel struct {
	state  string
	menu   menuModel
	form   *formModel
	runner *runnerModel
	width  int
	height int

	// infrastructure references needed to rebuild use-cases with a live logger
	gitGateway    gateway.GitGateway
	gitlabGateway gateway.GitLabGateway
	gitlabToken   string
	gitlabBaseURL string
	baseLogger    logger.Logger

	// pull-files wizard state
	pendingPullFiles *usecase.PullFilesInput

	// sync wizard state
	pendingSync *usecase.SyncInput

	// runSeq numbers runs so events from abandoned runs are ignored.
	runSeq int
}

// NewApp creates the TUI root model.
func NewApp(
	gitGateway gateway.GitGateway,
	gitlabGateway gateway.GitLabGateway,
	gitlabToken string,
	gitlabBaseURL string,
	baseLogger logger.Logger,
) tea.Model {
	return &appModel{
		state:         stateMenu,
		menu:          newMenuModel(),
		gitGateway:    gitGateway,
		gitlabGateway: gitlabGateway,
		gitlabToken:   gitlabToken,
		gitlabBaseURL: gitlabBaseURL,
		baseLogger:    baseLogger,
	}
}

func (m *appModel) Init() tea.Cmd {
	return m.menu.Init()
}

func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Every screen learns the new size, so going back to one later
		// does not show it at a stale size.
		m.menu, _ = m.menu.Update(msg)
		if m.form != nil {
			*m.form, _ = m.form.Update(msg)
		}
		if m.runner != nil {
			*m.runner, _ = m.runner.Update(msg)
		}
		return m, nil

	case tea.KeyMsg:
		// global quit
		if msg.Type == tea.KeyCtrlC {
			if m.runner != nil {
				m.runner.stop()
			}
			return m, tea.Quit
		}
		// q quits from menu or result screens
		if msg.String() == "q" && (m.state == stateMenu || m.state == stateResult) {
			return m, tea.Quit
		}
		// esc goes back from form to menu, unless a file picker is focused
		// (in which case esc navigates up inside the picker).
		if msg.Type == tea.KeyEsc && m.state == statePlanning {
			return m, func() tea.Msg { return backMsg{} }
		}
		if msg.Type == tea.KeyEsc && (m.state == stateForm || m.state == stateFileSelect) {
			if m.form != nil {
				if _, ok := m.form.form.GetFocusedField().(*huh.FilePicker); ok {
					// Let the file picker handle esc as "navigate up".
					break
				}
			}
			return m, func() tea.Msg { return backMsg{} }
		}

	case cmdSelectedMsg:
		m.state = stateForm
		return m, m.showForm(newFormModel(msg.cmd, m.gitlabGateway, m.height))

	case formSubmittedMsg:
		return m.handleFormSubmit(msg)

	case syncPlanMsg:
		return m.handleSyncPlan(msg)

	case backMsg:
		switch m.state {
		case stateForm, stateFileSelect, statePlanning:
			m.state = stateMenu
			m.form = nil
			m.pendingPullFiles = nil
			m.pendingSync = nil
			return m, nil
		case stateRunning, stateResult:
			if m.runner != nil {
				m.runner.stop()
			}
			m.state = stateMenu
			m.runner = nil
			m.pendingPullFiles = nil
			return m, nil
		}

	case runEventMsg:
		// Events from a run the user already left are dropped.
		if m.runner == nil || msg.runID != m.runner.runID {
			return m, nil
		}
		var cmd tea.Cmd
		*m.runner, cmd = m.runner.Update(msg)
		if m.runner.done {
			m.state = stateResult
		}
		return m, cmd
	}

	// delegate to sub-model
	switch m.state {
	case stateMenu:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		return m, cmd

	case stateForm, stateFileSelect:
		if m.form != nil {
			var cmd tea.Cmd
			*m.form, cmd = m.form.Update(msg)
			// If the huh.Form is completed, collect values and move on.
			if m.form.form.State == huh.StateCompleted {
				return m, func() tea.Msg {
					return formSubmittedMsg{cmd: m.form.cmd, form: m.form.form}
				}
			}
			return m, cmd
		}

	case stateRunning, stateResult:
		if m.runner != nil {
			var cmd tea.Cmd
			*m.runner, cmd = m.runner.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// showForm makes f the current form, sized to the terminal (a form created
// after startup never receives the initial WindowSizeMsg by itself).
func (m *appModel) showForm(f formModel) tea.Cmd {
	m.form = &f
	cmd := m.form.Init()
	if m.width > 0 {
		*m.form, _ = m.form.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	}
	return cmd
}

func (m *appModel) View() string {
	switch m.state {
	case stateMenu:
		return m.menu.View()
	case stateForm, stateFileSelect:
		if m.form != nil {
			return m.form.View()
		}
	case stateRunning, stateResult:
		if m.runner != nil {
			return m.runner.View()
		}
	case statePlanning:
		return lipgloss.NewStyle().Margin(1, 2).Render("Comparing local branch with GitLab...\n\n" + helpStyle.Render("esc: cancel"))
	}
	return lipgloss.NewStyle().Margin(1, 2).Render("Loading...")
}

func (m *appModel) handleFormSubmit(msg formSubmittedMsg) (tea.Model, tea.Cmd) {
	switch msg.cmd {
	case cmdSync, cmdSyncStatus, cmdSyncInit, cmdSyncLog, cmdSyncConfirm, cmdPullCommit:
		return m.handleSyncForm(msg)
	}

	// Special wizard for pull-files when no explicit file list is given.
	if msg.cmd == cmdPullFiles && m.state == stateForm {
		repoPath := msg.form.GetString("repoPath")
		branchName := msg.form.GetString("branchName")
		commits := safeAtoi(msg.form.GetString("commits"), 1)
		sinceCommit := msg.form.GetString("sinceCommit")

		// If since-commit is set, skip file-selection and go straight to running.
		if sinceCommit != "" {
			m.pendingPullFiles = &usecase.PullFilesInput{
				RepoPath:    repoPath,
				BranchName:  branchName,
				SinceCommit: sinceCommit,
				GitAdd:      false,
			}
			return m.startPullFilesOperation(msg.form)
		}

		// Always go to the file-selection step so the user can pick files
		// from the commit diffs on GitLab.
		files, err := getFilesFromGitLabCommits(m.gitlabGateway, repoPath, branchName, commits)
		if err != nil {
			// If we can't fetch the file list (e.g. project not found),
			// fall back to starting the operation with an empty file list
			// so the use-case can report the error properly.
			m.pendingPullFiles = &usecase.PullFilesInput{
				RepoPath:   repoPath,
				BranchName: branchName,
				Files:      "",
				Commits:    commits,
				GitAdd:     false,
			}
			return m.startPullFilesOperation(msg.form)
		}

		m.pendingPullFiles = &usecase.PullFilesInput{
			RepoPath:   repoPath,
			BranchName: branchName,
			Commits:    commits,
			GitAdd:     false,
		}

		m.state = stateFileSelect
		f := formModel{
			cmd:  cmdPullFiles,
			form: newPullFilesStep2Form(files, false),
		}
		return m, m.showForm(f)
	}

	if msg.cmd == cmdPullFiles && m.state == stateFileSelect {
		return m.startPullFilesOperation(msg.form)
	}

	return m.startOperation(msg)
}

func (m *appModel) startPullFilesOperation(f *huh.Form) (tea.Model, tea.Cmd) {
	if m.pendingPullFiles == nil {
		return m.startOperation(formSubmittedMsg{cmd: cmdPullFiles, form: f})
	}

	files := f.Get("files")
	if s, ok := files.([]string); ok && len(s) > 0 {
		m.pendingPullFiles.Files = toCommaSeparated(s)
	}
	m.pendingPullFiles.GitAdd = f.GetBool("gitAdd")

	// Capture the input locally so the run doesn't race with the nil
	// assignment below.
	input := *m.pendingPullFiles
	m.pendingPullFiles = nil

	return m.startRun(func(ctx context.Context, d deps) (time.Duration, int, error) {
		return usecase.NewPullFilesUseCase(d.git, d.gitlab, d.log).Execute(ctx, input)
	})
}

// deps are gateways bound to the TUI logger of a single run.
type deps struct {
	log     *TUILogger
	git     gateway.GitGateway
	syncGit gateway.SyncGit
	gitlab  gateway.GitLabGateway
}

func (d deps) syncUseCase() *usecase.SyncUseCase {
	return usecase.NewSyncUseCase(d.syncGit, d.gitlab, state.NewFileStore(d.syncGit), d.log)
}

// runFunc executes one operation and reports duration and processed file count.
type runFunc func(ctx context.Context, d deps) (time.Duration, int, error)

// startRun launches fn in the background and switches to the runner screen.
func (m *appModel) startRun(fn runFunc) (tea.Model, tea.Cmd) {
	return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
		dur, count, err := fn(ctx, d)
		return runResultMsg{duration: dur.String(), count: count, err: err}
	})
}

// startRunWithSummary is like startRun but lets fn build the whole result.
func (m *appModel) startRunWithSummary(fn func(ctx context.Context, d deps) runResultMsg) (tea.Model, tea.Cmd) {
	tuiLog := NewTUILogger()
	// rebuild gateways so their logs also appear in the TUI
	gitGW := git.NewOSExecGitGateway(tuiLog)
	d := deps{
		log:     tuiLog,
		git:     gitGW,
		syncGit: gitGW,
		gitlab:  gitlab.NewHTTPGitLabGateway(m.gitlabToken, tuiLog).WithBaseURL(m.gitlabBaseURL),
	}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		tuiLog.Finish(fn(ctx, d))
	}()

	m.runSeq++
	r := newRunnerModel(m.runSeq, tuiLog, cancel, m.width, m.height)
	m.runner = &r
	m.state = stateRunning
	return m, m.runner.Init()
}

func (m *appModel) startOperation(msg formSubmittedMsg) (tea.Model, tea.Cmd) {
	f := msg.form

	return m.startRun(func(ctx context.Context, d deps) (time.Duration, int, error) {
		gitGW, gitlabGW, tuiLog := d.git, d.gitlab, d.log

		switch msg.cmd {
		case cmdCreateFromLocal:
			uc := usecase.NewCreateAndPushOrphanBranchUseCase(gitGW, gitlabGW, tuiLog)
			return uc.Execute(ctx, usecase.Input{
				RepoPath:     f.GetString("repoPath"),
				BranchName:   f.GetString("branchName"),
				SourceBranch: f.GetString("sourceBranch"),
			})
		case cmdCreateFromGitlab:
			uc := usecase.NewCreateOrphanBranchFromGitlabUseCase(gitGW, gitlabGW, tuiLog)
			return uc.Execute(ctx, usecase.CreateOrphanBranchFromGitlabInput{
				RepoPath:   f.GetString("repoPath"),
				BranchName: f.GetString("branchName"),
				Ref:        f.GetString("ref"),
				Commit:     f.GetBool("commit"),
			})
		case cmdPushFiles:
			filesStr := ""
			if s, ok := f.Get("files").([]string); ok {
				filesStr = toCommaSeparated(s)
			}
			uc := usecase.NewPushFilesUseCase(gitlabGW, tuiLog)
			return uc.Execute(ctx, usecase.PushFilesInput{
				RepoPath:   f.GetString("repoPath"),
				BranchName: f.GetString("branchName"),
				Files:      filesStr,
			})
		case cmdPushFolder:
			uc := usecase.NewPushFolderUseCase(gitlabGW, tuiLog)
			return uc.Execute(ctx, usecase.PushFolderInput{
				FolderPath:  f.GetString("folderPath"),
				ProjectName: f.GetString("projectName"),
				BranchName:  f.GetString("branchName"),
			})
		case cmdCherryPickCommit:
			uc := usecase.NewCherryPickCommitUseCase(gitGW, gitlabGW, tuiLog)
			return uc.Execute(ctx, usecase.CherryPickCommitInput{
				RepoPath:      f.GetString("repoPath"),
				CommitHash:    f.GetString("commitHash"),
				BranchName:    f.GetString("branchName"),
				CommitMessage: f.GetString("commitMessage"),
			})
		case cmdPushBranch:
			uc := usecase.NewPushBranchUseCase(gitGW, gitlabGW, tuiLog)
			return uc.Execute(ctx, usecase.PushBranchInput{
				RepoPath:      f.GetString("repoPath"),
				SourceBranch:  f.GetString("sourceBranch"),
				BranchName:    f.GetString("branchName"),
				CommitMessage: f.GetString("commitMessage"),
			})
		}
		return 0, 0, fmt.Errorf("unknown command %q", msg.cmd)
	})
}
