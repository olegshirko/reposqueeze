package usecase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

func pushBranchMocks(t *testing.T, branches []string) (*MockGitGateway, *MockGitLabGateway) {
	gitGW := new(MockGitGateway)
	gitGW.On("GetMergeBase", "/x/proj", "master", "feature").Return("base123", nil)
	gitGW.On("GetBranchDiffFiles", "/x/proj", "base123", "feature").Return([]gateway.CommitFileInfo{
		{Status: "M", Path: "a.go"},
		{Status: "A", Path: "new.go"},
	}, nil)
	gitGW.On("GetFileContentFromCommit", "/x/proj", "feature", mock.Anything).Return([]byte("content"), nil)

	gl := new(MockGitLabGateway)
	gl.On("FindProjectByName", "proj").Return(&entity.Project{ID: 1, Name: "proj"}, nil)
	var infos []gateway.BranchInfo
	for _, b := range branches {
		infos = append(infos, gateway.BranchInfo{Name: b})
	}
	gl.On("GetBranches", 1).Return(infos, nil)
	return gitGW, gl
}

func TestPushBranch_CreatesNewGitLabBranch(t *testing.T) {
	gitGW, gl := pushBranchMocks(t, []string{"master", "develop"})
	var order []string
	// Files are looked up in the branch the new one starts from.
	gl.On("FileExists", 1, "a.go", "develop").Return(true, nil)
	gl.On("FileExists", 1, "new.go", "develop").Return(false, nil)
	gl.On("CreateRemoteBranch", mock.Anything, "1", "feature-x", "develop").
		Run(func(mock.Arguments) { order = append(order, "create") }).Return(nil)
	gl.On("CommitFilesViaAPI", "1", "feature-x", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			order = append(order, "commit")
			actions := args.Get(3).([]gateway.CommitAction)
			assert.Equal(t, "update", actions[0].Action)
			assert.Equal(t, "create", actions[1].Action)
		}).Return(gateway.CommitInfo{ID: "c1"}, nil)

	uc := NewPushBranchUseCase(gitGW, gl, newTestLogger())
	_, n, err := uc.Execute(context.Background(), PushBranchInput{
		RepoPath: "/x/proj", SourceBranch: "feature", BranchName: "feature-x", CreateFrom: "develop",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{"create", "commit"}, order)
}

func TestPushBranch_CreateFromValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		target, from, want string
	}{
		"target exists": {"develop", "master", "already exists"},
		"base missing":  {"feature-x", "release", "does not exist"},
		"bad name":      {"has space", "master", "invalid new branch name"},
	} {
		t.Run(name, func(t *testing.T) {
			gitGW, gl := pushBranchMocks(t, []string{"master", "develop"})
			uc := NewPushBranchUseCase(gitGW, gl, newTestLogger())
			_, _, err := uc.Execute(context.Background(), PushBranchInput{
				RepoPath: "/x/proj", SourceBranch: "feature", BranchName: tc.target, CreateFrom: tc.from,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			gl.AssertNotCalled(t, "CreateRemoteBranch", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			gl.AssertNotCalled(t, "CommitFilesViaAPI", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestPushBranch_NoChangesCreatesNothing(t *testing.T) {
	gitGW := new(MockGitGateway)
	gitGW.On("GetMergeBase", "/x/proj", "master", "feature").Return("base123", nil)
	gitGW.On("GetBranchDiffFiles", "/x/proj", "base123", "feature").Return([]gateway.CommitFileInfo{}, nil)
	_, gl := pushBranchMocks(t, []string{"master"})

	uc := NewPushBranchUseCase(gitGW, gl, newTestLogger())
	_, _, err := uc.Execute(context.Background(), PushBranchInput{
		RepoPath: "/x/proj", SourceBranch: "feature", BranchName: "feature-x", CreateFrom: "master",
	})
	require.Error(t, err)
	gl.AssertNotCalled(t, "CreateRemoteBranch", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
