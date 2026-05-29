package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/gitlab"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

const (
	stateMenu       = "menu"
	stateForm       = "form"
	stateFileSelect = "file-select"
	stateRunning    = "running"
	stateResult     = "result"
)

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
	baseLogger    logger.Logger

	// pull-files wizard state
	pendingPullFiles *usecase.PullFilesInput
}

// NewApp creates the TUI root model.
func NewApp(
	gitGateway gateway.GitGateway,
	gitlabGateway gateway.GitLabGateway,
	gitlabToken string,
	baseLogger logger.Logger,
) tea.Model {
	return &appModel{
		state:         stateMenu,
		menu:          newMenuModel(),
		gitGateway:    gitGateway,
		gitlabGateway: gitlabGateway,
		gitlabToken:   gitlabToken,
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
		// propagate to active sub-model
		switch m.state {
		case stateMenu:
			m.menu, _ = m.menu.Update(msg)
		case stateForm, stateFileSelect:
			if m.form != nil {
				*m.form, _ = m.form.Update(msg)
			}
		case stateRunning, stateResult:
			if m.runner != nil {
				*m.runner, _ = m.runner.Update(msg)
			}
		}
		return m, nil

	case tea.KeyMsg:
		// global quit
		if msg.Type == tea.KeyCtrlC {
			if m.runner != nil && m.runner.logCh != nil {
				func() {
					defer func() { recover() }()
					close(m.runner.logCh)
				}()
			}
			return m, tea.Quit
		}
		// q quits from menu or result screens
		if msg.String() == "q" && (m.state == stateMenu || m.state == stateResult) {
			return m, tea.Quit
		}
		// esc goes back from form to menu, unless a file picker is focused
		// (in which case esc navigates up inside the picker).
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
		f := newFormModel(msg.cmd, m.gitlabGateway)
		m.form = &f
		return m, m.form.Init()

	case formSubmittedMsg:
		return m.handleFormSubmit(msg)

	case backMsg:
		switch m.state {
		case stateForm, stateFileSelect:
			m.state = stateMenu
			m.form = nil
			m.pendingPullFiles = nil
			return m, nil
		case stateRunning, stateResult:
			if m.runner != nil && m.runner.logCh != nil {
				func() {
					defer func() { recover() }()
					close(m.runner.logCh)
				}()
			}
			m.state = stateMenu
			m.runner = nil
			m.pendingPullFiles = nil
			return m, nil
		}

	case runResultMsg:
		m.state = stateResult
		if m.runner != nil {
			*m.runner, _ = m.runner.Update(msg)
		}
		return m, nil
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
	}
	return lipgloss.NewStyle().Margin(1, 2).Render("Loading...")
}

func (m *appModel) handleFormSubmit(msg formSubmittedMsg) (tea.Model, tea.Cmd) {
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
		m.form = &f
		return m, m.form.Init()
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

	logCh := make(chan string, 100)
	resultCh := make(chan runResultMsg, 1)

	tuiLog := NewTUILogger(logCh)
	gitGW := git.NewOSExecGitGateway(tuiLog)
	gitlabGW := gitlab.NewHTTPGitLabGateway(m.gitlabToken, tuiLog)

	// Capture the input locally so the goroutine doesn't race with the nil
	// assignment below.
	input := *m.pendingPullFiles
	m.pendingPullFiles = nil

	go func() {
		uc := usecase.NewPullFilesUseCase(gitGW, gitlabGW, tuiLog)
		dur, count, err := uc.Execute(context.Background(), input)
		resultCh <- runResultMsg{duration: dur.String(), count: count, err: err}
		time.Sleep(100 * time.Millisecond)
		tuiLog.Close()
	}()

	r := newRunnerModel(logCh, resultCh)
	r.width = m.width
	r.height = m.height
	r.viewport.Width = m.width - 6
	r.viewport.Height = m.height - 10
	m.runner = &r
	m.state = stateRunning

	return m, m.runner.Init()
}

func (m *appModel) startOperation(msg formSubmittedMsg) (tea.Model, tea.Cmd) {
	logCh := make(chan string, 100)
	resultCh := make(chan runResultMsg, 1)

	tuiLog := NewTUILogger(logCh)
	// rebuild gateways so their logs also appear in the TUI
	gitGW := git.NewOSExecGitGateway(tuiLog)
	gitlabGW := gitlab.NewHTTPGitLabGateway(m.gitlabToken, tuiLog)

	f := msg.form

	go func() {
		var dur time.Duration
		var count int
		var err error

		switch msg.cmd {
		case cmdCreateFromLocal:
			uc := usecase.NewCreateAndPushOrphanBranchUseCase(gitGW, gitlabGW, tuiLog)
			dur, count, err = uc.Execute(context.Background(), usecase.Input{
				RepoPath:     f.GetString("repoPath"),
				BranchName:   f.GetString("branchName"),
				SourceBranch: f.GetString("sourceBranch"),
			})
		case cmdCreateFromGitlab:
			uc := usecase.NewCreateOrphanBranchFromGitlabUseCase(gitGW, gitlabGW, tuiLog)
			dur, count, err = uc.Execute(context.Background(), usecase.CreateOrphanBranchFromGitlabInput{
				RepoPath:   f.GetString("repoPath"),
				BranchName: f.GetString("branchName"),
				Ref:        f.GetString("ref"),
				Commit:     f.GetBool("commit"),
			})
		case cmdPushFiles:
			files := f.Get("files")
			filesStr := ""
			if s, ok := files.([]string); ok {
				filesStr = toCommaSeparated(s)
			}
			uc := usecase.NewPushFilesUseCase(gitlabGW, tuiLog)
			dur, count, err = uc.Execute(context.Background(), usecase.PushFilesInput{
				RepoPath:   f.GetString("repoPath"),
				BranchName: f.GetString("branchName"),
				Files:      filesStr,
			})
		case cmdPushFolder:
			uc := usecase.NewPushFolderUseCase(gitlabGW, tuiLog)
			dur, count, err = uc.Execute(context.Background(), usecase.PushFolderInput{
				FolderPath:  f.GetString("folderPath"),
				ProjectName: f.GetString("projectName"),
				BranchName:  f.GetString("branchName"),
			})
		case cmdCherryPickCommit:
			uc := usecase.NewCherryPickCommitUseCase(gitGW, gitlabGW, tuiLog)
			dur, count, err = uc.Execute(context.Background(), usecase.CherryPickCommitInput{
				RepoPath:      f.GetString("repoPath"),
				CommitHash:    f.GetString("commitHash"),
				BranchName:    f.GetString("branchName"),
				CommitMessage: f.GetString("commitMessage"),
			})
		case cmdPushBranch:
			uc := usecase.NewPushBranchUseCase(gitGW, gitlabGW, tuiLog)
			dur, count, err = uc.Execute(context.Background(), usecase.PushBranchInput{
				RepoPath:      f.GetString("repoPath"),
				SourceBranch:  f.GetString("sourceBranch"),
				BranchName:    f.GetString("branchName"),
				CommitMessage: f.GetString("commitMessage"),
			})
		}

		resultCh <- runResultMsg{duration: dur.String(), count: count, err: err}
		// close log channel after result so any buffered logs are drained
		time.Sleep(100 * time.Millisecond)
		tuiLog.Close()
	}()

	r := newRunnerModel(logCh, resultCh)
	r.width = m.width
	r.height = m.height
	r.viewport.Width = m.width - 6
	r.viewport.Height = m.height - 10
	m.runner = &r
	m.state = stateRunning

	return m, m.runner.Init()
}
