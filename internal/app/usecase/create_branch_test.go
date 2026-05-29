package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// MockGitGateway is a mock for GitGateway
type MockGitGateway struct {
	mock.Mock
}

func (m *MockGitGateway) CreateOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) error {
	args := m.Called(ctx, repository, branch, sourceBranch)
	return args.Error(0)
}

func (m *MockGitGateway) ListFiles(repoPath string) ([]string, error) {
	args := m.Called(repoPath)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockGitGateway) DeleteLocalBranch(repoPath, branchName string) error {
	args := m.Called(repoPath, branchName)
	return args.Error(0)
}

func (m *MockGitGateway) CheckoutBranch(repoPath, branchName string) error {
	args := m.Called(repoPath, branchName)
	return args.Error(0)
}

func (m *MockGitGateway) RemoveDirectory(repoPath, dirName string) error {
	args := m.Called(repoPath, dirName)
	return args.Error(0)
}

func (m *MockGitGateway) AddAll(repoPath string) error {
	args := m.Called(repoPath)
	return args.Error(0)
}

func (m *MockGitGateway) CreateEmptyOrphanBranch(ctx context.Context, repository *entity.Repository, branch *entity.Branch, sourceBranch string) error {
	args := m.Called(ctx, repository, branch, sourceBranch)
	return args.Error(0)
}

func (m *MockGitGateway) CleanWorkdir(repoPath string) error {
	args := m.Called(repoPath)
	return args.Error(0)
}

func (m *MockGitGateway) Commit(repoPath, message string) error {
	args := m.Called(repoPath, message)
	return args.Error(0)
}

func (m *MockGitGateway) BranchExists(repoPath, branchName string) (bool, error) {
	args := m.Called(repoPath, branchName)
	return args.Bool(0), args.Error(1)
}

func (m *MockGitGateway) GetCommitMessage(repoPath, commitHash string) (string, error) {
	args := m.Called(repoPath, commitHash)
	return args.String(0), args.Error(1)
}

func (m *MockGitGateway) GetCommitFiles(repoPath, commitHash string) ([]gateway.CommitFileInfo, error) {
	args := m.Called(repoPath, commitHash)
	return args.Get(0).([]gateway.CommitFileInfo), args.Error(1)
}

func (m *MockGitGateway) GetFileContentFromCommit(repoPath, commitHash, filePath string) ([]byte, error) {
	args := m.Called(repoPath, commitHash, filePath)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockGitGateway) GetBranchDiffFiles(repoPath, baseBranch, sourceBranch string) ([]gateway.CommitFileInfo, error) {
	args := m.Called(repoPath, baseBranch, sourceBranch)
	return args.Get(0).([]gateway.CommitFileInfo), args.Error(1)
}

func (m *MockGitGateway) ListFilesInBranch(repoPath, branchName string) ([]string, error) {
	args := m.Called(repoPath, branchName)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockGitGateway) GetMergeBase(repoPath, branch1, branch2 string) (string, error) {
	args := m.Called(repoPath, branch1, branch2)
	return args.String(0), args.Error(1)
}

// MockGitLabGateway is a mock for GitLabGateway
type MockGitLabGateway struct {
	mock.Mock
}

func (m *MockGitLabGateway) CommitFilesViaAPI(projectID, branchName, commitMessage string, actions []gateway.CommitAction) error {
	args := m.Called(projectID, branchName, commitMessage, actions)
	return args.Error(0)
}

func (m *MockGitLabGateway) CreateRemoteBranch(ctx context.Context, projectID, branchName, refSHA string) error {
	args := m.Called(ctx, projectID, branchName, refSHA)
	return args.Error(0)
}

func (m *MockGitLabGateway) FindProjectByName(name string) (*entity.Project, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*entity.Project), args.Error(1)
}

func (m *MockGitLabGateway) DeleteProject(projectID int) error {
	args := m.Called(projectID)
	return args.Error(0)
}

func (m *MockGitLabGateway) CreateProject(name string) (*entity.Project, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*entity.Project), args.Error(1)
}

func (m *MockGitLabGateway) DownloadRepoArchive(projectID int, ref string, writer *bytes.Buffer) error {
	args := m.Called(projectID, ref, writer)
	return args.Error(0)
}

func (m *MockGitLabGateway) GetBranches(projectID int) ([]gateway.BranchInfo, error) {
	args := m.Called(projectID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]gateway.BranchInfo), args.Error(1)
}

func (m *MockGitLabGateway) GetCommits(projectID int, branchName string, limit int) ([]gateway.CommitInfo, error) {
	args := m.Called(projectID, branchName, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]gateway.CommitInfo), args.Error(1)
}

func (m *MockGitLabGateway) GetCommitDiff(projectID int, sha string) ([]gateway.DiffEntry, error) {
	args := m.Called(projectID, sha)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]gateway.DiffEntry), args.Error(1)
}

func (m *MockGitLabGateway) GetCompareDiff(projectID int, from, to string) ([]gateway.DiffEntry, error) {
	args := m.Called(projectID, from, to)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]gateway.DiffEntry), args.Error(1)
}

func (m *MockGitLabGateway) GetRawFile(projectID int, filePath, ref string) ([]byte, error) {
	args := m.Called(projectID, filePath, ref)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockGitLabGateway) FileExists(projectID int, filePath, ref string) (bool, error) {
	args := m.Called(projectID, filePath, ref)
	return args.Bool(0), args.Error(1)
}

func setupTestCase(t *testing.T) (string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "repo")
	assert.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("test data"), 0600)
	assert.NoError(t, err)

	return tempDir, func() {
		os.RemoveAll(tempDir)
	}
}

func TestCreateAndPushOrphanBranchUseCase_Execute_Success(t *testing.T) {
	repoPath, cleanup := setupTestCase(t)
	defer cleanup()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway, log)

	input := Input{
		RepoPath:     repoPath,
		BranchName:   "new-orphan-branch",
		SourceBranch: "main",
	}
	projectName := filepath.Base(repoPath)
	project := &entity.Project{ID: 1, Name: projectName}

	gitlabGateway.On("FindProjectByName", projectName).Return(nil, nil)
	gitlabGateway.On("CreateProject", projectName).Return(project, nil)
	gitGateway.On("CreateOrphanBranch", mock.Anything, mock.Anything, mock.Anything, input.SourceBranch).Return(nil)
	gitGateway.On("RemoveDirectory", input.RepoPath, "vendor").Return(nil)
	gitGateway.On("ListFiles", input.RepoPath).Return([]string{"test.txt"}, nil)
	gitlabGateway.On("CommitFilesViaAPI", "1", input.BranchName, mock.Anything, mock.Anything).Return(nil)
	gitGateway.On("CheckoutBranch", input.RepoPath, input.SourceBranch).Return(nil)
	gitGateway.On("DeleteLocalBranch", input.RepoPath, input.BranchName).Return(nil)
	gitGateway.On("Commit", input.RepoPath, mock.Anything).Return(nil)

	duration, filesCount, err := useCase.Execute(context.Background(), input)

	assert.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 1, filesCount)
	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateAndPushOrphanBranchUseCase_Execute_FindProjectByName_Error(t *testing.T) {
	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway, log)

	input := Input{RepoPath: "/path/to/repo.git"}
	projectName := "repo"
	expectedErr := errors.New("find project error")

	gitlabGateway.On("FindProjectByName", projectName).Return(nil, expectedErr)

	_, _, err := useCase.Execute(context.Background(), input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "find project error")
	gitlabGateway.AssertExpectations(t)
}

func TestCreateAndPushOrphanBranchUseCase_Execute_CreateOrphanBranch_Error(t *testing.T) {
	repoPath, cleanup := setupTestCase(t)
	defer cleanup()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway, log)

	input := Input{
		RepoPath:     repoPath,
		BranchName:   "new-orphan-branch",
		SourceBranch: "main",
	}
	projectName := filepath.Base(repoPath)
	project := &entity.Project{ID: 1, Name: projectName}
	expectedErr := errors.New("create branch error")

	gitlabGateway.On("FindProjectByName", projectName).Return(nil, nil)
	gitlabGateway.On("CreateProject", projectName).Return(project, nil)
	gitGateway.On("CreateOrphanBranch", mock.Anything, mock.Anything, mock.Anything, input.SourceBranch).Return(expectedErr)

	_, _, err := useCase.Execute(context.Background(), input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create branch error")
	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateAndPushOrphanBranchUseCase_Execute_CommitFilesViaAPI_Error(t *testing.T) {
	repoPath, cleanup := setupTestCase(t)
	defer cleanup()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway, log)

	input := Input{
		RepoPath:     repoPath,
		BranchName:   "new-orphan-branch",
		SourceBranch: "main",
	}
	projectName := filepath.Base(repoPath)
	project := &entity.Project{ID: 1, Name: projectName}
	expectedErr := errors.New("commit error")

	gitlabGateway.On("FindProjectByName", projectName).Return(nil, nil)
	gitlabGateway.On("CreateProject", projectName).Return(project, nil)
	gitGateway.On("CreateOrphanBranch", mock.Anything, mock.Anything, mock.Anything, input.SourceBranch).Return(nil)
	gitGateway.On("RemoveDirectory", input.RepoPath, "vendor").Return(nil)
	gitGateway.On("Commit", input.RepoPath, mock.Anything).Return(nil)
	gitGateway.On("ListFiles", input.RepoPath).Return([]string{"test.txt"}, nil)
	gitlabGateway.On("CommitFilesViaAPI", "1", input.BranchName, mock.Anything, mock.Anything).Return(expectedErr)
	gitGateway.On("CheckoutBranch", input.RepoPath, input.SourceBranch).Return(nil)
	gitGateway.On("DeleteLocalBranch", input.RepoPath, input.BranchName).Return(nil)

	_, _, err := useCase.Execute(context.Background(), input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "commit error")
	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateAndPushOrphanBranchUseCase_Execute_DeleteProject_Error(t *testing.T) {
	repoPath, cleanup := setupTestCase(t)
	defer cleanup()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateAndPushOrphanBranchUseCase(gitGateway, gitlabGateway, log)

	input := Input{RepoPath: repoPath}
	projectName := filepath.Base(repoPath)
	project := &entity.Project{ID: 1, Name: projectName}
	expectedErr := errors.New("delete project error")

	gitlabGateway.On("FindProjectByName", projectName).Return(project, nil)
	gitlabGateway.On("DeleteProject", project.ID).Return(expectedErr)

	_, _, err := useCase.Execute(context.Background(), input)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, expectedErr) || err.Error() == fmt.Sprintf("failed to delete project: %s", expectedErr))
	gitlabGateway.AssertExpectations(t)
}
