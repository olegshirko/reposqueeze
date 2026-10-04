package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

func planningApp() *appModel {
	m := NewApp(nil, &mockGitLabGateway{}, "", "", nil).(*appModel)
	m.state = statePlanning
	m.pendingSync = &usecase.SyncInput{RepoPath: "/r", Strategy: "merge"}
	return m
}

func TestSyncPlan_NonEmptyAsksForConfirmation(t *testing.T) {
	m := planningApp()
	plan := &usecase.SyncPlan{
		Mirror:        entity.Mirror{Name: "main->p:release"},
		RemoteChanges: []usecase.FileChange{{Path: "some_file_name.go", Kind: usecase.ChangeModified}},
		Conflicts:     []usecase.Conflict{{Path: "both.txt", Local: "modified", Remote: "modified"}},
	}
	m.Update(syncPlanMsg{plan: plan})

	require.Equal(t, stateForm, m.state)
	require.NotNil(t, m.form)
	assert.Equal(t, cmdSyncConfirm, m.form.cmd)
	assert.Contains(t, m.form.header, "some_file_name.go", "paths are shown verbatim")
	assert.Contains(t, m.form.header, "both.txt")
	assert.NotNil(t, m.pendingSync, "input is kept for the confirm step")
}

func TestSyncPlan_EmptyOrErrorGoesToResult(t *testing.T) {
	m := planningApp()
	m.Update(syncPlanMsg{plan: &usecase.SyncPlan{Mirror: entity.Mirror{Name: "x"}}})
	assert.Equal(t, stateRunning, m.state)
	assert.Nil(t, m.pendingSync)

	m = planningApp()
	m.Update(syncPlanMsg{err: errors.New("no mirror")})
	assert.Equal(t, stateRunning, m.state)
}

func TestSyncPlan_IgnoredAfterEsc(t *testing.T) {
	m := planningApp()
	m.Update(backMsg{})
	assert.Equal(t, stateMenu, m.state)

	m.Update(syncPlanMsg{plan: &usecase.SyncPlan{RemoteChanges: []usecase.FileChange{{Path: "a"}}}})
	assert.Equal(t, stateMenu, m.state)
	assert.Nil(t, m.form)
}
