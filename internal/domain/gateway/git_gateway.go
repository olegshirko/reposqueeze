package gateway

import (
	"context"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

// GitGateway defines the interface for interacting with a local Git system.
type GitGateway interface {
	CreateOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) (string, error)
	ListFiles(repoPath string) ([]string, error)
	DeleteLocalBranch(repoPath, branchName string) error
	CheckoutBranch(repoPath, branchName string) error
	RemoveDirectory(repoPath, dirName string) error
	CleanWorkdir(repoPath string) error
	Commit(repoPath, message string) error
}
