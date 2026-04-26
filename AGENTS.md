# reposqueeze — Agent Guide

This document provides essential context for AI coding agents working on the `reposqueeze` project.

## Project Overview

`reposqueeze` is a Go CLI utility that automates the creation of **orphan Git branches** with GitLab integration. It provides the following commands:

1. `create-from-local` — Creates an orphan branch from a local repository, then pushes all files to a GitLab project via the GitLab Commits API. If a GitLab project with the same name already exists, it is deleted and recreated.
2. `create-from-gitlab` — Downloads a repository archive from GitLab, creates a local empty orphan branch, extracts the archive into it, and commits the result locally.
3. `push-files` — Commits specific local files to an existing GitLab project branch.
4. `pull-files` — Downloads files or commit diffs from a GitLab project branch to the local repository.
5. `push-folder` — Uploads an arbitrary local folder (even if it is not a Git repository) to a new GitLab project. If a project with the derived name already exists, it is deleted and recreated.
6. `tui` — Launches an interactive terminal UI (menu, forms, real-time log streaming) powered by Bubble Tea.

> **WARNING:** The `create-from-local` and `push-folder` commands **delete the existing GitLab project** by name before recreating it. All data in the existing project is lost. Use with extreme caution.

- **Module:** `github.com/olegshirko/reposqueeze`
- **Go version:** 1.25.1 (specified in `go.mod`)
- **Repository:** `git@github.com:olegshirko/reposqueeze.git`

## Technology Stack

- **Language:** Go 1.25.1+
- **Logging:** `github.com/sirupsen/logrus v1.9.3`
- **Testing:** `github.com/stretchr/testify` (assert, require)
- **TUI Framework:** `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss`
- **Build tooling:** GNU Make
- **Binary packing:** UPX (optional, via `make pack` / `make pack-linux`)

## Project Structure

```
reposqueeze/
├── cmd/app/main.go                          # Application entry point (manual dependency injection)
├── internal/
│   ├── app/
│   │   ├── controller/cli_controller.go     # CLI flag parsing and command dispatch
│   │   ├── tui/                             # Bubble Tea TUI screens (menu, form, runner)
│   │   ├── usecase/create_branch.go         # create-from-local business logic
│   │   ├── usecase/create_orphan_branch_from_gitlab.go  # create-from-gitlab business logic
│   │   ├── usecase/push_files.go            # push-files business logic
│   │   ├── usecase/pull_files.go            # pull-files business logic
│   │   ├── usecase/push_folder.go           # push-folder business logic
│   │   └── usecase/create_branch_test.go    # Use-case tests (currently broken)
│   ├── domain/
│   │   ├── entity/                          # Core models: Branch, Repository, Project, GitLabProject
│   │   └── gateway/                         # Interface contracts: GitGateway, GitLabGateway
│   ├── infrastructure/
│   │   ├── git/os_exec_git.go               # Git operations via os/exec
│   │   ├── git/os_exec_git_test.go          # Git integration tests (currently broken)
│   │   ├── gitlab/http_gitlab.go            # GitLab API client via net/http
│   │   └── gitlab/http_gitlab_test.go       # GitLab client tests (currently broken)
│   └── pkg/logger/logger.go                 # Logrus-based Logger interface and implementation
├── pkg/config/config.go                     # Empty placeholder file
├── bin/                                     # Pre-built binaries
├── Makefile                                 # Build targets
├── go.mod / go.sum                          # Go module files
└── coverage.out                             # Test coverage report (stale)
```

### Layer Responsibilities

1. **Domain** (`internal/domain/`)
   - `entity/` — Pure data structures (`Branch`, `Repository`, `Project`, `GitLabProject`). Note: `GitLabProject` (with `ID string`) is currently unused; `Project` (with `ID int`) is used by gateways.
   - `gateway/` — Go interfaces defining contracts for Git and GitLab operations.

2. **Application** (`internal/app/`)
   - `controller/` — `CLIController` parses subcommands (`create-from-local`, `create-from-gitlab`) and flags, then invokes the appropriate use case.
   - `usecase/` — Contains the main workflows:
     - `CreateAndPushOrphanBranchUseCase` (`create-from-local`)
     - `CreateOrphanBranchFromGitlabUseCase` (`create-from-gitlab`)
     - `PushFilesUseCase` (`push-files`)
     - `PullFilesUseCase` (`pull-files`)
     - `PushFolderUseCase` (`push-folder`)

3. **Infrastructure** (`internal/infrastructure/`)
   - `git/OSExecGitGateway` — Implements `GitGateway` using local `git` commands via `os/exec`.
   - `gitlab/HTTPGitLabGateway` — Implements `GitLabGateway` using GitLab REST API v4 (`https://gitlab.com/api/v4`).

## Build Commands

All build tasks are managed via `Makefile`:

| Command | Description |
|---------|-------------|
| `make build` | Compile for the current OS → `./bin/reposqueeze` |
| `make build-linux` | Cross-compile for Linux amd64 → `./bin/reposqueeze-linux` |
| `make pack` | Build + compress with UPX |
| `make pack-linux` | Build for Linux + compress with UPX |
| `make help` | Show available targets |

Direct Go build:
```bash
go build -o ./bin/reposqueeze ./cmd/app/main.go
```

## Testing

> **Current Status:** The test suite does **not compile** as of the latest checkout. Multiple issues exist:
> - `internal/app/usecase/create_branch_test.go` — Mock signatures are out of sync with interfaces (e.g., `MockGitGateway.CreateOrphanBranch` returns `(string, error)` but the real interface returns `error`). `NewCreateAndPushOrphanBranchUseCase` is called without the required `logger.Logger` argument.
> - `internal/infrastructure/git/os_exec_git_test.go` — `NewOSExecGitGateway` is called without the required `logger.Logger` argument. `CreateOrphanBranch` result assignment expects two return values instead of one.
> - `internal/infrastructure/gitlab/http_gitlab_test.go` — The local variable `gateway` shadows the imported `gateway` package, causing "imported and not used" and "gateway.CommitAction is not a type" errors.
> - `pkg/config/config.go` is an empty file (0 bytes), which causes a package parsing error.

To run tests after fixing the above:
```bash
go test ./...
```

Generate coverage:
```bash
go test -coverprofile=coverage.out ./...
```

### Testing Strategy (Intended)

- **Use-case layer** — Mock-based unit tests using `testify/mock` to mock `GitGateway` and `GitLabGateway`. Covers success paths and error paths.
- **Git infrastructure** — Integration-style tests that create real temporary Git repositories using `os/exec` and `t.TempDir()`.
- **GitLab infrastructure** — Uses `net/http/httptest` to spin up local HTTP servers that simulate GitLab API responses.

## Runtime Usage

The application requires a GitLab personal access token with `api` scope. It must be provided via the `GITLAB_TOKEN` environment variable; the application panics on startup if the variable is unset.

```bash
export GITLAB_TOKEN="glpat-xxxxxxxxxxxxxxxxxxxx"

# create-from-local
./bin/reposqueeze create-from-local /path/to/repo --branch-name new-branch --from main

# create-from-gitlab
./bin/reposqueeze create-from-gitlab /path/to/repo --branch-name new-branch

# push-folder
./bin/reposqueeze push-folder /path/to/folder --project-name my-project --branch-name master

# tui (interactive mode)
./bin/reposqueeze tui
```

Flags for `create-from-local`:
- `<path>` — Path to the local Git repository (positional, required)
- `--branch-name` — Name of the new orphan branch (required)
- `--from` — Source branch to base the orphan on (default: `master`)

Flags for `create-from-gitlab`:
- `<path>` — Path to the local Git repository (positional, required)
- `--branch-name` — Name of the new orphan branch (required)

Flags for `push-folder`:
- `<path>` — Path to the local folder to upload (positional, required)
- `--project-name` — GitLab project name (optional; defaults to the folder base name)
- `--branch-name` — Target branch on GitLab (default: `master`)

## Code Style Guidelines

- Follow standard Go conventions (`gofmt`, `go vet`).
- Keep the domain layer free of external dependencies.
- All cross-cutting concerns (HTTP, OS exec) live in `internal/infrastructure/`.
- Interfaces are defined in `internal/domain/gateway/` and implemented in infrastructure packages.
- Errors are logged via the injected `logger.Logger`; some are also returned to the caller.
- Use dependency injection in `main.go`; infrastructure packages should not be imported directly by use-case tests.
- Some comments in the codebase are in Russian; new code and documentation should be in English for consistency with the existing `AGENTS.md` and `README.md` structure.

## Security Considerations

- **Destructive operation:** `create-from-local` and `push-folder` find and **delete** the GitLab project by name before recreating it. Any data in the existing project is lost.
- The token is read exclusively from the `GITLAB_TOKEN` environment variable (the `--token` flag mentioned in older docs is not implemented in the current controller).
- File contents are base64-encoded before transmission to the GitLab Commits API.
- The GitLab API base URL is hardcoded to `https://gitlab.com/api/v4`.

## Known Issues & Technical Debt

- `pkg/config/config.go` is an empty placeholder and breaks builds/tests.
- `GitLabProject` entity (`internal/domain/entity/gitlab.go`) is defined but unused; `Project` is used instead.
- `CreateRemoteBranch` in `GitLabGateway` is implemented but never called by any use case (commented-out code exists in `create_branch.go`).
- `go.sum` was previously missing entries for `stretchr/testify`; run `go mod tidy` to resolve.
