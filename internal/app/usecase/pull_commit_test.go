package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
)

func (e *syncEnv) picker() *PullCommitUseCase {
	return NewPullCommitUseCase(e.git, e.gl, state.NewFileStore(e.git), newTestLogger())
}

func (e *syncEnv) pick(in PullCommitInput) *PullCommitResult {
	e.t.Helper()
	in.RepoPath = e.repo
	res, err := e.picker().Execute(context.Background(), in)
	require.NoError(e.t, err)
	return res
}

func TestPullCommit_SelectedCommitsOnly(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.gl.editAs("Alice Smith", "feat: one", map[string]string{"one.txt": "1"})
	e.gl.editAs("Alice Smith", "feat: unwanted", map[string]string{"a.txt": "unwanted"})
	e.gl.editAs("Bob Jones", "fix(b): three\n\nWhy it matters.", map[string]string{"b.txt": "b3"})
	one, three := e.gl.commits[1].id, e.gl.commits[3].id

	// Given newest first and abbreviated: applied oldest first anyway.
	res := e.pick(PullCommitInput{Commits: []string{three[:10], one}, Format: entity.CommitFormat{Type: "fix", Task: "TASK-NUMBER01"}})
	require.Len(t, res.Picked, 2)

	assert.Equal(t, []string{"fix: one TASK-NUMBER01|Alice Smith", "fix: three TASK-NUMBER01|Bob Jones"},
		strings.Split(e.run("log", "-n", "2", "--reverse", "--format=%s|%an"), "\n"))
	assert.Contains(t, e.run("log", "-1", "--format=%B"), "Why it matters.")
	assert.Contains(t, e.run("log", "-1", "--format=%B"), TrailerReplayedFrom+": myproj@"+three)
	assert.Equal(t, "1", e.read("one.txt"))
	assert.Equal(t, "b3", e.read("b.txt"))
	assert.Equal(t, "a", e.read("a.txt"), "unselected commit is not applied")

	// Picking again is a no-op.
	res = e.pick(PullCommitInput{Commits: []string{one}})
	assert.Empty(t, res.Picked)
	require.Len(t, res.Skipped, 1)

	// The listing marks picked commits.
	commits, picked, err := e.picker().ListCommits(e.repo, "release", 10)
	require.NoError(t, err)
	assert.Len(t, commits, 4)
	assert.Contains(t, picked, one)
	assert.NotContains(t, picked, e.gl.commits[2].id)
}

func TestPullCommit_MergesIntoLocalFile(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"f.txt": "1\n2\n3\n4\n5\n"})
	e.gl.editAs("Alice Smith", "remote", map[string]string{"f.txt": "1\n2\n3\n4\nR5\n"})
	e.write("f.txt", "L1\n2\n3\n4\n5\n")
	e.commit("local")

	res := e.pick(PullCommitInput{Commits: []string{e.gl.head().id}})
	require.Len(t, res.Picked, 1)
	assert.Equal(t, "L1\n2\n3\n4\nR5\n", e.read("f.txt"))
}

func TestPullCommit_ConflictStopsLikeCherryPick(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"f.txt": "x\n", "g.txt": "g"})
	e.gl.editAs("Alice Smith", "fine", map[string]string{"g.txt": "g1"})
	e.gl.editAs("Bob Jones", "clash", map[string]string{"f.txt": "remote\n", "h.txt": "h"})
	e.gl.editAs("Bob Jones", "later", map[string]string{"g.txt": "g2"})
	e.write("f.txt", "local\n")
	e.commit("local")

	res := e.pick(PullCommitInput{Commits: []string{e.gl.commits[1].id, e.gl.commits[2].id, e.gl.commits[3].id},
		Format: entity.CommitFormat{Type: "feat", Task: "TASK-9"}})
	require.Len(t, res.Picked, 1, "the commit before the conflict is applied")
	require.NotNil(t, res.StoppedAt)
	assert.Equal(t, "clash", res.StoppedAt.Title)
	assert.Equal(t, []string{"f.txt"}, res.Conflicts)
	assert.Equal(t, []string{e.gl.commits[3].id}, res.Remaining)

	assert.Contains(t, e.read("f.txt"), "<<<<<<< local main")
	assert.Equal(t, "h", e.read("h.txt"), "non-conflicting part of the commit is in the working tree")
	gitDir := e.run("rev-parse", "--absolute-git-dir")
	msg, err := os.ReadFile(filepath.Join(gitDir, "reposqueeze", "PICK_MSG"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(msg), "feat: clash TASK-9"))
	assert.Contains(t, res.CommitCommand, "--author \"Bob Jones <bob.jones@example.com>\"")

	// Resolving and committing with the suggested command keeps the provenance trailer.
	e.write("f.txt", "resolved\n")
	e.run("add", "-A")
	e.run("commit", "-q", "-F", filepath.Join(gitDir, "reposqueeze", "PICK_MSG"))
	res = e.pick(PullCommitInput{Commits: res.Remaining})
	require.Len(t, res.Picked, 1)
	assert.Equal(t, "g2", e.read("g.txt"))
}

func TestPullCommit_AbortAndDirtyTree(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"f.txt": "x\n"})
	e.gl.editAs("Bob Jones", "clash", map[string]string{"f.txt": "remote\n", "new.txt": "n"})
	e.write("f.txt", "local\n")
	e.commit("local")
	head := e.run("rev-parse", "HEAD")

	_, err := e.picker().Execute(context.Background(), PullCommitInput{RepoPath: e.repo, Commits: []string{e.gl.head().id}, Strategy: StrategyAbort})
	require.Error(t, err)
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))
	assert.Equal(t, "", e.run("status", "--porcelain"))

	e.write("dirty.txt", "x")
	_, err = e.picker().Execute(context.Background(), PullCommitInput{RepoPath: e.repo, Commits: []string{e.gl.head().id}})
	require.Error(t, err)

	_, err = e.picker().Execute(context.Background(), PullCommitInput{RepoPath: e.repo, Commits: []string{"deadbeefdeadbeef"}})
	require.Error(t, err)
}

func TestReplay_SkipsCommitsPickedEarlier(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	e.init()
	e.gl.editAs("Alice Smith", "first", map[string]string{"one.txt": "1"})
	e.gl.editAs("Alice Smith", "second", map[string]string{"two.txt": "2"})
	e.pick(PullCommitInput{Commits: []string{e.gl.commits[2].id}})

	res := e.sync(SyncInput{Replay: true})
	require.Len(t, res.Replayed, 2)
	titles := []string{res.Replayed[0].Title, res.Replayed[1].Title}
	assert.Contains(t, titles, "second (picked earlier)")
	assert.Equal(t, 1, strings.Count(e.run("log", "--format=%s"), "second"), "not duplicated")
	e.assertInSync()
}
