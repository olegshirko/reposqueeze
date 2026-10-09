package tui

import (
	"context"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/clipboard"
)

// runClip sends or receives the shared clipboard right away (no form).
func (m *appModel) runClip(cmd string) (tea.Model, tea.Cmd) {
	cfg := usecase.ClipConfig{KeyFile: clipKeyFile()}
	return m.startRunWithSummary(func(ctx context.Context, d deps) runResultMsg {
		uc := usecase.NewClipUseCase(d.clip, clipboard.NewClipsync(clipboard.DefaultBin()), d.log)
		var res *usecase.ClipResult
		var err error
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

func clipKeyFile() string {
	if p := os.Getenv("REPOSQUEEZE_CLIP_KEY"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clipsync", "key")
}
