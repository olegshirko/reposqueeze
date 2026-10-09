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
	baseURL, token := m.gitlabBaseURL, m.gitlabToken
	return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
		o := clipsetup.Options{BaseURL: baseURL, API: d.clip, Warnf: d.log.Warnf}
		if token != "" {
			o.Token = token
			if u, ok := d.clip.(interface{ CurrentUser() (string, error) }); ok {
				o.CurrentUser = u.CurrentUser
			}
		}
		store, _, err := clipsetup.Store(ctx, o)
		if err != nil {
			return runResultMsg{err: err}
		}
		uc := usecase.NewClipUseCase(store, clipboard.Default(), d.log)
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
