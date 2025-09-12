package gateway

import "context"

// CommitAction represents a single file operation for the GitLab Commits API.
type CommitAction struct {
	Action   string `json:"action"` // "create", "delete", "move", "update", "chmod"
	FilePath string `json:"file_path"`
	Content  string `json:"content"` // Base64 encoded content
}

// GitLabGateway defines the interface for interacting with the GitLab API.
type GitLabGateway interface {
	CommitFilesViaAPI(projectID, branchName, commitMessage, token string, actions []CommitAction) error
	CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA, token string) error
}