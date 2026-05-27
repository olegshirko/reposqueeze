package usecase

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// buildTestZip создаёт in-memory ZIP архив, имитирующий ответ GitLab.
// prefix — корневая папка внутри архива (например, "my-project-a1b2c3d4/").
func buildTestZip(t *testing.T, prefix string, files map[string]string) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	// GitLab всегда добавляет корневую директорию первой записью.
	if prefix != "" {
		_, err := zw.Create(prefix)
		require.NoError(t, err)
	}

	for name, content := range files {
		w, err := zw.Create(prefix + name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())
	return buf
}

func TestCreateOrphanBranchFromGitlabUseCase_Execute_Success_NewBranch(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)

	input := CreateOrphanBranchFromGitlabInput{
		RepoPath:   repoPath,
		BranchName: "orphan-from-gitlab",
		Ref:        "main",
		Commit:     true,
	}

	project := &entity.Project{ID: 42, Name: "my-project"}

	zipBuf := buildTestZip(t, "my-project-a1b2c3d4/", map[string]string{
		"README.md": "# Hello",
		"main.go":   "package main",
	})

	gitlabGateway.On("FindProjectByName", "my-project").Return(project, nil)
	gitGateway.On("BranchExists", repoPath, "orphan-from-gitlab").Return(false, nil)
	gitGateway.On("CreateEmptyOrphanBranch", mock.Anything, mock.Anything, mock.Anything, "").Return(nil)
	gitGateway.On("CleanWorkdir", repoPath).Return(nil)
	gitlabGateway.On("DownloadRepoArchive", 42, "main", mock.Anything).Run(func(args mock.Arguments) {
		buf := args.Get(2).(*bytes.Buffer)
		_, err := buf.Write(zipBuf.Bytes())
		require.NoError(t, err)
	}).Return(nil)
	gitGateway.On("Commit", repoPath, "Add project files to orphan branch orphan-from-gitlab").Return(nil)

	duration, filesCount, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 2, filesCount)

	assert.FileExists(t, filepath.Join(repoPath, "README.md"))
	assert.FileExists(t, filepath.Join(repoPath, "main.go"))

	readme, _ := os.ReadFile(filepath.Join(repoPath, "README.md"))
	assert.Equal(t, "# Hello", string(readme))

	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateOrphanBranchFromGitlabUseCase_Execute_ExistingBranch(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)

	input := CreateOrphanBranchFromGitlabInput{
		RepoPath:   repoPath,
		BranchName: "feature-x",
		Ref:        "develop",
		Commit:     true,
	}

	project := &entity.Project{ID: 42, Name: "my-project"}

	zipBuf := buildTestZip(t, "my-project-a1b2c3d4/", map[string]string{
		"README.md": "# Hello",
	})

	gitlabGateway.On("FindProjectByName", "my-project").Return(project, nil)
	gitGateway.On("BranchExists", repoPath, "feature-x").Return(true, nil)
	gitGateway.On("CheckoutBranch", repoPath, "feature-x").Return(nil)
	gitlabGateway.On("DownloadRepoArchive", 42, "develop", mock.Anything).Run(func(args mock.Arguments) {
		buf := args.Get(2).(*bytes.Buffer)
		_, err := buf.Write(zipBuf.Bytes())
		require.NoError(t, err)
	}).Return(nil)
	gitGateway.On("Commit", repoPath, "Add project files to orphan branch feature-x").Return(nil)

	duration, filesCount, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 1, filesCount)

	assert.FileExists(t, filepath.Join(repoPath, "README.md"))
	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateOrphanBranchFromGitlabUseCase_Execute_NoCommit(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)

	input := CreateOrphanBranchFromGitlabInput{
		RepoPath:   repoPath,
		BranchName: "no-commit-branch",
		Ref:        "main",
		Commit:     false,
	}

	project := &entity.Project{ID: 42, Name: "my-project"}

	zipBuf := buildTestZip(t, "my-project-a1b2c3d4/", map[string]string{
		"README.md": "# Hello",
	})

	gitlabGateway.On("FindProjectByName", "my-project").Return(project, nil)
	gitGateway.On("BranchExists", repoPath, "no-commit-branch").Return(false, nil)
	gitGateway.On("CreateEmptyOrphanBranch", mock.Anything, mock.Anything, mock.Anything, "").Return(nil)
	gitGateway.On("CleanWorkdir", repoPath).Return(nil)
	gitlabGateway.On("DownloadRepoArchive", 42, "main", mock.Anything).Run(func(args mock.Arguments) {
		buf := args.Get(2).(*bytes.Buffer)
		_, err := buf.Write(zipBuf.Bytes())
		require.NoError(t, err)
	}).Return(nil)
	// Commit must NOT be called when Commit=false

	duration, filesCount, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 0, int(duration))
	assert.Equal(t, 1, filesCount)
	assert.FileExists(t, filepath.Join(repoPath, "README.md"))
	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateOrphanBranchFromGitlabUseCase_Execute_ProjectNotFound(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)

	input := CreateOrphanBranchFromGitlabInput{
		RepoPath:   repoPath,
		BranchName: "orphan-from-gitlab",
		Commit:     true,
	}

	gitlabGateway.On("FindProjectByName", "my-project").Return(nil, nil)

	duration, filesCount, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 0, int(duration))
	assert.Equal(t, 0, filesCount)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateOrphanBranchFromGitlabUseCase_Execute_FindProjectError(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)

	input := CreateOrphanBranchFromGitlabInput{
		RepoPath:   repoPath,
		BranchName: "orphan-from-gitlab",
		Commit:     true,
	}

	expectedErr := errors.New("gitlab api error")
	gitlabGateway.On("FindProjectByName", "my-project").Return(nil, expectedErr)

	_, _, err := useCase.Execute(context.Background(), input)

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
	gitlabGateway.AssertExpectations(t)
}

func TestCreateOrphanBranchFromGitlabUseCase_Execute_ArchiveWithoutRootPrefix(t *testing.T) {
	// Edge-case: архив без корневой папки (нестандартный для GitLab, но проверим защиту).
	repoPath := filepath.Join(t.TempDir(), "my-project")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewCreateOrphanBranchFromGitlabUseCase(gitGateway, gitlabGateway, log)

	input := CreateOrphanBranchFromGitlabInput{
		RepoPath:   repoPath,
		BranchName: "orphan-from-gitlab",
		Commit:     true,
	}

	project := &entity.Project{ID: 42, Name: "my-project"}

	// Архив БЕЗ корневой директории — первый файл просто README.md
	zipBuf := buildTestZip(t, "", map[string]string{
		"README.md": "# Hello",
	})

	gitlabGateway.On("FindProjectByName", "my-project").Return(project, nil)
	gitGateway.On("BranchExists", repoPath, "orphan-from-gitlab").Return(false, nil)
	gitGateway.On("CreateEmptyOrphanBranch", mock.Anything, mock.Anything, mock.Anything, "").Return(nil)
	gitGateway.On("CleanWorkdir", repoPath).Return(nil)
	gitlabGateway.On("DownloadRepoArchive", 42, "", mock.Anything).Run(func(args mock.Arguments) {
		buf := args.Get(2).(*bytes.Buffer)
		_, err := buf.Write(zipBuf.Bytes())
		require.NoError(t, err)
	}).Return(nil)
	gitGateway.On("Commit", repoPath, mock.Anything).Return(nil)

	duration, filesCount, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	// При таком архиве firstFile.Name = "README.md", rootDir = "README.md/",
	// и ни один файл не пройдёт HasPrefix -> файлы не извлекутся.
	// filesCount считает файлы внутри ZIP до фильтрации rootDir, поэтому он = 1.
	assert.Equal(t, 1, filesCount)
	assert.NoFileExists(t, filepath.Join(repoPath, "README.md"))
}
