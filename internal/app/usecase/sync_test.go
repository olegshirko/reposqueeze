package usecase

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/git"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
)

// ---------------------------------------------------------------- fake GitLab

type fakeCommit struct {
	id      string
	message string
	author  string
	date    string
	parent  string
	files   map[string]string
}

func (c *fakeCommit) info() gateway.CommitInfo {
	title, _, _ := strings.Cut(c.message, "\n")
	info := gateway.CommitInfo{ID: c.id, Title: title, Message: c.message, AuthorName: c.author,
		AuthorEmail: strings.ToLower(strings.ReplaceAll(c.author, " ", ".")) + "@example.com", AuthoredDate: c.date}
	if c.parent != "" {
		info.ParentIDs = []string{c.parent}
	}
	return info
}

// fakeGitLab is an in-memory GitLab project with one linear branch.
type fakeGitLab struct {
	project  entity.Project
	branch   string
	commits  []*fakeCommit // oldest first
	seq      int
	failPush error
	// beforeCommit runs inside CommitFilesViaAPI before the commit is applied,
	// e.g. to simulate a concurrent push.
	beforeCommit func()
}

func newFakeGitLab(name, branch string, files map[string]string) *fakeGitLab {
	f := &fakeGitLab{project: entity.Project{ID: 42, Name: name}, branch: branch}
	f.addCommit("initial", files)
	return f
}

func (f *fakeGitLab) head() *fakeCommit { return f.commits[len(f.commits)-1] }

func (f *fakeGitLab) addCommit(msg string, files map[string]string) *fakeCommit {
	f.seq++
	c := &fakeCommit{id: fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprint("fake commit ", f.seq)))), message: msg, files: files,
		author: "Web User", date: fmt.Sprintf("2024-01-%02dT10:00:00Z", f.seq)}
	if len(f.commits) > 0 {
		c.parent = f.head().id
	}
	f.commits = append(f.commits, c)
	return c
}

// edit creates a commit on GitLab as if someone pushed through the web UI.
// An empty string deletes the file.
func (f *fakeGitLab) edit(changes map[string]string) {
	f.editAs("Web User", "web edit", changes)
}

// editAs is edit with a given author and commit message.
func (f *fakeGitLab) editAs(author, msg string, changes map[string]string) {
	files := copyFiles(f.head().files)
	for p, c := range changes {
		if c == "" {
			delete(files, p)
		} else {
			files[p] = c
		}
	}
	c := f.addCommit(msg, files)
	c.author = author
}

func copyFiles(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (f *fakeGitLab) find(ref string) *fakeCommit {
	if ref == f.branch {
		return f.head()
	}
	for _, c := range f.commits {
		if c.id == ref || (len(ref) >= 7 && strings.HasPrefix(c.id, ref)) {
			return c
		}
	}
	return nil
}

func (f *fakeGitLab) CommitFilesViaAPI(projectID, branchName, msg string, actions []gateway.CommitAction) (gateway.CommitInfo, error) {
	if f.failPush != nil {
		return gateway.CommitInfo{}, f.failPush
	}
	if f.beforeCommit != nil {
		f.beforeCommit()
		f.beforeCommit = nil
	}
	files := copyFiles(f.head().files)
	for _, a := range actions {
		_, exists := files[a.FilePath]
		switch a.Action {
		case "create":
			if exists {
				return gateway.CommitInfo{}, fmt.Errorf("create %s: already exists", a.FilePath)
			}
			files[a.FilePath] = a.Content
		case "update":
			if !exists {
				return gateway.CommitInfo{}, fmt.Errorf("update %s: does not exist", a.FilePath)
			}
			files[a.FilePath] = a.Content
		case "delete":
			if !exists {
				return gateway.CommitInfo{}, fmt.Errorf("delete %s: does not exist", a.FilePath)
			}
			delete(files, a.FilePath)
		default:
			return gateway.CommitInfo{}, fmt.Errorf("unexpected action %s", a.Action)
		}
	}
	parent := f.head().id
	c := f.addCommit(msg, files)
	return gateway.CommitInfo{ID: c.id, Message: msg, ParentIDs: []string{parent}}, nil
}

func (f *fakeGitLab) FindProjectByName(name string) (*entity.Project, error) {
	if name == f.project.Name {
		p := f.project
		return &p, nil
	}
	return nil, nil
}

func (f *fakeGitLab) GetCommits(projectID int, ref string, limit int) ([]gateway.CommitInfo, error) {
	start := f.find(ref)
	if start == nil {
		return nil, fmt.Errorf("unknown ref %s", ref)
	}
	var out []gateway.CommitInfo
	for i := len(f.commits) - 1; i >= 0 && len(out) < limit; i-- {
		c := f.commits[i]
		if len(out) == 0 && c != start {
			continue
		}
		out = append(out, c.info())
	}
	return out, nil
}

func (f *fakeGitLab) ListCommitsAfter(projectID int, ref, after string, max int) ([]gateway.CommitInfo, error) {
	head := f.find(ref)
	if head == nil {
		return nil, fmt.Errorf("unknown ref %s", ref)
	}
	var out []gateway.CommitInfo
	found := false
	for _, c := range f.commits {
		if found {
			out = append(out, c.info())
		}
		if c.id == after || (len(after) >= 7 && strings.HasPrefix(c.id, after)) {
			found = true
		}
		if c == head {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("commit %s is not on %s", after, ref)
	}
	return out, nil
}

func (f *fakeGitLab) GetBranchHead(projectID int, branch string) (string, error) {
	if branch != f.branch {
		return "", fmt.Errorf("no branch %s", branch)
	}
	return f.head().id, nil
}

func (f *fakeGitLab) GetCompareDiff(projectID int, from, to string) ([]gateway.DiffEntry, error) {
	a, b := f.find(from), f.find(to)
	if a == nil || b == nil {
		return nil, fmt.Errorf("unknown refs %s..%s", from, to)
	}
	var out []gateway.DiffEntry
	for p, c := range b.files {
		old, ok := a.files[p]
		switch {
		case !ok:
			out = append(out, gateway.DiffEntry{NewPath: p, OldPath: p, NewFile: true})
		case old != c:
			out = append(out, gateway.DiffEntry{NewPath: p, OldPath: p})
		}
	}
	for p := range a.files {
		if _, ok := b.files[p]; !ok {
			out = append(out, gateway.DiffEntry{NewPath: p, OldPath: p, DeletedFile: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NewPath < out[j].NewPath })
	return out, nil
}

func (f *fakeGitLab) GetRawFile(projectID int, path, ref string) ([]byte, error) {
	c := f.find(ref)
	if c == nil {
		return nil, fmt.Errorf("unknown ref %s", ref)
	}
	content, ok := c.files[path]
	if !ok {
		return nil, fmt.Errorf("404 %s", path)
	}
	return []byte(content), nil
}

func (f *fakeGitLab) FileExists(projectID int, path, ref string) (bool, error) {
	c := f.find(ref)
	if c == nil {
		return false, fmt.Errorf("unknown ref %s", ref)
	}
	_, ok := c.files[path]
	return ok, nil
}

func (f *fakeGitLab) CreateRemoteBranch(context.Context, string, string, string) error { return nil }
func (f *fakeGitLab) DeleteProject(int) error                                          { return nil }
func (f *fakeGitLab) CreateProject(string) (*entity.Project, error)                    { return nil, nil }
func (f *fakeGitLab) DownloadRepoArchive(int, string, *bytes.Buffer) error             { return nil }
func (f *fakeGitLab) GetBranches(int) ([]gateway.BranchInfo, error)                    { return nil, nil }
func (f *fakeGitLab) GetCommitDiff(int, string) ([]gateway.DiffEntry, error)           { return nil, nil }

// ---------------------------------------------------------------- local repo helpers

type syncEnv struct {
	t    *testing.T
	repo string
	gl   *fakeGitLab
	uc   *SyncUseCase
	git  *git.OSExecGitGateway
}

func newSyncEnv(t *testing.T, files map[string]string) *syncEnv {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "myproj")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	e := &syncEnv{t: t, repo: repo}
	e.run("init", "-q", "-b", "main")
	e.run("config", "user.email", "t@example.com")
	e.run("config", "user.name", "T")
	for p, c := range files {
		e.write(p, c)
	}
	e.run("add", "-A")
	e.run("commit", "-q", "-m", "initial")

	e.gl = newFakeGitLab("myproj", "release", copyFiles(files))
	e.git = git.NewOSExecGitGateway(newTestLogger())
	e.uc = NewSyncUseCase(e.git, e.gl, state.NewFileStore(e.git), newTestLogger())
	return e
}

func (e *syncEnv) run(args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", e.repo}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(e.t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (e *syncEnv) write(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.repo, filepath.FromSlash(rel))
	require.NoError(e.t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(e.t, os.WriteFile(p, []byte(content), 0o644))
}

func (e *syncEnv) read(rel string) string {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.repo, filepath.FromSlash(rel)))
	require.NoError(e.t, err)
	return string(data)
}

func (e *syncEnv) commit(msg string) {
	e.t.Helper()
	e.run("add", "-A")
	e.run("commit", "-q", "-m", msg)
}

func (e *syncEnv) init() *entity.Mirror {
	e.t.Helper()
	m, err := e.uc.Init(context.Background(), SyncInitInput{RepoPath: e.repo, RemoteBranch: "release"})
	require.NoError(e.t, err)
	return m
}

func (e *syncEnv) sync(in SyncInput) *SyncResult {
	e.t.Helper()
	in.RepoPath = e.repo
	res, err := e.uc.Sync(context.Background(), in)
	require.NoError(e.t, err)
	return res
}

// localFiles returns the committed tree of HEAD.
func (e *syncEnv) localFiles() map[string]string {
	e.t.Helper()
	out := map[string]string{}
	for _, p := range strings.Split(e.run("ls-tree", "-r", "--name-only", "HEAD"), "\n") {
		if p != "" {
			out[p] = e.run("show", "HEAD:"+p)
		}
	}
	return out
}

func (e *syncEnv) assertInSync() {
	e.t.Helper()
	remote := map[string]string{}
	for p, c := range e.gl.head().files {
		remote[p] = strings.TrimSpace(c)
	}
	assert.Equal(e.t, remote, e.localFiles())
}

// ---------------------------------------------------------------- tests

func TestSync_InitRecordsOrigin(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	m := e.init()

	assert.Equal(t, "main->myproj:release", m.Name)
	assert.Equal(t, e.run("rev-parse", "HEAD"), m.Origin.LocalSHA)
	assert.Equal(t, e.gl.head().id, m.Origin.RemoteSHA)

	_, err := e.uc.Init(context.Background(), SyncInitInput{RepoPath: e.repo, RemoteBranch: "release"})
	require.Error(t, err, "second init without --force must fail")

	// Start mirroring from explicit commits (short remote SHA is resolved).
	e.gl.edit(map[string]string{"a.txt": "a2"})
	m, err = e.uc.Init(context.Background(), SyncInitInput{
		RepoPath: e.repo, RemoteBranch: "release", Force: true,
		LocalSHA: "HEAD", RemoteSHA: e.gl.commits[0].id[:10],
	})
	require.NoError(t, err)
	assert.Equal(t, e.gl.commits[0].id, m.Origin.RemoteSHA)
}

func TestSync_PullOnly(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "gone.txt": "x"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote", "new/b.txt": "b", "gone.txt": ""})

	res := e.sync(SyncInput{})
	assert.Equal(t, 2, res.Pulled)
	assert.Equal(t, 0, res.Pushed)
	assert.Empty(t, res.RemoteCommit)
	e.assertInSync()

	// The local sync commit carries the GitLab SHA as a trailer.
	assert.Contains(t, e.run("log", "-1", "--format=%B"), TrailerRemote+": myproj/release@"+e.gl.head().id)
}

func TestSync_PushOnlyAndJournal(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "gone.txt": "x"})
	e.init()
	localStart := e.run("rev-parse", "HEAD")
	e.write("a.txt", "a-local")
	e.write("c.txt", "c")
	require.NoError(t, os.Remove(filepath.Join(e.repo, "gone.txt")))
	e.commit("local work")

	res := e.sync(SyncInput{})
	assert.Equal(t, 3, res.Pushed)
	e.assertInSync()
	assert.Contains(t, e.gl.head().message, TrailerSource+": main@")

	mirrors, err := e.uc.Mirrors(e.repo)
	require.NoError(t, err)
	require.Len(t, mirrors[0].Journal, 1)
	j := mirrors[0].Journal[0]
	assert.Equal(t, entity.SyncPush, j.Direction)
	assert.Equal(t, localStart, j.LocalFrom)
	assert.Equal(t, e.gl.head().id, j.RemoteSHA)
	assert.Equal(t, e.run("rev-parse", "HEAD"), j.LocalSHA)

	// Nothing changed since: the next sync is a no-op.
	res = e.sync(SyncInput{})
	assert.True(t, res.Plan.Empty())
	assert.Len(t, e.gl.commits, 2)
}

func TestSync_BothDirectionsDifferentFiles(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote"})
	e.write("b.txt", "b-local")
	e.commit("local")

	res := e.sync(SyncInput{})
	assert.Equal(t, 1, res.Pulled)
	assert.Equal(t, 1, res.Pushed)
	e.assertInSync()

	// Repeated rounds keep following the latest pair.
	e.gl.edit(map[string]string{"b.txt": "b-remote-2"})
	e.write("a.txt", "a-local-2")
	e.commit("local 2")
	e.sync(SyncInput{})
	e.assertInSync()
	assert.Equal(t, "b-remote-2", e.read("b.txt"))
	assert.Equal(t, "a-local-2", e.gl.head().files["a.txt"])
}

func TestSync_MergeCleanly(t *testing.T) {
	base := "line1\nline2\nline3\nline4\nline5\n"
	e := newSyncEnv(t, map[string]string{"f.txt": base})
	e.init()
	e.gl.edit(map[string]string{"f.txt": strings.Replace(base, "line5", "line5-remote", 1)})
	e.write("f.txt", strings.Replace(base, "line1", "line1-local", 1))
	e.commit("local edit")

	res := e.sync(SyncInput{})
	assert.Equal(t, []string{"f.txt"}, res.Merged)
	assert.Empty(t, res.Conflicts)
	want := "line1-local\nline2\nline3\nline4\nline5-remote\n"
	assert.Equal(t, want, e.read("f.txt"))
	assert.Equal(t, want, e.gl.head().files["f.txt"])
}

func TestSync_MergeConflictThenResolve(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"f.txt": "same\n", "other.txt": "o"})
	e.init()
	e.gl.edit(map[string]string{"f.txt": "remote\n", "other.txt": "o-remote"})
	e.write("f.txt", "local\n")
	e.commit("local edit")

	res := e.sync(SyncInput{})
	assert.Equal(t, []string{"f.txt"}, res.Conflicts)
	assert.Equal(t, 1, res.Pulled, "non-conflicting files are still pulled")
	assert.Equal(t, "remote\n", e.gl.head().files["f.txt"], "conflicting file is not pushed")
	assert.Contains(t, e.read("f.txt"), "<<<<<<< local main")
	assert.Contains(t, e.read("f.txt"), ">>>>>>> gitlab release")

	// Syncing again before resolving is refused.
	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve conflicts")

	// Resolve, commit, sync: the resolution goes to GitLab.
	e.write("f.txt", "resolved\n")
	e.commit("resolve")
	res = e.sync(SyncInput{})
	assert.Empty(t, res.Conflicts)
	assert.Equal(t, "resolved\n", e.gl.head().files["f.txt"])
	e.assertInSync()

	mirrors, _ := e.uc.Mirrors(e.repo)
	assert.Nil(t, mirrors[0].PendingMerge)
}

func TestSync_ConflictKeptAsIsIsStillPushed(t *testing.T) {
	// Changed locally, deleted on GitLab: local file is kept; if the user
	// commits nothing, the next sync re-creates it on GitLab.
	e := newSyncEnv(t, map[string]string{"f.txt": "x"})
	e.init()
	e.gl.edit(map[string]string{"f.txt": ""})
	e.write("f.txt", "local")
	e.commit("local")

	res := e.sync(SyncInput{})
	assert.Equal(t, []string{"f.txt"}, res.Conflicts)
	_, onRemote := e.gl.head().files["f.txt"]
	assert.False(t, onRemote)

	e.sync(SyncInput{})
	assert.Equal(t, "local", e.gl.head().files["f.txt"])
	e.assertInSync()
}

func TestSync_Strategies(t *testing.T) {
	for _, tc := range []struct {
		strategy string
		want     string
	}{
		{StrategyLocal, "local"},
		{StrategyRemote, "remote"},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			e := newSyncEnv(t, map[string]string{"f.txt": "base"})
			e.init()
			e.gl.edit(map[string]string{"f.txt": "remote"})
			e.write("f.txt", "local")
			e.commit("local")

			e.sync(SyncInput{Strategy: tc.strategy})
			assert.Equal(t, tc.want, e.read("f.txt"))
			assert.Equal(t, tc.want, e.gl.head().files["f.txt"])
			e.assertInSync()
		})
	}

	t.Run("abort", func(t *testing.T) {
		e := newSyncEnv(t, map[string]string{"f.txt": "base"})
		e.init()
		e.gl.edit(map[string]string{"f.txt": "remote"})
		e.write("f.txt", "local")
		e.commit("local")
		before := e.run("rev-parse", "HEAD")

		_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo, Strategy: StrategyAbort})
		require.Error(t, err)
		assert.Equal(t, before, e.run("rev-parse", "HEAD"))
		assert.Len(t, e.gl.commits, 2)
	})
}

func TestSync_BinaryConflictShowsGitLabVersion(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"img.bin": "a\x00b"})
	e.init()
	e.gl.edit(map[string]string{"img.bin": "remote\x00"})
	e.write("img.bin", "local\x00")
	e.commit("local")

	res := e.sync(SyncInput{})
	assert.Equal(t, []string{"img.bin"}, res.Conflicts)
	assert.Equal(t, "remote\x00", e.read("img.bin"), "GitLab version is left in the working tree")
	assert.Equal(t, "local\x00", e.run("show", "HEAD:img.bin"))
}

func TestSync_DryRunChangesNothing(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote"})
	e.write("b.txt", "b-local")
	e.commit("local")
	head := e.run("rev-parse", "HEAD")

	res := e.sync(SyncInput{DryRun: true})
	assert.Equal(t, []FileChange{{Path: "a.txt", Kind: ChangeModified}}, res.Plan.RemoteChanges)
	assert.Equal(t, []FileChange{{Path: "b.txt", Kind: ChangeModified}}, res.Plan.LocalChanges)
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))
	assert.Len(t, e.gl.commits, 2)
}

func TestSync_DirtyTreeAndAutostash(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote"})
	e.write("b.txt", "wip")

	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--autostash")

	res := e.sync(SyncInput{Autostash: true})
	assert.Empty(t, res.Warnings)
	assert.Equal(t, "a-remote", e.read("a.txt"))
	assert.Equal(t, "wip", e.read("b.txt"), "uncommitted work is restored")
	assert.Equal(t, "b", e.gl.head().files["b.txt"], "uncommitted work is not pushed")
}

func TestSync_PushFailureRestoresWorkingTree(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote", "new.txt": "n"})
	e.write("b.txt", "b-local")
	e.commit("local")
	head := e.run("rev-parse", "HEAD")
	e.gl.failPush = fmt.Errorf("boom")

	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Equal(t, head, e.run("rev-parse", "HEAD"))
	assert.Equal(t, "", e.run("status", "--porcelain"))

	// After the outage the same sync succeeds.
	e.gl.failPush = nil
	e.sync(SyncInput{})
	e.assertInSync()
}

func TestSync_RemoteMovedDuringPush(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.init()
	e.write("a.txt", "a-local")
	e.commit("local")
	e.gl.beforeCommit = func() { e.gl.edit(map[string]string{"b.txt": "b-concurrent"}) }

	res := e.sync(SyncInput{})
	assert.True(t, res.RemoteMoved)
	assert.NotEmpty(t, res.Warnings)

	// The concurrent change is picked up by the next sync.
	e.sync(SyncInput{})
	assert.Equal(t, "b-concurrent", e.read("b.txt"))
	e.assertInSync()
}

func TestSync_WrongBranchAndRecover(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote"})
	e.sync(SyncInput{})
	syncCommit := e.run("rev-parse", "HEAD")

	e.run("checkout", "-q", "-b", "other")
	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo, Mirror: "main->myproj:release"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checked out")
	e.run("checkout", "-q", "main")

	// Lose the state file and rebuild it from the commit trailer.
	gitDir := e.run("rev-parse", "--absolute-git-dir")
	require.NoError(t, os.RemoveAll(filepath.Join(gitDir, "reposqueeze")))
	m, err := e.uc.Init(context.Background(), SyncInitInput{RepoPath: e.repo, RemoteBranch: "release", Recover: true})
	require.NoError(t, err)
	assert.Equal(t, syncCommit, m.Origin.LocalSHA)
	assert.Equal(t, e.gl.head().id, m.Origin.RemoteSHA)

	plan, err := e.uc.Plan(context.Background(), SyncInput{RepoPath: e.repo})
	require.NoError(t, err)
	assert.True(t, plan.Empty())
}

func TestSync_MultipleMirrorsNeedName(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	e.init()
	_, err := e.uc.Init(context.Background(), SyncInitInput{RepoPath: e.repo, RemoteBranch: "release", Name: "second"})
	require.NoError(t, err)

	_, err = e.uc.Plan(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--mirror")

	_, err = e.uc.Plan(context.Background(), SyncInput{RepoPath: e.repo, Mirror: "second"})
	require.NoError(t, err)
}

func TestSplitChanges(t *testing.T) {
	local := []FileChange{{"a", ChangeModified}, {"both", ChangeModified}, {"gone", ChangeDeleted}}
	remote := []FileChange{{"b", ChangeAdded}, {"both", ChangeDeleted}, {"gone", ChangeDeleted}}
	l, r, c := splitChanges(local, remote)
	assert.Equal(t, []FileChange{{"a", ChangeModified}}, l)
	assert.Equal(t, []FileChange{{"b", ChangeAdded}}, r)
	assert.Equal(t, []Conflict{{Path: "both", Local: ChangeModified, Remote: ChangeDeleted}}, c)

	assert.Equal(t, []FileChange{{"old", ChangeDeleted}, {"new", ChangeAdded}},
		remoteChangesFromDiff([]gateway.DiffEntry{{OldPath: "old", NewPath: "new", RenamedFile: true}}))
}

func TestSync_UntrackedFilesDoNotBlock(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a"})
	e.init()
	e.gl.edit(map[string]string{"a.txt": "a-remote", "new.txt": "from gitlab"})
	e.write(".idea/workspace.xml", "ide")
	e.write("bin/app", "binary")

	// Unrelated untracked files are fine.
	plan := e.sync(SyncInput{DryRun: true}).Plan
	require.Len(t, plan.RemoteChanges, 2)

	// An untracked file where GitLab adds one is reported by name and kept.
	e.write("new.txt", "mine")
	_, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "untracked local files would be overwritten: new.txt")
	assert.Equal(t, "mine", e.read("new.txt"))
	require.NoError(t, os.Remove(filepath.Join(e.repo, "new.txt")))

	e.sync(SyncInput{})
	assert.Equal(t, "a-remote", e.read("a.txt"))
	assert.Equal(t, "ide", e.read(".idea/workspace.xml"), "untracked files are left alone")

	// A modified tracked file is named in the error.
	e.write("a.txt", "dirty")
	_, err = e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted changes in a.txt")
}
