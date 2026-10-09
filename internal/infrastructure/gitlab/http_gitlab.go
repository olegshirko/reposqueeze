package gitlab

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// HTTPGitLabGateway is an implementation of the GitLabGateway that uses net/http.
type HTTPGitLabGateway struct {
	Client  *http.Client
	Token   string
	BaseURL string
	logger  logger.Logger
}

// defaultHTTPTimeout bounds every GitLab API call, including archive downloads.
const defaultHTTPTimeout = 5 * time.Minute

// NewHTTPGitLabGateway creates a new instance of HTTPGitLabGateway.
func NewHTTPGitLabGateway(token string, log logger.Logger) *HTTPGitLabGateway {
	return &HTTPGitLabGateway{
		Client: &http.Client{Timeout: defaultHTTPTimeout},
		Token:  token,
		logger: log,
	}
}

// WithBaseURL sets the GitLab instance URL, e.g. "https://gitlab.example.com".
// The "/api/v4" suffix is appended when missing.
func (g *HTTPGitLabGateway) WithBaseURL(base string) *HTTPGitLabGateway {
	g.BaseURL = NormalizeBaseURL(base)
	return g
}

// NormalizeBaseURL turns a GitLab instance URL into its API v4 root.
// An empty string stays empty so the gitlab.com default applies.
func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || strings.HasSuffix(base, "/api/v4") {
		return base
	}
	return base + "/api/v4"
}

func (g *HTTPGitLabGateway) baseURL() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return "https://gitlab.com/api/v4"
}

// commitPayload is the structure for the GitLab Commits API request body.
type commitPayload struct {
	Branch        string                 `json:"branch"`
	CommitMessage string                 `json:"commit_message"`
	Actions       []gateway.CommitAction `json:"actions"`
}

// CommitFilesViaAPI creates a new commit in a GitLab repository with a set of file actions
// and returns the SHA of the created commit.
func (g *HTTPGitLabGateway) CommitFilesViaAPI(projectID, branchName, commitMessage string, actions []gateway.CommitAction) (gateway.CommitInfo, error) {
	// 1. Prepare the API payload on a copy so the caller's actions stay untouched.
	encoded := make([]gateway.CommitAction, len(actions))
	for i, a := range actions {
		a.Content = base64.StdEncoding.EncodeToString([]byte(a.Content))
		a.Encoding = "base64"
		encoded[i] = a
	}

	payload := commitPayload{
		Branch:        branchName,
		CommitMessage: commitMessage,
		Actions:       encoded,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		g.logger.Errorf("failed to marshal gitlab commit payload: %v", err)
		return gateway.CommitInfo{}, err
	}

	// 2. Construct the API endpoint URL
	// We need to URL-encode the project ID in case it contains slashes (e.g., "group/project")
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%s/repository/commits", baseURL, url.PathEscape(projectID))

	// 3. Create the HTTP request
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return gateway.CommitInfo{}, err
	}

	// 4. Set necessary headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", g.Token)

	// 5. Send the request
	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return gateway.CommitInfo{}, err
	}
	defer resp.Body.Close()

	// 6. Check the response status code
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-201 status: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return gateway.CommitInfo{}, err
	}

	var created gateway.CommitInfo
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return gateway.CommitInfo{}, fmt.Errorf("failed to decode created commit: %w", err)
	}

	return created, nil
}

type createBranchPayload struct {
	Branch string `json:"branch"`
	Ref    string `json:"ref"`
}

func (g *HTTPGitLabGateway) CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA string) error {
	// 1. Prepare the API payload
	payload := createBranchPayload{
		Branch: branchName,
		Ref:    refSHA,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		g.logger.Errorf("failed to marshal gitlab create branch payload: %v", err)
		return err
	}

	// 2. Construct the API endpoint URL
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%s/repository/branches", baseURL, url.PathEscape(projectID))

	// 3. Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return err
	}

	// 4. Set necessary headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", g.Token) // Используем токен из структуры

	// 5. Send the request
	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return err
	}
	defer resp.Body.Close()

	// 6. Check the response status code
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-201 status for create branch: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return err
	}

	return nil
}

func (g *HTTPGitLabGateway) FindProjectByName(projectName string) (*entity.Project, error) {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects?owned=true&per_page=100&search=%s", baseURL, url.QueryEscape(projectName))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return nil, err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-200 status for find project: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return nil, err
	}

	var projects []entity.Project
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		g.logger.Errorf("failed to decode gitlab projects: %v", err)
		return nil, err
	}

	var matchingProjects []entity.Project
	for _, p := range projects {
		if p.Name == projectName {
			matchingProjects = append(matchingProjects, p)
		}
	}

	if len(matchingProjects) == 1 {
		return &matchingProjects[0], nil
	}

	if len(matchingProjects) > 1 {
		err := fmt.Errorf("found multiple projects with name %s, please specify the full path", projectName)
		g.logger.Error(err)
		return nil, err
	}

	return nil, nil // Not found
}

func (g *HTTPGitLabGateway) DeleteProject(projectID int) error {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%s", baseURL, strconv.Itoa(projectID))

	req, err := http.NewRequest("DELETE", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	// Log request details
	g.logger.Infof("Deleting project. Request URL: %s", apiURL)
	g.logger.Info("Request Headers:")
	for name, values := range req.Header {
		if name != "PRIVATE-TOKEN" {
			for _, value := range values {
				g.logger.Infof("  %s: %s", name, value)
			}
		}
	}

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return err
	}
	defer resp.Body.Close()

	// Log response details
	body, _ := io.ReadAll(resp.Body)
	g.logger.Infof("GitLab API Response Status: %s", resp.Status)
	g.logger.Infof("GitLab API Response Body: %s", string(body))

	if resp.StatusCode != http.StatusAccepted {
		err := fmt.Errorf("gitlab api returned non-202 status for delete project: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return err
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
		g.logger.Errorf("failed to marshal gitlab create project payload: %v", err)
		return nil, err
	}

	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects", baseURL)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-201 status for create project: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return nil, err
	}

	var project entity.Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		g.logger.Errorf("failed to decode gitlab project: %v", err)
		return nil, err
	}

	return &project, nil
}

func (g *HTTPGitLabGateway) DownloadRepoArchive(projectID int, ref string, writer *bytes.Buffer) error {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%d/repository/archive.zip", baseURL, projectID)
	if ref != "" {
		apiURL += "?sha=" + url.QueryEscape(ref)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-200 status for download archive: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return err
	}

	_, err = writer.ReadFrom(resp.Body)
	if err != nil {
		g.logger.Errorf("failed to read response body: %v", err)
		return err
	}

	return nil
}

func (g *HTTPGitLabGateway) GetBranches(projectID int) ([]gateway.BranchInfo, error) {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%d/repository/branches?per_page=100", baseURL, projectID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return nil, err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-200 status for list branches: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return nil, err
	}

	var branches []gateway.BranchInfo
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		g.logger.Errorf("failed to decode branches: %v", err)
		return nil, err
	}

	return branches, nil
}

func (g *HTTPGitLabGateway) GetCommits(projectID int, branchName string, limit int) ([]gateway.CommitInfo, error) {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%d/repository/commits?ref_name=%s&per_page=%d",
		baseURL, projectID, url.QueryEscape(branchName), limit)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return nil, err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-200 status for list commits: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return nil, err
	}

	var commits []gateway.CommitInfo
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		g.logger.Errorf("failed to decode commits: %v", err)
		return nil, err
	}

	return commits, nil
}

func (g *HTTPGitLabGateway) GetCommitDiff(projectID int, sha string) ([]gateway.DiffEntry, error) {
	baseURL := g.baseURL()
	var allDiffs []gateway.DiffEntry
	page := 1

	for {
		apiURL := fmt.Sprintf("%s/projects/%d/repository/commits/%s/diff?per_page=100&page=%d",
			baseURL, projectID, url.PathEscape(sha), page)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			g.logger.Errorf("failed to create gitlab api request: %v", err)
			return nil, err
		}

		req.Header.Set("PRIVATE-TOKEN", g.Token)

		resp, err := g.Client.Do(req)
		if err != nil {
			g.logger.Errorf("failed to send request to gitlab api: %v", err)
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			err := fmt.Errorf("gitlab api returned non-200 status for commit diff: %s, body: %s", resp.Status, string(body))
			g.logger.Error(err)
			return nil, err
		}

		var diffs []gateway.DiffEntry
		if err := json.NewDecoder(resp.Body).Decode(&diffs); err != nil {
			resp.Body.Close()
			g.logger.Errorf("failed to decode commit diff: %v", err)
			return nil, err
		}
		resp.Body.Close()

		allDiffs = append(allDiffs, diffs...)

		if len(diffs) < 100 {
			break
		}
		page++
	}

	return allDiffs, nil
}

func (g *HTTPGitLabGateway) GetCompareDiff(projectID int, from, to string) ([]gateway.DiffEntry, error) {
	baseURL := g.baseURL()
	var allDiffs []gateway.DiffEntry
	page := 1

	for {
		apiURL := fmt.Sprintf("%s/projects/%d/repository/compare?from=%s&to=%s&per_page=100&page=%d",
			baseURL, projectID, url.QueryEscape(from), url.QueryEscape(to), page)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			g.logger.Errorf("failed to create gitlab api request: %v", err)
			return nil, err
		}

		req.Header.Set("PRIVATE-TOKEN", g.Token)

		resp, err := g.Client.Do(req)
		if err != nil {
			g.logger.Errorf("failed to send request to gitlab api: %v", err)
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			err := fmt.Errorf("gitlab api returned non-200 status for compare diff: %s, body: %s", resp.Status, string(body))
			g.logger.Error(err)
			return nil, err
		}

		var result struct {
			Diffs []gateway.DiffEntry `json:"diffs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			g.logger.Errorf("failed to decode compare diff: %v", err)
			return nil, err
		}
		resp.Body.Close()

		allDiffs = append(allDiffs, result.Diffs...)

		if len(result.Diffs) < 100 {
			break
		}
		page++
	}

	return allDiffs, nil
}

func (g *HTTPGitLabGateway) GetRawFile(projectID int, filePath, ref string) ([]byte, error) {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%d/repository/files/%s/raw?ref=%s",
		baseURL, projectID, url.PathEscape(filePath), url.QueryEscape(ref))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return nil, err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("gitlab api returned non-200 status for raw file: %s, body: %s", resp.Status, string(body))
		g.logger.Error(err)
		return nil, err
	}

	return io.ReadAll(resp.Body)
}

func (g *HTTPGitLabGateway) FileExists(projectID int, filePath, ref string) (bool, error) {
	baseURL := g.baseURL()
	apiURL := fmt.Sprintf("%s/projects/%d/repository/files/%s/raw?ref=%s",
		baseURL, projectID, url.PathEscape(filePath), url.QueryEscape(ref))

	req, err := http.NewRequest("HEAD", apiURL, nil)
	if err != nil {
		g.logger.Errorf("failed to create gitlab api request: %v", err)
		return false, err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)

	resp, err := g.Client.Do(req)
	if err != nil {
		g.logger.Errorf("failed to send request to gitlab api: %v", err)
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("gitlab api returned unexpected status for file exists: %s", resp.Status)
}

// GetBranchHead returns the SHA of the latest commit on the given branch.
func (g *HTTPGitLabGateway) GetBranchHead(projectID int, branchName string) (string, error) {
	commits, err := g.GetCommits(projectID, branchName, 1)
	if err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("branch %q has no commits", branchName)
	}
	return commits[0].ID, nil
}

// ListCommitsAfter walks the first-parent history of ref (newest first, page by
// page) until it reaches `after`, and returns the commits seen before it, oldest first.
func (g *HTTPGitLabGateway) ListCommitsAfter(projectID int, ref, after string, max int) ([]gateway.CommitInfo, error) {
	const perPage = 100
	var newestFirst []gateway.CommitInfo

	for page := 1; ; page++ {
		apiURL := fmt.Sprintf("%s/projects/%d/repository/commits?ref_name=%s&first_parent=true&per_page=%d&page=%d",
			g.baseURL(), projectID, url.QueryEscape(ref), perPage, page)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("PRIVATE-TOKEN", g.Token)

		resp, err := g.Client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("gitlab api returned non-200 status for list commits: %s, body: %s", resp.Status, string(body))
		}
		var commits []gateway.CommitInfo
		err = json.NewDecoder(resp.Body).Decode(&commits)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode commits: %w", err)
		}

		for _, c := range commits {
			if c.ID == after || (len(after) >= 7 && strings.HasPrefix(c.ID, after)) {
				return reverseCommits(newestFirst), nil
			}
			newestFirst = append(newestFirst, c)
			if len(newestFirst) > max {
				return nil, fmt.Errorf("more than %d commits on %s after %s", max, ref, after)
			}
		}
		if len(commits) < perPage {
			return nil, fmt.Errorf("commit %s is not on the first-parent history of %s", after, ref)
		}
	}
}

func reverseCommits(c []gateway.CommitInfo) []gateway.CommitInfo {
	out := make([]gateway.CommitInfo, len(c))
	for i := range c {
		out[len(c)-1-i] = c[i]
	}
	return out
}
