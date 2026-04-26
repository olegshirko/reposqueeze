package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// logMsg carries a single log line from the use-case logger.
type logMsg struct {
	content string
}

// runnerModel shows execution progress (spinner + log viewport).
type runnerModel struct {
	viewport viewport.Model
	spinner  spinner.Model
	logCh    chan string
	resultCh chan runResultMsg
	done     bool
	result   runResultMsg
	width    int
	height   int
}

func newRunnerModel(logCh chan string, resultCh chan runResultMsg) runnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(primaryColor)

	v := viewport.New(0, 0)
	v.SetContent(helpStyle.Render("Waiting for logs..."))

	return runnerModel{
		spinner:  s,
		viewport: v,
		logCh:    logCh,
		resultCh: resultCh,
	}
}

func (m runnerModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		listenLogCmd(m.logCh),
		listenResultCmd(m.resultCh),
	)
}

func (m runnerModel) Update(msg tea.Msg) (runnerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 6
		m.viewport.Height = msg.Height - 10
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

	case logMsg:
		m.appendLog(msg.content)
		return m, listenLogCmd(m.logCh)

	case runResultMsg:
		m.done = true
		m.result = msg
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("[ERROR] %v", msg.err))
		} else {
			m.appendLog(fmt.Sprintf("[DONE] Copied %d file(s) in %s", msg.count, msg.duration))
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m runnerModel) View() string {
	var b strings.Builder
	if !m.done {
		b.WriteString(fmt.Sprintf("%s Running operation...\n\n", m.spinner.View()))
	} else if m.result.err != nil {
		b.WriteString(errorStyle.Render("✗ Operation failed") + "\n\n")
	} else {
		b.WriteString(successStyle.Render("✓ Operation completed") + "\n\n")
	}

	b.WriteString(m.viewport.View() + "\n\n")
	b.WriteString(helpStyle.Render("esc: back to menu • q: quit"))

	return lipgloss.NewStyle().Margin(1, 2).Render(b.String())
}

func (m *runnerModel) appendLog(line string) {
	content := m.viewport.View()
	if content == helpStyle.Render("Waiting for logs...") {
		content = ""
	}
	if content != "" {
		content += "\n"
	}
	content += logStyle.Render(line)
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func listenLogCmd(ch chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return logMsg{content: line}
	}
}

func listenResultCmd(ch chan runResultMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}
