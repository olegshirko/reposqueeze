package gitlab

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// HTTPGitLabGateway is an implementation of the GitLabGateway that uses net/http.
type HTTPGitLabGateway struct {
	Client *http.Client
	Token  string
}

// NewHTTPGitLabGateway creates a new instance of HTTPGitLabGateway.
func NewHTTPGitLabGateway(token string) *HTTPGitLabGateway {
	return &HTTPGitLabGateway{
		Client: http.DefaultClient,
		Token:  token,
	}
}

// commitPayload is the structure for the GitLab Commits API request body.
type commitPayload struct {
	Branch        string                 `json:"branch"`
	CommitMessage string                 `json:"commit_message"`
	Actions       []gateway.CommitAction `json:"actions"`
}

// CommitFilesViaAPI creates a new commit in a GitLab repository with a set of file actions.
func (g *HTTPGitLabGateway) CommitFilesViaAPI(projectID, branchName, commitMessage string, actions []gateway.CommitAction) error {
	// 1. Prepare the API payload
	for i := range actions {
		actions[i].Content = base64.StdEncoding.EncodeToString([]byte(actions[i].Content))
		actions[i].Encoding = "base64"
	}

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
	req.Header.Set("PRIVATE-TOKEN", g.Token)

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

func (g *HTTPGitLabGateway) FindProjectByName(name string) (*entity.Project, error) {
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects?search=%s", url.QueryEscape(name))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create gitlab api request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to gitlab api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab api returned non-200 status for find project: %s, body: %s", resp.Status, string(body))
	}

	var projects []entity.Project
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, fmt.Errorf("failed to decode gitlab projects: %w", err)
	}

	for _, p := range projects {
		if p.Name == name {
			return &p, nil
		}
	}

	return nil, nil // Not found
}

func (g *HTTPGitLabGateway) DeleteProject(projectID int) error {
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s", strconv.Itoa(projectID))

	req, err := http.NewRequest("DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create gitlab api request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to gitlab api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("gitlab api returned non-202 status for delete project: %s, body: %s", resp.Status, string(body))
	}

	return nil
}

type createProjectPayload struct {
	Name string `json:"name"`
}

func (g *HTTPGitLabGateway) CreateProject(name string) (*entity.Project, error) {
	payload := createProjectPayload{
		Name: name,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gitlab create project payload: %w", err)
	}

	apiURL := "https://gitlab.com/api/v4/projects"

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create gitlab api request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to gitlab api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab api returned non-201 status for create project: %s, body: %s", resp.Status, string(body))
	}

	var project entity.Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("failed to decode gitlab project: %w", err)
	}

	return &project, nil
}
