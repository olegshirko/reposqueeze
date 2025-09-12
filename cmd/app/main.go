package main

import (
	"os"
	"reposqueeze/internal/app/controller"
	"reposqueeze/internal/app/usecase"
	"reposqueeze/internal/infrastructure/git"
	"reposqueeze/internal/infrastructure/gitlab"
)

func main() {
	// 1. Create instances of the gateway implementations (Frameworks & Drivers)
	gitGateway := git.NewOSExecGitGateway()
	gitlabGateway := gitlab.NewHTTPGitLabGateway()

	// 2. Create an instance of the use case, injecting the gateways (Use Cases)
	createBranchUseCase := usecase.NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway)

	// 3. Create an instance of the controller, injecting the use case (Interface Adapters)
	cliController := controller.NewCLIController(createBranchUseCase)

	// 4. Run the controller with command-line arguments
	// os.Args[1:] excludes the program name
	cliController.Run(os.Args[1:])
}