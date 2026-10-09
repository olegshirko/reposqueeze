package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/entity"
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

func repoPicker(key string) *huh.FilePicker {
	return huh.NewFilePicker().
		Key(key).
		Title("Repository path").
		Description("Choose a local Git repository (h/←/backspace: up, enter: select)").
		CurrentDirectory(homeDir()).
		DirAllowed(true).
		FileAllowed(false)
}

func mirrorSelect(repoPath *string, value *string) *huh.Select[string] {
	return huh.NewSelect[string]().
		Key("mirror").
		Title("Mirror").
		Description("Local branch <-> GitLab branch pair (create one with Sync init)").
		OptionsFunc(func() []huh.Option[string] {
			names := getMirrorNames(*repoPath)
			if len(names) == 0 {
				return []huh.Option[string]{huh.NewOption("(no mirrors: run Sync init first)", "")}
			}
			return huh.NewOptions(names...)
		}, repoPath).
		Value(value)
}

// commitTypes offered for local commit titles ("" keeps the remembered/original type).
var commitTypes = []string{"fix", "feat", "test", "refactor", "chore", "docs", "perf", "ci", "build", "style"}

func commitTypeSelect(repoPath *string, value *string) *huh.Select[string] {
	return huh.NewSelect[string]().
		Key("commitType").
		Title("Commit type for local commits").
		OptionsFunc(func() []huh.Option[string] {
			keep := "keep original"
			if f := getRememberedFormat(*repoPath); f.Type != "" {
				keep = "remembered: " + f.Type
			}
			opts := []huh.Option[string]{huh.NewOption(keep, "")}
			for _, t := range commitTypes {
				opts = append(opts, huh.NewOption(t, t))
			}
			return opts
		}, repoPath).
		Value(value)
}

func taskInput(repoPath *string, value *string) *huh.Input {
	return huh.NewInput().
		Key("task").
		Title("Task").
		Description("Appended to local commit titles: \"fix: <title> TASK-1\" (spaces allowed)").
		PlaceholderFunc(func() string {
			if f := getRememberedFormat(*repoPath); f.Task != "" {
				return f.Task + " (remembered)"
			}
			return "TASK-123"
		}, repoPath).
		Validate(func(s string) error {
			return entity.CommitFormat{Task: strings.TrimSpace(s)}.Validate()
		}).
		Value(value)
}

// spreadFields hold the date-spreading answers of a form.
type spreadFields struct {
	enabled         bool
	from, to, hours string
	weekends        bool
}

// spreadToggle asks whether to spread commit dates.
func spreadToggle(sf *spreadFields) *huh.Confirm {
	return huh.NewConfirm().
		Key("spread").
		Title("Spread commit dates evenly over a period?").
		Description("No: keep the GitLab dates. Yes: working days of a period you choose, evenly, with a small random shift.").
		Value(&sf.enabled)
}

// spreadGroup asks for the period; hidden unless spreading is on.
func spreadGroup(sf *spreadFields, hide func() bool) *huh.Group {
	sf.hours = "10-19"
	return huh.NewGroup(
		huh.NewInput().
			Key("spreadFrom").
			Title("Start date").
			Description("First day of the period, YYYY-MM-DD").
			Placeholder(time.Now().AddDate(0, 0, -14).Format("2006-01-02")).
			Validate(func(v string) error {
				_, err := time.Parse("2006-01-02", strings.TrimSpace(v))
				if err != nil {
					return fmt.Errorf("use YYYY-MM-DD")
				}
				return nil
			}).
			Value(&sf.from),
		huh.NewInput().
			Key("spreadTo").
			Title("End date").
			Description("Last day of the period, YYYY-MM-DD (not in the future)").
			Placeholder(time.Now().Format("2006-01-02")).
			Validate(func(v string) error {
				s, err := usecase.ParseDateSpread(sf.from, v, sf.hours, sf.weekends, 0)
				if err != nil {
					return err
				}
				if s.To.After(time.Now()) {
					return fmt.Errorf("the end date is in the future")
				}
				return nil
			}).
			Value(&sf.to),
		huh.NewInput().
			Key("spreadHours").
			Title("Working hours").
			Validate(func(v string) error {
				_, err := usecase.ParseDateSpread("2000-01-03", "2000-01-03", v, false, 0)
				return err
			}).
			Value(&sf.hours),
		huh.NewConfirm().
			Key("spreadWeekends").
			Title("Use weekends too?").
			Value(&sf.weekends),
	).WithHideFunc(hide)
}

func newSyncForm() *huh.Form {
	var repoPath, mirror, strategy, commitType, task string
	var autostash, replay bool
	var sf spreadFields
	strategy = "merge"

	return huh.NewForm(
		huh.NewGroup(
			repoPicker("repoPath").Value(&repoPath),
			mirrorSelect(&repoPath, &mirror),
			huh.NewConfirm().
				Key("replay").
				Title("Pull GitLab commits one by one?").
				Description("Yes: a local commit per GitLab commit (message and date kept, author from your git config). No: one sync commit.").
				Value(&replay),
			huh.NewSelect[string]().
				Key("strategy").
				Title("Files changed on both sides").
				Options(
					huh.NewOption("merge: 3-way merge, conflicts left for manual resolution", "merge"),
					huh.NewOption("local: keep the local version", "local"),
					huh.NewOption("remote: take the GitLab version", "remote"),
					huh.NewOption("abort: stop and show the list", "abort"),
				).
				Value(&strategy),
			huh.NewConfirm().
				Key("autostash").
				Title("Stash uncommitted changes during sync?").
				Value(&autostash),
		),
		huh.NewGroup(
			commitTypeSelect(&repoPath, &commitType),
			taskInput(&repoPath, &task),
		),
		// Dates can only be spread when commits are replayed one by one.
		huh.NewGroup(spreadToggle(&sf)).WithHideFunc(func() bool { return !replay }),
		spreadGroup(&sf, func() bool { return !replay || !sf.enabled }),
	)
}

// listRows is how many rows a long option list gets on a terminal of the given
// height, leaving room for titles, other fields and help.
func listRows(termHeight, reserved int) int {
	if termHeight <= 0 {
		return 12
	}
	return max(termHeight-reserved, 5)
}

func newPullCommitForm(gitlabGW gateway.GitLabGateway, termHeight int) *huh.Form {
	var repoPath, branch, strategy, commitType, task string
	var commits []string
	var sf spreadFields
	strategy = "merge"

	return huh.NewForm(
		huh.NewGroup(
			repoPicker("repoPath").Value(&repoPath),
			huh.NewSelect[string]().
				Key("branchName").
				Title("GitLab branch to take commits from").
				OptionsFunc(func() []huh.Option[string] {
					return huh.NewOptions(getGitLabBranches(gitlabGW, repoPath)...)
				}, &repoPath).
				Value(&branch),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Key("commits").
				Title("Commits to bring in").
				Description("space/x: select • /: filter • [in local] = already brought in • applied oldest first").
				OptionsFunc(func() []huh.Option[string] {
					return getPickableCommits(gitlabGW, repoPath, branch, 100)
				}, &branch).
				Filterable(true).
				Height(listRows(termHeight, 8)).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return fmt.Errorf("select at least one commit")
					}
					return nil
				}).
				Value(&commits),
		),
		huh.NewGroup(
			commitTypeSelect(&repoPath, &commitType),
			taskInput(&repoPath, &task),
			huh.NewSelect[string]().
				Key("strategy").
				Title("If a commit touches files you changed locally").
				Options(
					huh.NewOption("merge: 3-way merge, stop on conflict like git cherry-pick", "merge"),
					huh.NewOption("local: keep the local version", "local"),
					huh.NewOption("remote: take the GitLab version", "remote"),
					huh.NewOption("abort: stop without changing anything", "abort"),
				).
				Value(&strategy),
			spreadToggle(&sf),
		),
		spreadGroup(&sf, func() bool { return !sf.enabled }),
	)
}

func newPickConfirmForm() *huh.Form {
	run := true
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title("Create these local commits?").
				Affirmative("Pick").
				Negative("Cancel").
				Value(&run),
		),
	)
}

func newSyncStatusForm() *huh.Form {
	var repoPath, mirror string
	return huh.NewForm(
		huh.NewGroup(
			repoPicker("repoPath").Value(&repoPath),
			mirrorSelect(&repoPath, &mirror),
		),
	)
}

func newSyncLogForm() *huh.Form {
	var repoPath string
	return huh.NewForm(
		huh.NewGroup(
			repoPicker("repoPath").Value(&repoPath),
		),
	)
}

func newSyncConfirmForm() *huh.Form {
	run := true
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title("Run this sync?").
				Affirmative("Sync").
				Negative("Cancel").
				Value(&run),
		),
	)
}

func newSyncInitForm(gitlabGW gateway.GitLabGateway, termHeight int) *huh.Form {
	// The second page shows two commit lists plus two small fields.
	rows := max((listRows(termHeight, 14))/2, 5)

	var repoPath, localBranch, remoteBranch, localSHA, remoteSHA, name string
	var force bool

	return huh.NewForm(
		huh.NewGroup(
			repoPicker("repoPath").Value(&repoPath),
			huh.NewSelect[string]().
				Key("localBranch").
				Title("Local branch").
				OptionsFunc(func() []huh.Option[string] {
					return huh.NewOptions(getGitBranches(repoPath)...)
				}, &repoPath).
				Value(&localBranch),
			huh.NewSelect[string]().
				Key("remoteBranch").
				Title("GitLab branch").
				OptionsFunc(func() []huh.Option[string] {
					return huh.NewOptions(getGitLabBranches(gitlabGW, repoPath)...)
				}, &repoPath).
				Value(&remoteBranch),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("localSHA").
				Title("Local commit mirroring starts from").
				Description("Its content must match the GitLab commit chosen below").
				OptionsFunc(func() []huh.Option[string] {
					return getGitCommits(repoPath, localBranch, 50)
				}, &localBranch).
				Height(rows).
				Value(&localSHA),
			huh.NewSelect[string]().
				Key("remoteSHA").
				Title("Matching GitLab commit").
				OptionsFunc(func() []huh.Option[string] {
					return getGitLabCommits(gitlabGW, repoPath, remoteBranch, 50)
				}, &remoteBranch).
				Height(rows).
				Value(&remoteSHA),
			huh.NewInput().
				Key("name").
				Title("Mirror name (optional)").
				Placeholder("<local>-><project>:<remote>").
				Value(&name),
			huh.NewConfirm().
				Key("force").
				Title("Overwrite an existing mirror with the same name?").
				Value(&force),
		),
	)
}

// buildForm returns a huh.Form for the given command.
func buildForm(cmd string, gitlabGW gateway.GitLabGateway, termHeight int) *huh.Form {
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
	case cmdSync:
		form = newSyncForm()
	case cmdSyncStatus:
		form = newSyncStatusForm()
	case cmdSyncInit:
		form = newSyncInitForm(gitlabGW, termHeight)
	case cmdSyncLog:
		form = newSyncLogForm()
	case cmdSyncConfirm:
		form = newSyncConfirmForm()
	case cmdPullCommit:
		form = newPullCommitForm(gitlabGW, termHeight)
	case cmdPickConfirm:
		form = newPickConfirmForm()
	}
	if form != nil {
		form.WithKeyMap(formKeyMap())
	}
	return form
}

// formModel wraps a huh.Form so it implements our local Update contract.
type formModel struct {
	cmd    string
	form   *huh.Form
	header string // optional text shown above the form
}

func newFormModel(cmd string, gitlabGW gateway.GitLabGateway, termHeight int) formModel {
	return formModel{cmd: cmd, form: buildForm(cmd, gitlabGW, termHeight)}
}

func (m formModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		// The form is drawn inside a 1x2 margin, below the optional header.
		ws.Width = max(ws.Width-4, 20)
		ws.Height = max(ws.Height-2-m.headerHeight(), 5)
		msg = ws
	}
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}
	return m, cmd
}

func (m formModel) View() string {
	view := m.form.View()
	if m.header != "" {
		view = m.header + "\n\n" + view
	}
	return lipgloss.NewStyle().Margin(1, 2).Render(view)
}

func (m formModel) headerHeight() int {
	if m.header == "" {
		return 0
	}
	return lipgloss.Height(m.header) + 1
}
