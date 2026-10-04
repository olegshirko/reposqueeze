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

// MergeResult is the outcome of a three-way merge of a single file.
type MergeResult struct {
	Content   []byte
	Conflicts bool
}

// SyncGit is the set of local Git operations needed by two-way sync.
type SyncGit interface {
	RevParse(repoPath, ref string) (string, error)
	CurrentBranch(repoPath string) (string, error)
	IsClean(repoPath string) (bool, error)
	GitDir(repoPath string) (string, error)
	// DiffFiles lists changes between two refs without rename detection
	// (renames show up as D + A).
	DiffFiles(repoPath, from, to string) ([]CommitFileInfo, error)
	// FileAtRef returns a file's content at ref; found is false when the file does not exist there.
	FileAtRef(repoPath, ref, path string) (content []byte, found bool, err error)
	MergeFile(ours, base, theirs []byte, labels [3]string) (MergeResult, error)
	// CommitPaths stages exactly the given paths (including deletions) and commits.
	CommitPaths(repoPath, message string, paths []string, allowEmpty bool) (string, error)
	// RestorePaths returns the given paths to their HEAD state, removing files HEAD does not have.
	RestorePaths(repoPath string, paths []string) error
	StashPush(repoPath, message string) (stashed bool, err error)
	StashPop(repoPath string) error
	LogCommits(repoPath, ref string, limit int) ([]CommitInfo, error)
	// FindLastTrailer returns the newest commit reachable from ref that has the trailer key.
	FindLastTrailer(repoPath, ref, key string) (sha, value string, err error)
	IsAncestor(repoPath, ancestor, descendant string) (bool, error)
}

// MirrorStore persists sync mirrors of a local repository.
type MirrorStore interface {
	Load(repoPath string) (*entity.MirrorSet, error)
	Save(repoPath string, set *entity.MirrorSet) error
}
