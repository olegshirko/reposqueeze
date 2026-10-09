package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/olegshirko/reposqueeze/internal/app/clipsetup"
	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/clipboard"
)

// runClip sends or receives the shared clipboard right away (no form).
func (m *appModel) runClip(cmd string) (tea.Model, tea.Cmd) {
	cfg := usecase.ClipConfig{KeyFile: clipsetup.KeyFile()}
	baseURL := m.gitlabBaseURL
	return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
		store, _, err := clipsetup.Store(ctx, clipsetup.Options{BaseURL: baseURL, API: d.clip})
		if err != nil {
			return runResultMsg{err: err}
		}
		uc := usecase.NewClipUseCase(store, clipboard.NewClipsync(clipboard.DefaultBin()), d.log)
		var res *usecase.ClipResult
		if cmd == cmdClipPush {
			res, err = uc.Push(ctx, cfg)
		} else {
			res, err = uc.Pull(ctx, cfg)
		}
		if err != nil {
			return runResultMsg{err: err}
		}
		if cmd == cmdClipPush {
			return runResultMsg{summary: "sent: " + res.Summary}
		}
		return runResultMsg{summary: "received: " + res.Describe(time.Now())}
	})
}
