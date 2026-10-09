package git

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

func newSyncGW() *OSExecGitGateway {
	return NewOSExecGitGateway(logger.NewLoggerWithWriter(io.Discard))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestSyncGit_DiffCommitRestore(t *testing.T) {
	repo := setupTestRepo(t)
	g := newSyncGW()

	base, err := g.RevParse(repo, "HEAD")
	require.NoError(t, err)
	branch, err := g.CurrentBranch(repo)
	require.NoError(t, err)
	assert.NotEmpty(t, branch)

	clean, err := g.IsClean(repo)
	require.NoError(t, err)
	assert.True(t, clean)

	write(t, repo, "dir/new file.txt", "new")
	write(t, repo, "test.txt", "changed")
	clean, _ = g.IsClean(repo)
	assert.False(t, clean)

	head, err := g.CommitPaths(repo, "msg\n\nReposqueeze-Remote: p/main@abc", []string{"dir/new file.txt", "test.txt"}, gateway.CommitOptions{})
	require.NoError(t, err)

	files, err := g.DiffFiles(repo, base, head)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"dir/new file.txt", "test.txt"}, []string{files[0].Path, files[1].Path})

	content, found, err := g.FileAtRef(repo, base, "test.txt")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "hello", string(content))
	_, found, err = g.FileAtRef(repo, base, "dir/new file.txt")
	require.NoError(t, err)
	assert.False(t, found)

	sha, value, err := g.FindLastTrailer(repo, "HEAD", "Reposqueeze-Remote")
	require.NoError(t, err)
	assert.Equal(t, head, sha)
	assert.Equal(t, "p/main@abc", value)

	ok, err := g.IsAncestor(repo, base, head)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = g.IsAncestor(repo, head, base)
	require.NoError(t, err)
	assert.False(t, ok)

	// RestorePaths reverts tracked files and removes files HEAD lacks.
	write(t, repo, "test.txt", "dirty")
	write(t, repo, "untracked.txt", "x")
	require.NoError(t, g.RestorePaths(repo, []string{"test.txt", "untracked.txt"}))
	clean, _ = g.IsClean(repo)
	assert.True(t, clean)

	// Author and date can be carried over from another commit.
	_, err = g.CommitPaths(repo, "replayed", nil, gateway.CommitOptions{
		AllowEmpty: true, AuthorName: "Jane Doe", AuthorEmail: "jane@example.com", AuthorDate: "2024-05-06T07:08:09Z",
	})
	require.NoError(t, err)
	out, err := g.gitString(repo, "log", "-1", "--format=%an|%ae|%aI")
	require.NoError(t, err)
	assert.Equal(t, "Jane Doe|jane@example.com|2024-05-06T07:08:09Z", out)
	_, err = g.gitString(repo, "reset", "-q", "--hard", "HEAD~1")
	require.NoError(t, err)

	commits, err := g.LogCommits(repo, "HEAD", 10)
	require.NoError(t, err)
	require.Len(t, commits, 2)
	assert.Equal(t, head, commits[0].ID)
	assert.Equal(t, "msg", commits[0].Message)
}

func TestSyncGit_MergeFile(t *testing.T) {
	g := newSyncGW()
	labels := [3]string{"local", "base", "gitlab"}

	res, err := g.MergeFile([]byte("A\nb\nc\n"), []byte("a\nb\nc\n"), []byte("a\nb\nC\n"), labels)
	require.NoError(t, err)
	assert.False(t, res.Conflicts)
	assert.Equal(t, "A\nb\nC\n", string(res.Content))

	res, err = g.MergeFile([]byte("x\n"), []byte("a\n"), []byte("y\n"), labels)
	require.NoError(t, err)
	assert.True(t, res.Conflicts)
	assert.Contains(t, string(res.Content), "<<<<<<< local")
	assert.Contains(t, string(res.Content), ">>>>>>> gitlab")
}

func TestSyncGit_Stash(t *testing.T) {
	repo := setupTestRepo(t)
	g := newSyncGW()

	stashed, err := g.StashPush(repo, "nothing")
	require.NoError(t, err)
	assert.False(t, stashed)

	write(t, repo, "test.txt", "wip")
	stashed, err = g.StashPush(repo, "wip")
	require.NoError(t, err)
	assert.True(t, stashed)
	clean, _ := g.IsClean(repo)
	assert.True(t, clean)

	require.NoError(t, g.StashPop(repo))
	data, _ := os.ReadFile(filepath.Join(repo, "test.txt"))
	assert.Equal(t, "wip", string(data))
}

func TestSyncGit_CheckCommitMessage(t *testing.T) {
	repo := setupTestRepo(t)
	g := newSyncGW()

	// No hook: everything passes.
	require.NoError(t, g.CheckCommitMessage(repo, "anything"))

	hook := filepath.Join(repo, ".git", "hooks", "commit-msg")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nhead -1 \"$1\" | grep -Eq '^(fix|feat|test): .+ TASK-[0-9]+$' || { echo 'bad format' >&2; exit 1; }\n"), 0o755))

	require.NoError(t, g.CheckCommitMessage(repo, "fix: commit message TASK-01\n\nbody"))
	err := g.CheckCommitMessage(repo, "sync main with gitlab")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad format")
}
