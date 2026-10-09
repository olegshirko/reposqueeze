package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

// logLines returns `git log` of the last n commits as "subject|author|date" lines, oldest first.
func (e *syncEnv) logLines(n int) []string {
	e.t.Helper()
	return strings.Split(e.run("log", "-n", strconv.Itoa(n), "--reverse", "--format=%s|%an|%aI"), "\n")
}

func TestReplay_PullsCommitsOneByOne(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.gl.editAs("Alice Smith", "feat: change a\n\nLonger body.", map[string]string{"a.txt": "a1"})
	e.gl.editAs("Bob Jones", "feat: add c", map[string]string{"c.txt": "c"})
	e.gl.editAs("Alice Smith", "chore: drop b", map[string]string{"b.txt": ""})

	res := e.sync(SyncInput{Replay: true})
	require.Len(t, res.Replayed, 3)
	assert.Equal(t, 0, res.Pushed)
	e.assertInSync()

	assert.Equal(t, []string{
		"feat: change a|T|2024-01-02T10:00:00Z",
		"feat: add c|T|2024-01-03T10:00:00Z",
		"chore: drop b|T|2024-01-04T10:00:00Z",
	}, e.logLines(3))

	// Messages are exactly the GitLab ones; the correspondence lives in the
	// state, and the last replayed commit is the new sync point.
	for i, pair := range res.Replayed {
		assert.Equal(t, e.gl.commits[i+1].id, pair.RemoteSHA)
		assert.NotContains(t, e.run("log", "-1", "--format=%B", pair.LocalSHA), "Reposqueeze")
	}
	assert.Equal(t, "feat: change a\n\nLonger body.", e.run("log", "-1", "--format=%B", res.Replayed[0].LocalSHA))
	assert.Equal(t, res.Replayed[2].LocalSHA, e.run("rev-parse", "HEAD"), "no extra sync commit")

	// Each commit holds only its own change.
	assert.Equal(t, "a.txt", e.run("show", "--format=", "--name-only", res.Replayed[0].LocalSHA))
	assert.Equal(t, "c.txt", e.run("show", "--format=", "--name-only", res.Replayed[1].LocalSHA))

	mirrors, _ := e.uc.Mirrors(e.repo)
	j := mirrors[0].Journal[0]
	assert.Equal(t, res.Replayed, j.Replayed)
	assert.Equal(t, e.run("rev-parse", "HEAD"), j.LocalSHA)
	assert.Equal(t, e.gl.head().id, j.RemoteSHA)

	res = e.sync(SyncInput{Replay: true})
	assert.True(t, res.Plan.Empty())
}

func TestReplay_WithLocalChangesElsewhere(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.gl.editAs("Alice Smith", "remote 1", map[string]string{"a.txt": "a1"})
	e.gl.editAs("Alice Smith", "remote 2", map[string]string{"a.txt": "a2"})
	e.write("b.txt", "b-local")
	e.write("new.txt", "n")
	e.commit("local work")

	res := e.sync(SyncInput{Replay: true})
	require.Len(t, res.Replayed, 2)
	assert.Equal(t, 2, res.Pushed)
	e.assertInSync()

	subjects := strings.Split(e.run("log", "-n", "3", "--reverse", "--format=%s"), "\n")
	assert.Equal(t, []string{"local work", "remote 1", "remote 2"}, subjects)
	mirrors, _ := e.uc.Mirrors(e.repo)
	assert.Equal(t, e.run("rev-parse", "HEAD"), mirrors[0].Current().LocalSHA)
	assert.Equal(t, e.gl.head().id, mirrors[0].Current().RemoteSHA)

	// The next sync has nothing to do: the pair is exact.
	plan, err := e.uc.Plan(context.Background(), SyncInput{RepoPath: e.repo, Replay: true})
	require.NoError(t, err)
	assert.True(t, plan.Empty())
}

func TestReplay_MergesIntoLocallyChangedFile(t *testing.T) {
	base := "1\n2\n3\n4\n5\n6\n7\n"
	e := newSyncEnv(t, map[string]string{"f.txt": base})
	e.init()
	e.gl.editAs("Alice Smith", "remote edits line 5", map[string]string{"f.txt": "1\n2\n3\n4\nR5\n6\n7\n"})
	e.gl.editAs("Alice Smith", "remote edits line 7", map[string]string{"f.txt": "1\n2\n3\n4\nR5\n6\nR7\n"})
	e.write("f.txt", "L1\n2\n3\n4\n5\n6\n7\n")
	e.commit("local edits line 1")

	res := e.sync(SyncInput{Replay: true})
	require.Len(t, res.Replayed, 2)
	assert.Equal(t, []string{"f.txt"}, res.Merged)

	// Each replayed commit applies only GitLab's change on top of the local file.
	assert.Equal(t, "L1\n2\n3\n4\nR5\n6\n7", e.run("show", res.Replayed[0].LocalSHA+":f.txt"))
	assert.Equal(t, "L1\n2\n3\n4\nR5\n6\nR7", e.run("show", res.Replayed[1].LocalSHA+":f.txt"))
	assert.Equal(t, "L1\n2\n3\n4\nR5\n6\nR7\n", e.gl.head().files["f.txt"])
	e.assertInSync()
}

func TestReplay_ConflictChangesNothing(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"f.txt": "x\n", "g.txt": "g"})
	e.init()
	e.gl.editAs("Alice Smith", "fine", map[string]string{"g.txt": "g1"})
	e.gl.editAs("Bob Jones", "clash", map[string]string{"f.txt": "remote\n"})
	e.write("f.txt", "local\n")
	e.commit("local")
	head := e.run("rev-parse", "HEAD")

	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo, Replay: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clash")
	assert.Contains(t, err.Error(), "f.txt")
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))
	assert.Equal(t, "", e.run("status", "--porcelain"))
	assert.Len(t, e.gl.commits, 3, "nothing pushed")

	mirrors, _ := e.uc.Mirrors(e.repo)
	assert.Empty(t, mirrors[0].Journal)
}

func TestReplay_Strategies(t *testing.T) {
	for _, tc := range []struct{ strategy, want string }{
		{StrategyLocal, "local\n"},
		{StrategyRemote, "remote\n"},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			e := newSyncEnv(t, map[string]string{"f.txt": "x\n"})
			e.init()
			e.gl.editAs("Bob Jones", "clash", map[string]string{"f.txt": "remote\n"})
			e.write("f.txt", "local\n")
			e.commit("local")

			res := e.sync(SyncInput{Replay: true, Strategy: tc.strategy})
			require.Len(t, res.Replayed, 1)
			assert.Equal(t, tc.want, e.read("f.txt"))
			assert.Equal(t, tc.want, e.gl.head().files["f.txt"])
			e.assertInSync()
		})
	}
}

func TestReplay_DryRunAndRevertedCommits(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	e.init()
	e.gl.editAs("Alice Smith", "add tmp", map[string]string{"tmp.txt": "t"})
	e.gl.editAs("Alice Smith", "remove tmp", map[string]string{"tmp.txt": ""})
	head := e.run("rev-parse", "HEAD")

	res := e.sync(SyncInput{Replay: true, DryRun: true})
	require.Len(t, res.Plan.RemoteCommits, 2)
	assert.Equal(t, "add tmp", res.Plan.RemoteCommits[0].Title)
	assert.Equal(t, []FileChange{{Path: "tmp.txt", Kind: ChangeAdded}}, res.Plan.RemoteCommits[0].Changes)
	assert.Contains(t, strings.Join(res.Plan.Lines(), "\n"), "replay GitLab commits one by one (2)")
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))

	// Net diff is empty, but both commits are still replayed.
	res = e.sync(SyncInput{Replay: true})
	require.Len(t, res.Replayed, 2)
	assert.Equal(t, "add tmp\nremove tmp", e.run("log", "-n", "2", "--reverse", "--format=%s"))
	e.assertInSync()
}

func TestReplay_PickedRecordsFollowHistory(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	e.gl.editAs("Alice Smith", "remote", map[string]string{"one.txt": "1"})
	id := e.gl.head().id

	e.pick(PullCommitInput{Commits: []string{id}})
	_, picked, err := e.picker().ListCommits(e.repo, "release", 10)
	require.NoError(t, err)
	assert.Contains(t, picked, id)

	// Dropping the local commit makes the GitLab commit pickable again.
	e.run("reset", "-q", "--hard", "HEAD~1")
	_, picked, err = e.picker().ListCommits(e.repo, "release", 10)
	require.NoError(t, err)
	assert.NotContains(t, picked, id)
	res := e.pick(PullCommitInput{Commits: []string{id}})
	assert.Len(t, res.Picked, 1)
}

// installCommitMsgHook enforces "<type>: <title> TASK-<n>" like a team hook would.
func (e *syncEnv) installCommitMsgHook() {
	e.t.Helper()
	hook := "#!/bin/sh\nhead -1 \"$1\" | grep -Eq '^(fix|feat|test|chore): .+ TASK-[0-9]+$' || { echo 'commit message must be: <type>: <msg> TASK-<n>' >&2; exit 1; }\n"
	e.write(".git/hooks/commit-msg", hook)
	require.NoError(e.t, os.Chmod(filepath.Join(e.repo, ".git", "hooks", "commit-msg"), 0o755))
}

func TestReplay_CommitFormat(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.installCommitMsgHook()
	e.gl.editAs("Alice Smith", "feat(api): add endpoint\n\nDetails.", map[string]string{"a.txt": "a1"})
	e.gl.editAs("Bob Jones", "update docs", map[string]string{"b.txt": "b1"})

	// Without a format the hook rejects the messages before anything changes.
	head := e.run("rev-parse", "HEAD")
	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo, Replay: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--type")
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))
	assert.Len(t, e.gl.commits, 3)

	res := e.sync(SyncInput{Replay: true, Format: entity.CommitFormat{Type: "fix", Task: "TASK-01"}})
	require.Len(t, res.Replayed, 2)
	assert.Equal(t, []string{"fix: add endpoint TASK-01", "fix: update docs TASK-01"},
		strings.Split(e.run("log", "-n", "2", "--reverse", "--format=%s"), "\n"))
	assert.Contains(t, e.run("log", "-1", "--format=%B", res.Replayed[0].LocalSHA), "Details.")

	// The format is remembered: the next sync reuses it, a new task overrides it.
	e.gl.editAs("Bob Jones", "more", map[string]string{"a.txt": "a2"})
	e.write("b.txt", "b-local")
	e.run("add", "-A")
	e.run("commit", "-q", "-m", "test: local change TASK-02")
	e.sync(SyncInput{Format: entity.CommitFormat{Task: "TASK-02"}})
	assert.Equal(t, "fix: sync main with myproj/release TASK-02", e.run("log", "-1", "--format=%s"))
	e.assertInSync()

	mirrors, _ := e.uc.Mirrors(e.repo)
	assert.Equal(t, entity.CommitFormat{Type: "fix", Task: "TASK-02"}, mirrors[0].CommitFormat)
}

func TestSync_HookRejectsBeforePush(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.installCommitMsgHook()
	e.gl.edit(map[string]string{"a.txt": "a-remote"})
	e.write("b.txt", "b-local")
	e.run("add", "-A")
	e.run("commit", "-q", "-m", "feat: local TASK-1")
	head := e.run("rev-parse", "HEAD")

	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))
	assert.Equal(t, "", e.run("status", "--porcelain"), "working tree restored")
	assert.Len(t, e.gl.commits, 2, "nothing pushed")

	_, err = e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo, Format: entity.CommitFormat{Type: "Bad Type"}})
	require.Error(t, err)

	e.sync(SyncInput{Format: entity.CommitFormat{Type: "chore", Task: "TASK-7"}})
	assert.Equal(t, "chore: sync main with myproj/release TASK-7", e.run("log", "-1", "--format=%s"))
	e.assertInSync()
}
