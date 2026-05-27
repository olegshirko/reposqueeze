package usecase

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullFilesUseCase_Execute_WithFilesList(t *testing.T) {
	repoPath := t.TempDir()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
		Files:      "README.md, docs/guide.md",
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetRawFile", 7, "README.md", "main").Return([]byte("# Hello"), nil)
	gitlabGateway.On("GetRawFile", 7, "docs/guide.md", "main").Return([]byte("Guide"), nil)

	duration, count, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 2, count)
	assert.FileExists(t, filepath.Join(repoPath, "README.md"))
	assert.FileExists(t, filepath.Join(repoPath, "docs", "guide.md"))

	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestPullFilesUseCase_Execute_WithCommits(t *testing.T) {
	repoPath := t.TempDir()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
		Commits:    2,
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}
	commits := []gateway.CommitInfo{
		{ID: "aaa", Message: "first"},
		{ID: "bbb", Message: "second"},
	}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetCommits", 7, "main", 2).Return(commits, nil)

	// Diff для первого коммита
	gitlabGateway.On("GetCommitDiff", 7, "aaa").Return([]gateway.DiffEntry{
		{NewPath: "main.go", NewFile: true},
	}, nil)
	gitlabGateway.On("GetRawFile", 7, "main.go", "aaa").Return([]byte("package main"), nil)

	// Diff для второго коммита
	gitlabGateway.On("GetCommitDiff", 7, "bbb").Return([]gateway.DiffEntry{
		{NewPath: "README.md", NewFile: true},
	}, nil)
	gitlabGateway.On("GetRawFile", 7, "README.md", "bbb").Return([]byte("# Hello"), nil)

	duration, count, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 2, count)
	assert.FileExists(t, filepath.Join(repoPath, "main.go"))
	assert.FileExists(t, filepath.Join(repoPath, "README.md"))

	gitlabGateway.AssertExpectations(t)
}

func TestPullFilesUseCase_Execute_DeletedFile(t *testing.T) {
	repoPath := t.TempDir()
	// Создаём локальный файл, который должен быть удалён
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "old.go"), []byte("old"), 0644))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
		Commits:    1,
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}
	commits := []gateway.CommitInfo{
		{ID: "ccc", Message: "remove old.go"},
	}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetCommits", 7, "main", 1).Return(commits, nil)
	gitlabGateway.On("GetCommitDiff", 7, "ccc").Return([]gateway.DiffEntry{
		{NewPath: "old.go", DeletedFile: true},
	}, nil)

	duration, count, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 0, count) // deleted files don't count as "downloaded"
	assert.NoFileExists(t, filepath.Join(repoPath, "old.go"))

	gitlabGateway.AssertExpectations(t)
}

func TestPullFilesUseCase_Execute_RenamedFile(t *testing.T) {
	// Edge-case / known limitation: renamed files are downloaded to the new path,
	// but the old path is NOT deleted locally.
	repoPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "old.go"), []byte("old"), 0644))

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
		Commits:    1,
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}
	commits := []gateway.CommitInfo{
		{ID: "ddd", Message: "rename old.go -> new.go"},
	}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetCommits", 7, "main", 1).Return(commits, nil)
	gitlabGateway.On("GetCommitDiff", 7, "ddd").Return([]gateway.DiffEntry{
		{NewPath: "new.go", OldPath: "old.go", RenamedFile: true},
	}, nil)
	gitlabGateway.On("GetRawFile", 7, "new.go", "ddd").Return([]byte("new content"), nil)

	duration, count, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 1, count)
	assert.FileExists(t, filepath.Join(repoPath, "new.go"))
	// old.go should be removed because the diff indicates a rename
	assert.NoFileExists(t, filepath.Join(repoPath, "old.go"))

	gitlabGateway.AssertExpectations(t)
}

func TestPullFilesUseCase_Execute_ProjectNotFound(t *testing.T) {
	repoPath := t.TempDir()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
	}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(nil, nil)

	_, _, err := useCase.Execute(context.Background(), input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found on GitLab")
}

func TestPullFilesUseCase_Execute_NoCommits(t *testing.T) {
	repoPath := t.TempDir()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
		Commits:    5,
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetCommits", 7, "main", 5).Return([]gateway.CommitInfo{}, nil)

	_, _, err := useCase.Execute(context.Background(), input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no commits found")
}

func TestPullFilesUseCase_Execute_WithGitAdd(t *testing.T) {
	repoPath := t.TempDir()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:   repoPath,
		BranchName: "main",
		Files:      "README.md",
		GitAdd:     true,
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetRawFile", 7, "README.md", "main").Return([]byte("# Hello"), nil)
	gitGateway.On("AddAll", repoPath).Return(nil)

	duration, count, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 1, count)
	gitGateway.AssertExpectations(t)
	gitlabGateway.AssertExpectations(t)
}

func TestPullFilesUseCase_Execute_SinceCommit(t *testing.T) {
	repoPath := t.TempDir()

	log := logger.NewLoggerWithWriter(logrus.New().Out)
	gitGateway := new(MockGitGateway)
	gitlabGateway := new(MockGitLabGateway)
	useCase := NewPullFilesUseCase(gitGateway, gitlabGateway, log)

	input := PullFilesInput{
		RepoPath:    repoPath,
		BranchName:  "main",
		SinceCommit: "abc123",
	}

	project := &entity.Project{ID: 7, Name: filepath.Base(repoPath)}

	gitlabGateway.On("FindProjectByName", filepath.Base(repoPath)).Return(project, nil)
	gitlabGateway.On("GetCompareDiff", 7, "abc123", "main").Return([]gateway.DiffEntry{
		{NewPath: "main.go", NewFile: true},
		{NewPath: "old.go", DeletedFile: true},
		{NewPath: "README.md", NewFile: true},
	}, nil)
	gitlabGateway.On("GetRawFile", 7, "main.go", "main").Return([]byte("package main"), nil)
	gitlabGateway.On("GetRawFile", 7, "README.md", "main").Return([]byte("# Hello"), nil)

	duration, count, err := useCase.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, duration > 0)
	assert.Equal(t, 2, count)
	assert.FileExists(t, filepath.Join(repoPath, "main.go"))
	assert.FileExists(t, filepath.Join(repoPath, "README.md"))
	assert.NoFileExists(t, filepath.Join(repoPath, "old.go"))

	gitlabGateway.AssertExpectations(t)
}
