package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

func homeDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return "."
	}
	return home
}

// formKeyMap returns a custom keymap for huh forms.
// We disable the FilePicker "Close" binding so that Esc falls through to the
// underlying bubbles/filepicker and works as "navigate up" (Back).
func formKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.FilePicker.Close = key.NewBinding(key.WithKeys())
	return km
}

// formSubmittedMsg is sent when a huh.Form is completed.
type formSubmittedMsg struct {
	cmd  string
	form *huh.Form
}

func newCreateFromLocalForm() *huh.Form {
	var repoPath, branchName, sourceBranch string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("repoPath").
				Title("Repository path").
				Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&repoPath),

			huh.NewInput().
				Key("branchName").
				Title("Branch name").
				Placeholder("new-branch").
				Value(&branchName),

			huh.NewSelect[string]().
				Key("sourceBranch").
				Title("Source branch").
				Description("Branch to base the orphan on").
				OptionsFunc(func() []huh.Option[string] {
					branches := getGitBranches(repoPath)
					if len(branches) == 0 {
						return []huh.Option[string]{huh.NewOption("master", "master")}
					}
					return huh.NewOptions(branches...)
				}, &repoPath).
				Value(&sourceBranch),
		),
	)
}

func newCreateFromGitlabForm(gitlabGW gateway.GitLabGateway) *huh.Form {
	var repoPath, branchName, ref string
	var commit bool

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("repoPath").
				Title("Repository path").
				Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&repoPath),

			huh.NewInput().
				Key("ref").
				Title("GitLab source (branch, tag or SHA)").
				Description("Leave empty for default branch").
				Placeholder("master").
				Value(&ref),

			huh.NewInput().
				Key("branchName").
				Title("Target branch name").
				Description("Name of the local branch to checkout or create as orphan").
				Placeholder("new-branch").
				Value(&branchName),

			huh.NewConfirm().
				Key("commit").
				Title("Create commit automatically?").
				Description("If yes, files will be committed after unpacking.").
				Value(&commit),
		),
	)
}

func newPushFilesForm(gitlabGW gateway.GitLabGateway) *huh.Form {
	var repoPath, branchName string
	var files []string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("repoPath").
				Title("Repository path").
				Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&repoPath),

			huh.NewSelect[string]().
				Key("branchName").
				Title("Branch name").
				Description("Target branch on GitLab").
				OptionsFunc(func() []huh.Option[string] {
					branches := getGitLabBranches(gitlabGW, repoPath)
					if len(branches) == 0 {
						return []huh.Option[string]{huh.NewOption("master", "master")}
					}
					return huh.NewOptions(branches...)
				}, &repoPath).
				Value(&branchName),

			huh.NewMultiSelect[string]().
				Key("files").
				Title("Files to push").
				Description("Select files from the repository").
				OptionsFunc(func() []huh.Option[string] {
					f := getGitFiles(repoPath)
					if len(f) == 0 {
						return nil
					}
					return huh.NewOptions(f...)
				}, &repoPath).
				Value(&files),
		),
	)
}

func newPullFilesStep1Form(gitlabGW gateway.GitLabGateway) *huh.Form {
	var repoPath, branchName, commits, sinceCommit string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("repoPath").
				Title("Repository path").
				Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&repoPath),

			huh.NewSelect[string]().
				Key("branchName").
				Title("Branch name").
				Description("Source branch on GitLab").
				OptionsFunc(func() []huh.Option[string] {
					branches := getGitLabBranches(gitlabGW, repoPath)
					if len(branches) == 0 {
						return []huh.Option[string]{huh.NewOption("master", "master")}
					}
					return huh.NewOptions(branches...)
				}, &repoPath).
				Value(&branchName),

			huh.NewInput().
				Key("commits").
				Title("Commits to inspect").
				Description("Files will be loaded from these commits on GitLab. Ignored if Since commit is set.").
				Placeholder("1").
				Value(&commits),

			huh.NewInput().
				Key("sinceCommit").
				Title("Since commit (optional)").
				Description("Pull all changes from this commit SHA up to HEAD. Overrides 'Commits to inspect'.").
				Placeholder("abc1234").
				Value(&sinceCommit),
		),
	)
}

func newPullFilesStep2Form(files []string, gitAdd bool) *huh.Form {
	var selected []string
	return huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Key("files").
				Title("Files to pull").
				Description("Select files from the commit diffs").
				Options(huh.NewOptions(files...)...).
				Value(&selected),

			huh.NewConfirm().
				Key("gitAdd").
				Title("Stage downloaded files locally (git add)?").
				Value(&gitAdd),
		),
	)
}

func newPushFolderForm() *huh.Form {
	var folderPath, projectName, branchName string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("folderPath").
				Title("Folder path").
				Description("Choose a local folder to upload (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&folderPath),

			huh.NewInput().
				Key("projectName").
				Title("Project name (optional)").
				Placeholder("defaults to folder base name").
				Value(&projectName),

			huh.NewInput().
				Key("branchName").
				Title("Branch name").
				Placeholder("master").
				Value(&branchName),
		),
	)
}

func newCherryPickCommitForm(gitlabGW gateway.GitLabGateway) *huh.Form {
	var repoPath, commitHash, branchName, commitMessage string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("repoPath").
				Title("Repository path").
				Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&repoPath),

			huh.NewInput().
				Key("commitHash").
				Title("Commit hash").
				Description("Local commit hash to cherry-pick").
				Placeholder("abc1234").
				Value(&commitHash),

			huh.NewSelect[string]().
				Key("branchName").
				Title("Branch name").
				Description("Target branch on GitLab").
				OptionsFunc(func() []huh.Option[string] {
					branches := getGitLabBranches(gitlabGW, repoPath)
					if len(branches) == 0 {
						return []huh.Option[string]{huh.NewOption("master", "master")}
					}
					return huh.NewOptions(branches...)
				}, &repoPath).
				Value(&branchName),

			huh.NewInput().
				Key("commitMessage").
				Title("Commit message (optional)").
				Description("Custom commit message; leave empty to use the original").
				Placeholder("").
				Value(&commitMessage),
		),
	)
}

func newPushBranchForm(gitlabGW gateway.GitLabGateway) *huh.Form {
	var repoPath, sourceBranch, branchName, commitMessage string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Key("repoPath").
				Title("Repository path").
				Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
				CurrentDirectory(homeDir()).
				DirAllowed(true).
				FileAllowed(false).
				Value(&repoPath),

			huh.NewSelect[string]().
				Key("sourceBranch").
				Title("Source branch").
				Description("Local branch to take files from").
				OptionsFunc(func() []huh.Option[string] {
					branches := getGitBranches(repoPath)
					if len(branches) == 0 {
						return []huh.Option[string]{huh.NewOption("master", "master")}
					}
					return huh.NewOptions(branches...)
				}, &repoPath).
				Value(&sourceBranch),

			huh.NewSelect[string]().
				Key("branchName").
				Title("Target branch").
				Description("Target branch on GitLab").
				OptionsFunc(func() []huh.Option[string] {
					branches := getGitLabBranches(gitlabGW, repoPath)
					if len(branches) == 0 {
						return []huh.Option[string]{huh.NewOption("master", "master")}
					}
					return huh.NewOptions(branches...)
				}, &repoPath).
				Value(&branchName),

			huh.NewInput().
				Key("commitMessage").
				Title("Commit message (optional)").
				Description("Custom commit message; leave empty for default").
				Placeholder("").
				Value(&commitMessage),
		),
	)
}

// buildForm returns a huh.Form for the given command.
func buildForm(cmd string, gitlabGW gateway.GitLabGateway) *huh.Form {
	var form *huh.Form
	switch cmd {
	case cmdCreateFromLocal:
		form = newCreateFromLocalForm()
	case cmdCreateFromGitlab:
		form = newCreateFromGitlabForm(gitlabGW)
	case cmdPushFiles:
		form = newPushFilesForm(gitlabGW)
	case cmdPullFiles:
		form = newPullFilesStep1Form(gitlabGW)
	case cmdPushFolder:
		form = newPushFolderForm()
	case cmdCherryPickCommit:
		form = newCherryPickCommitForm(gitlabGW)
	case cmdPushBranch:
		form = newPushBranchForm(gitlabGW)
	}
	if form != nil {
		form.WithKeyMap(formKeyMap())
	}
	return form
}

// formModel wraps a huh.Form so it implements our local Update contract.
type formModel struct {
	cmd  string
	form *huh.Form
}

func newFormModel(cmd string, gitlabGW gateway.GitLabGateway) formModel {
	return formModel{cmd: cmd, form: buildForm(cmd, gitlabGW)}
}

func (m formModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}
	return m, cmd
}

func (m formModel) View() string {
	return m.form.View()
}
