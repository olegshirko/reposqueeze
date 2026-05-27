package gateway

import (
	"bytes"
	"context"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

// CommitAction represents a single file operation for the GitLab Commits API.
type CommitAction struct {
	Action   string `json:"action"` // "create", "delete", "move", "update", "chmod"
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"` // "text" or "base64"
}

// CommitInfo holds basic metadata about a GitLab commit.
type CommitInfo struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// DiffEntry describes a single file change inside a commit diff.
type DiffEntry struct {
	NewPath     string `json:"new_path"`
	OldPath     string `json:"old_path"`
	DeletedFile bool   `json:"deleted_file"`
	RenamedFile bool   `json:"renamed_file"`
	NewFile     bool   `json:"new_file"`
}

// BranchInfo holds basic metadata about a GitLab repository branch.
type BranchInfo struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// GitLabGateway defines the interface for interacting with the GitLab API.
type GitLabGateway interface {
	CommitFilesViaAPI(projectID, branchName, commitMessage string, actions []CommitAction) error
	CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA string) error
	FindProjectByName(name string) (*entity.Project, error)
	DeleteProject(projectID int) error
	CreateProject(name string) (*entity.Project, error)
	DownloadRepoArchive(projectID int, ref string, writer *bytes.Buffer) error
	GetBranches(projectID int) ([]BranchInfo, error)
	GetCommits(projectID int, branchName string, limit int) ([]CommitInfo, error)
	GetCommitDiff(projectID int, sha string) ([]DiffEntry, error)
	GetCompareDiff(projectID int, from, to string) ([]DiffEntry, error)
	GetRawFile(projectID int, filePath, ref string) ([]byte, error)
	FileExists(projectID int, filePath, ref string) (bool, error)
}
