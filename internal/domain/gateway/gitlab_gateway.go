package gateway

import (
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

// GitLabGateway defines the interface for interacting with the GitLab API.
type GitLabGateway interface {
	CommitFilesViaAPI(projectID, branchName, commitMessage string, actions []CommitAction) error
	CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA, token string) error
	FindProjectByName(name string) (*entity.Project, error)
	DeleteProject(projectID int) error
	CreateProject(name string) (*entity.Project, error)
}
