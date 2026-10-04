package tui

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTUILogger_OrderAndClose(t *testing.T) {
	l := NewTUILogger()
	go func() {
		for i := 0; i < 2000; i++ { // more than the buffer: nothing may be dropped
			l.Infof("line %d", i)
		}
		l.Finish(runResultMsg{count: 7})
	}()

	n := 0
	var res *runResultMsg
	for ev := range l.Events() {
		if ev.result != nil {
			res = ev.result
			continue
		}
		require.Nil(t, res, "result must be the last event")
		assert.Equal(t, fmt.Sprintf("[INFO] line %d", n), ev.line)
		n++
	}
	assert.Equal(t, 2000, n)
	require.NotNil(t, res)
	assert.Equal(t, 7, res.count)
}

func TestTUILogger_StopUnblocksWorker(t *testing.T) {
	l := NewTUILogger()
	l.Stop()
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			l.Info("x")
		}
		l.Finish(runResultMsg{})
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("worker blocked after Stop")
	}
}

func drain(t *testing.T, m *appModel, cmd tea.Cmd) {
	t.Helper()
	// Feed run events back into the model until the run reports a result.
	for i := 0; i < 100 && m.runner != nil && !m.runner.done; i++ {
		msg := m.runner.listen()()
		_, cmd = m.Update(msg)
	}
	_ = cmd
}

func TestAppModel_RunKeepsAllLogLines(t *testing.T) {
	m := NewApp(nil, nil, "", "", nil).(*appModel)
	m.width, m.height = 80, 10 // viewport much smaller than the log

	_, cmd := m.startRun(func(ctx context.Context, d deps) (time.Duration, int, error) {
		for i := 0; i < 50; i++ {
			d.log.Infof("line %d", i)
		}
		return time.Second, 3, nil
	})
	drain(t, m, cmd)

	require.NotNil(t, m.runner)
	assert.Equal(t, stateResult, m.state)
	assert.Len(t, m.runner.lines, 51) // 50 lines + [DONE]
	assert.Contains(t, m.runner.lines[0], "line 0")
	assert.Contains(t, m.runner.lines[49], "line 49")
}

func TestAppModel_EscDuringRunIgnoresLateEvents(t *testing.T) {
	m := NewApp(nil, nil, "", "", nil).(*appModel)
	release := make(chan struct{})
	cancelled := make(chan struct{})

	m.startRun(func(ctx context.Context, d deps) (time.Duration, int, error) {
		<-ctx.Done()
		close(cancelled)
		<-release
		d.log.Info("late line")
		return 0, 0, errors.New("late result")
	})
	staleListen := m.runner.listen()

	m.Update(backMsg{})
	assert.Equal(t, stateMenu, m.state)
	assert.Nil(t, m.runner)

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled on esc")
	}
	close(release)

	// A late event from the abandoned run must not change the screen.
	done := make(chan tea.Msg, 1)
	go func() { done <- staleListen() }()
	select {
	case msg := <-done:
		m.Update(msg)
	case <-time.After(time.Second):
		// Stop() drops the late events entirely, which is also fine.
	}
	assert.Equal(t, stateMenu, m.state)
}
