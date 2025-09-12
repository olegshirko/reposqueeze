package gateway

import (
	"context"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

// GitGateway defines the interface for interacting with a local Git system.
type GitGateway interface {
	CreateOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch) (string, error)
	ListFiles(repoPath string) ([]string, error)
}
