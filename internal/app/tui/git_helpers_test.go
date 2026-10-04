package tui

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- simple helpers ---

func TestToCommaSeparated(t *testing.T) {
	assert.Equal(t, "a,b,c", toCommaSeparated([]string{"a", "b", "c"}))
	assert.Equal(t, "", toCommaSeparated([]string{}))
}

func TestSafeAtoi(t *testing.T) {
	assert.Equal(t, 42, safeAtoi("42", 10))
	assert.Equal(t, 10, safeAtoi("", 10))
	assert.Equal(t, 10, safeAtoi("abc", 10))
	assert.Equal(t, 10, safeAtoi("0", 10))
	assert.Equal(t, 5, safeAtoi("-1", 5))
}

// --- git helpers ---

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644))
	cmd = exec.Command("git", "add", "a.txt")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	return dir
}

func TestGetGitBranches(t *testing.T) {
	dir := setupTestGitRepo(t)

	// Create a second branch
	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	branches := getGitBranches(dir)
	assert.Equal(t, []string{"feature", "master"}, branches)
}

func TestGetGitFiles(t *testing.T) {
	dir := setupTestGitRepo(t)

	files := getGitFiles(dir)
	assert.Equal(t, []string{"a.txt"}, files)
}

// --- mock GitLab gateway ---

type mockGitLabGateway struct {
	branches       []gateway.BranchInfo
	commits        []gateway.CommitInfo
	diffs          []gateway.DiffEntry
	findProject    *entity.Project
	findProjectErr error
}

func (m *mockGitLabGateway) CommitFilesViaAPI(projectID, branchName, commitMessage string, actions []gateway.CommitAction) (string, error) {
	return "", nil
}
func (m *mockGitLabGateway) GetBranchHead(projectID int, branchName string) (string, error) {
	return "", nil
}
func (m *mockGitLabGateway) CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA string) error {
	return nil
}
func (m *mockGitLabGateway) FindProjectByName(name string) (*entity.Project, error) {
	return m.findProject, m.findProjectErr
}
func (m *mockGitLabGateway) DeleteProject(projectID int) error { return nil }
func (m *mockGitLabGateway) CreateProject(name string) (*entity.Project, error) {
	return nil, nil
}
func (m *mockGitLabGateway) DownloadRepoArchive(projectID int, ref string, writer *bytes.Buffer) error {
	return nil
}
func (m *mockGitLabGateway) GetBranches(projectID int) ([]gateway.BranchInfo, error) {
	return m.branches, nil
}
func (m *mockGitLabGateway) GetCommits(projectID int, branchName string, limit int) ([]gateway.CommitInfo, error) {
	return m.commits, nil
}
func (m *mockGitLabGateway) GetCommitDiff(projectID int, sha string) ([]gateway.DiffEntry, error) {
	return m.diffs, nil
}
func (m *mockGitLabGateway) GetCompareDiff(projectID int, from, to string) ([]gateway.DiffEntry, error) {
	return m.diffs, nil
}
func (m *mockGitLabGateway) GetRawFile(projectID int, filePath, ref string) ([]byte, error) {
	return nil, nil
}
func (m *mockGitLabGateway) FileExists(projectID int, filePath, ref string) (bool, error) {
	return false, nil
}

func TestGetGitLabBranches(t *testing.T) {
	gw := &mockGitLabGateway{
		findProject: &entity.Project{ID: 7, Name: "my-project"},
		branches: []gateway.BranchInfo{
			{Name: "develop"},
			{Name: "main", Default: true},
		},
	}
	branches := getGitLabBranches(gw, "/tmp/my-project")
	assert.Equal(t, []string{"develop", "main"}, branches)
}

func TestGetGitLabBranches_ProjectNotFound(t *testing.T) {
	gw := &mockGitLabGateway{findProject: nil}
	branches := getGitLabBranches(gw, "/tmp/my-project")
	assert.Nil(t, branches)
}

func TestGetFilesFromGitLabCommits(t *testing.T) {
	gw := &mockGitLabGateway{
		findProject: &entity.Project{ID: 7, Name: "my-project"},
		commits: []gateway.CommitInfo{
			{ID: "aaa", Message: "first"},
			{ID: "bbb", Message: "second"},
		},
		diffs: []gateway.DiffEntry{
			{NewPath: "main.go", NewFile: true},
			{NewPath: "old.go", DeletedFile: true},
			{NewPath: "README.md", NewFile: true},
		},
	}
	files, err := getFilesFromGitLabCommits(gw, "/tmp/my-project", "main", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"README.md", "main.go"}, files)
}

func TestGetFilesFromGitLabCommits_ProjectNotFound(t *testing.T) {
	gw := &mockGitLabGateway{findProject: nil}
	files, err := getFilesFromGitLabCommits(gw, "/tmp/my-project", "main", 1)
	require.Error(t, err)
	assert.Nil(t, files)
}
