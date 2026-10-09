package main

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olegshirko/reposqueeze/internal/app/clipsetup"
	"github.com/olegshirko/reposqueeze/internal/app/controller"
	"github.com/olegshirko/reposqueeze/internal/app/tui"
	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/clipboard"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/gitlab"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// 0. Read configuration
	gitlabToken := os.Getenv("GITLAB_TOKEN")
	gitlabBaseURL := os.Getenv("GITLAB_BASE_URL")
	if gitlabToken == "" && !isHelp(args) && !clipOverSSH(args) {
		fmt.Fprintln(os.Stderr, "GITLAB_TOKEN environment variable is not set.")
		fmt.Fprintln(os.Stderr, "Create a personal access token with the 'api' scope and run: export GITLAB_TOKEN=glpat-...")
		return controller.ExitUsage
	}
	log := logger.NewLoggerWithMasking(gitlabToken)

	// 1. Create instances of the gateway implementations (Frameworks & Drivers)
	gitGateway := git.NewOSExecGitGateway(log)
	gitlabGateway := gitlab.NewHTTPGitLabGateway(gitlabToken, log).WithBaseURL(gitlabBaseURL)

	// 2. TUI mode. Stdout belongs to the UI, so the gateways used for form
	// lookups log nowhere; each operation streams its own log into the UI.
	if len(args) > 0 && args[0] == "tui" {
		quiet := logger.NewLoggerWithWriter(io.Discard)
		m := tui.NewApp(git.NewOSExecGitGateway(quiet),
			gitlab.NewHTTPGitLabGateway(gitlabToken, quiet).WithBaseURL(gitlabBaseURL),
			gitlabToken, gitlabBaseURL, quiet)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Errorf("TUI error: %v", err)
			return controller.ExitError
		}
		return controller.ExitOK
	}

	// 3. Create an instance of the use case, injecting the gateways (Use Cases)
	createBranchUseCase := usecase.NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway, log)
	createOrphanBranchFromGitlabUseCase := usecase.NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)
	pushFilesUseCase := usecase.NewPushFilesUseCase(gitlabGateway, log)
	pullFilesUseCase := usecase.NewPullFilesUseCase(gitGateway, gitlabGateway, log)
	pushFolderUseCase := usecase.NewPushFolderUseCase(gitlabGateway, log)
	cherryPickCommitUseCase := usecase.NewCherryPickCommitUseCase(gitGateway, gitlabGateway, log)
	pushBranchUseCase := usecase.NewPushBranchUseCase(gitGateway, gitlabGateway, log)
	mirrorStore := state.NewFileStore(gitGateway)
	syncUseCase := usecase.NewSyncUseCase(gitGateway, gitlabGateway, mirrorStore, log)
	pullCommitUseCase := usecase.NewPullCommitUseCase(gitGateway, gitlabGateway, mirrorStore, log)

	// 4. Create an instance of the controller, injecting the use case (Interface Adapters)
	cliController := controller.NewCLIController(createBranchUseCase, createOrphanBranchFromGitlabUseCase, pushFilesUseCase, pullFilesUseCase, pushFolderUseCase, cherryPickCommitUseCase, pushBranchUseCase, syncUseCase, pullCommitUseCase, gitlabGateway, log)

	cliController.SetClipDeps(func(ctx context.Context, o clipsetup.Options) (*usecase.ClipUseCase, string, error) {
		o.BaseURL = gitlabBaseURL
		o.Warnf = log.Warnf
		if gitlabToken != "" {
			o.API = gitlabGateway
			o.Token = gitlabToken
			o.CurrentUser = gitlabGateway.CurrentUser
		}
		store, where, err := clipsetup.Store(ctx, o)
		if err != nil {
			return nil, "", err
		}
		return usecase.NewClipUseCase(store, clipboard.Default(), log), where, nil
	})

	// 5. Run the controller with command-line arguments
	return cliController.Run(args)
}

// clipOverSSH reports a clip command using the SSH transport, which needs no token.
func clipOverSSH(args []string) bool {
	if len(args) == 0 || args[0] != "clip" {
		return false
	}
	for i, a := range args {
		if a == "--transport=api" || a == "-transport=api" ||
			((a == "--transport" || a == "-transport") && i+1 < len(args) && args[i+1] == "api") {
			return false
		}
	}
	return true
}

func isHelp(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "help", "-h", "--help":
		return true
	}
	for _, a := range args[1:] {
		if a == "-h" || a == "--help" || a == "-help" {
			return true
		}
	}
	return false
}
