package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRepo(t *testing.T) string {
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

	err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)
	require.NoError(t, err)

	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/test/repo.git")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	return dir
}

func TestOSExecGitGateway_CreateOrphanBranch(t *testing.T) {
	repoPath := setupTestRepo(t)
	gateway := NewOSExecGitGateway(logger.NewLoggerWithWriter(logrus.New().Out))

	repo := &entity.Repository{Path: repoPath}
	branch := &entity.Branch{Name: "new-orphan-branch"}

	err := gateway.CreateOrphanBranch(context.Background(), repo, branch, "")
	require.NoError(t, err)

	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)

	assert.Equal(t, branch.Name, strings.TrimSpace(string(output)))
}

func TestOSExecGitGateway_ListFiles(t *testing.T) {
	repoPath := setupTestRepo(t)
	gateway := NewOSExecGitGateway(logger.NewLoggerWithWriter(logrus.New().Out))

	files, err := gateway.ListFiles(repoPath)
	require.NoError(t, err)

	assert.Len(t, files, 1)
	assert.Equal(t, "test.txt", files[0])
}