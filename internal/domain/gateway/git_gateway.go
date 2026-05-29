package gateway

import (
	"context"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

// CommitFileInfo represents a file change in a specific commit.
type CommitFileInfo struct {
	Status  string // "A", "M", "D", "R100", etc.
	Path    string
	OldPath string // filled for renames
}

// GitGateway defines the interface for interacting with a local Git system.
type GitGateway interface {
	CreateOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) error
	CreateEmptyOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) error
	ListFiles(repoPath string) ([]string, error)
	DeleteLocalBranch(repoPath, branchName string) error
	CheckoutBranch(repoPath, branchName string) error
	RemoveDirectory(repoPath, dirName string) error
	CleanWorkdir(repoPath string) error
	Commit(repoPath, message string) error
	AddAll(repoPath string) error
	BranchExists(repoPath, branchName string) (bool, error)
	GetCommitMessage(repoPath, commitHash string) (string, error)
	GetCommitFiles(repoPath, commitHash string) ([]CommitFileInfo, error)
	GetFileContentFromCommit(repoPath, commitHash, filePath string) ([]byte, error)
	GetBranchDiffFiles(repoPath, baseBranch, sourceBranch string) ([]CommitFileInfo, error)
	ListFilesInBranch(repoPath, branchName string) ([]string, error)
	GetMergeBase(repoPath, branch1, branch2 string) (string, error)
}
