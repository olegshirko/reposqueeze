package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// command identifiers
const (
	cmdCreateFromLocal  = "create-from-local"
	cmdCreateFromGitlab = "create-from-gitlab"
	cmdPushFiles        = "push-files"
	cmdPullFiles        = "pull-files"
	cmdPushFolder       = "push-folder"
	cmdCherryPickCommit = "cherry-pick-commit"
	cmdPushBranch       = "push-branch"
)

type menuItem struct {
	title       string
	description string
	cmd         string
}

func (i menuItem) Title() string       { return i.title }
func (i menuItem) Description() string { return i.description }
func (i menuItem) FilterValue() string { return i.title }

// menuModel is the top-level command picker.
type menuModel struct {
	list list.Model
}

func newMenuModel() menuModel {
	items := []list.Item{
		menuItem{title: "Create from local", description: "Create orphan branch from local repo and push to GitLab", cmd: cmdCreateFromLocal},
		menuItem{title: "Create from GitLab", description: "Download GitLab repo archive into local orphan branch", cmd: cmdCreateFromGitlab},
		menuItem{title: "Push files", description: "Commit specific files to an existing GitLab branch", cmd: cmdPushFiles},
		menuItem{title: "Pull files", description: "Download files or commit diffs from GitLab branch", cmd: cmdPullFiles},
		menuItem{title: "Push folder", description: "Upload arbitrary local folder to a new GitLab project", cmd: cmdPushFolder},
		menuItem{title: "Cherry-pick commit", description: "Push a single local commit's file changes to an existing GitLab branch", cmd: cmdCherryPickCommit},
		menuItem{title: "Push branch", description: "Push all file changes from a local branch (diff against base) as one commit", cmd: cmdPushBranch},
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select a command"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle
	l.Styles.HelpStyle = helpStyle
	l.Styles.PaginationStyle = helpStyle

	return menuModel{list: l}
}

func (m menuModel) Init() tea.Cmd {
	return nil
}

func (m menuModel) Update(msg tea.Msg) (menuModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Reserve margins for frame
		m.list.SetSize(msg.Width-4, msg.Height-6)

	case tea.KeyMsg:
		if msg.Type == tea.KeyEnter {
			if selected := m.SelectedCmd(); selected != "" {
				return m, selectedCmdMsg(selected)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m menuModel) View() string {
	return lipgloss.NewStyle().Margin(1, 2).Render(m.list.View())
}

func (m menuModel) SelectedCmd() string {
	if i, ok := m.list.SelectedItem().(menuItem); ok {
		return i.cmd
	}
	return ""
}

func backToMenuMsg() tea.Msg {
	return backMsg{}
}

type backMsg struct{}

func quitMsg() tea.Msg {
	return quitAppMsg{}
}

type quitAppMsg struct{}

func selectedCmdMsg(cmd string) tea.Cmd {
	return func() tea.Msg {
		return cmdSelectedMsg{cmd: cmd}
	}
}

type cmdSelectedMsg struct {
	cmd string
}

// runResultMsg carries the outcome of a use-case execution.
type runResultMsg struct {
	duration string
	count    int
	err      error
}
