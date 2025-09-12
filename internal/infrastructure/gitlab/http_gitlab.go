package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// HTTPGitLabGateway is an implementation of the GitLabGateway that uses net/http.
type HTTPGitLabGateway struct {
	Client *http.Client
}

// NewHTTPGitLabGateway creates a new instance of HTTPGitLabGateway.
func NewHTTPGitLabGateway() *HTTPGitLabGateway {
	return &HTTPGitLabGateway{
		Client: http.DefaultClient,
	}
}

// commitPayload is the structure for the GitLab Commits API request body.
type commitPayload struct {
	Branch        string                `json:"branch"`
	CommitMessage string                `json:"commit_message"`
	Actions       []gateway.CommitAction `json:"actions"`
}

// CommitFilesViaAPI creates a new commit in a GitLab repository with a set of file actions.
func (g *HTTPGitLabGateway) CommitFilesViaAPI(projectID, branchName, commitMessage, token string, actions []gateway.CommitAction) error {
	// 1. Prepare the API payload
	payload := commitPayload{
		Branch:        branchName,
		CommitMessage: commitMessage,
		Actions:       actions,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal gitlab commit payload: %w", err)
	}

	// 2. Construct the API endpoint URL
	// We need to URL-encode the project ID in case it contains slashes (e.g., "group/project")
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/commits", url.PathEscape(projectID))

	// 3. Create the HTTP request
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create gitlab api request: %w", err)
	}

	// 4. Set necessary headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", token)

	// 5. Send the request
	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to gitlab api: %w", err)
	}
	defer resp.Body.Close()

	// 6. Check the response status code
	if resp.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("gitlab api returned non-201 status: %s, body: %s", resp.Status, string(body))
	}

	return nil
}

type createBranchPayload struct {
	Branch string `json:"branch"`
	Ref    string `json:"ref"`
}

func (g *HTTPGitLabGateway) CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA, token string) error {
	// 1. Prepare the API payload
	payload := createBranchPayload{
		Branch: branchName,
		Ref:    refSHA,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal gitlab create branch payload: %w", err)
	}

	// 2. Construct the API endpoint URL
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/branches", url.PathEscape(projectID))

	// 3. Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create gitlab api request: %w", err)
	}

	// 4. Set necessary headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", token)

	// 5. Send the request
	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to gitlab api: %w", err)
	}
	defer resp.Body.Close()

	// 6. Check the response status code
	if resp.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("gitlab api returned non-201 status for create branch: %s, body: %s", resp.Status, string(body))
	}

	return nil
}