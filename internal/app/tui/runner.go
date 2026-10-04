package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runEventMsg wraps an event from a specific run so stale runs can be ignored.
type runEventMsg struct {
	runID int
	event runEvent
	ok    bool // false when the event channel is closed
}

// runnerModel shows execution progress (spinner + log viewport).
type runnerModel struct {
	runID    int
	viewport viewport.Model
	spinner  spinner.Model
	log      *TUILogger
	cancel   context.CancelFunc
	lines    []string
	done     bool
	result   runResultMsg
	width    int
	height   int
}

func newRunnerModel(runID int, log *TUILogger, cancel context.CancelFunc, width, height int) runnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(primaryColor)

	m := runnerModel{
		runID:    runID,
		spinner:  s,
		viewport: viewport.New(0, 0),
		log:      log,
		cancel:   cancel,
	}
	m.resize(width, height)
	m.viewport.SetContent(helpStyle.Render("Waiting for logs..."))
	return m
}

func (m runnerModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.listen())
}

// stop cancels the operation and detaches the UI from its logger.
func (m *runnerModel) stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.log != nil {
		m.log.Stop()
	}
}

func (m *runnerModel) resize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = max(width-6, 10)
	m.viewport.Height = max(height-8, 3)
}

func (m runnerModel) Update(msg tea.Msg) (runnerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			return m, func() tea.Msg { return backMsg{} }
		}

	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case runEventMsg:
		if msg.runID != m.runID || !msg.ok {
			return m, nil
		}
		if msg.event.result != nil {
			m.finish(*msg.event.result)
			return m, m.listen()
		}
		m.appendLog(msg.event.line)
		return m, m.listen()
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *runnerModel) finish(res runResultMsg) {
	m.done = true
	m.result = res
	switch {
	case res.err != nil:
		m.appendLog(errorStyle.Render(fmt.Sprintf("[ERROR] %v", res.err)))
	case res.summary != "":
		m.appendLog(successStyle.Render("[DONE] " + res.summary))
	default:
		m.appendLog(successStyle.Render(fmt.Sprintf("[DONE] %d file(s) in %s", res.count, res.duration)))
	}
}

func (m runnerModel) View() string {
	var b strings.Builder
	switch {
	case !m.done:
		b.WriteString(fmt.Sprintf("%s Running operation...\n\n", m.spinner.View()))
	case m.result.err != nil:
		b.WriteString(errorStyle.Render("✗ Operation failed") + "\n\n")
	default:
		b.WriteString(successStyle.Render("✓ Operation completed") + "\n\n")
	}

	b.WriteString(m.viewport.View() + "\n\n")
	help := "↑/↓/pgup/pgdn: scroll • esc: back to menu • q: quit"
	if !m.done {
		help = "↑/↓/pgup/pgdn: scroll • esc: cancel and back to menu"
	}
	b.WriteString(helpStyle.Render(help))

	return lipgloss.NewStyle().Margin(1, 2).Render(b.String())
}

func (m *runnerModel) appendLog(line string) {
	m.lines = append(m.lines, logStyle.Render(line))
	atBottom := m.viewport.AtBottom() || len(m.lines) == 1
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m runnerModel) listen() tea.Cmd {
	ch := m.log.Events()
	id := m.runID
	return func() tea.Msg {
		ev, ok := <-ch
		return runEventMsg{runID: id, event: ev, ok: ok}
	}
}
