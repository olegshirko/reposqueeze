package tui

import (
	"fmt"
	"sync"

	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// runEvent is a single item streamed from a running operation to the UI:
// either a log line or, as the very last event, the operation result.
type runEvent struct {
	line   string
	result *runResultMsg
}

// TUILogger implements logger.Logger and streams every log line, followed by
// the final result, over one channel so the UI sees them in order.
//
// Lines are never dropped while the UI is listening. Once the UI stops
// listening (Stop), sends return immediately so the worker never blocks.
type TUILogger struct {
	ch        chan runEvent
	done      chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
}

// NewTUILogger creates a logger with its own event channel.
func NewTUILogger() *TUILogger {
	return &TUILogger{
		ch:   make(chan runEvent, 1000),
		done: make(chan struct{}),
	}
}

// Events returns the channel the UI reads from. It is closed after Finish.
func (l *TUILogger) Events() <-chan runEvent {
	return l.ch
}

func (l *TUILogger) emit(ev runEvent) {
	select {
	case <-l.done:
		return
	default:
	}
	select {
	case l.ch <- ev:
	case <-l.done:
	}
}

func (l *TUILogger) send(level string, msg string) {
	l.emit(runEvent{line: fmt.Sprintf("[%s] %s", level, msg)})
}

// Finish publishes the result and closes the event channel. It must be called
// exactly once, from the goroutine that produced all log lines.
func (l *TUILogger) Finish(result runResultMsg) {
	l.emit(runEvent{result: &result})
	l.closeOnce.Do(func() { close(l.ch) })
}

// Stop tells the logger that nobody listens anymore (user left the screen).
func (l *TUILogger) Stop() {
	l.stopOnce.Do(func() { close(l.done) })
}

func (l *TUILogger) Info(args ...interface{}) { l.send("INFO", fmt.Sprint(args...)) }
func (l *TUILogger) Infof(format string, args ...interface{}) {
	l.send("INFO", fmt.Sprintf(format, args...))
}
func (l *TUILogger) Warn(args ...interface{}) { l.send("WARN", fmt.Sprint(args...)) }
func (l *TUILogger) Warnf(format string, args ...interface{}) {
	l.send("WARN", fmt.Sprintf(format, args...))
}
func (l *TUILogger) Error(args ...interface{}) { l.send("ERROR", fmt.Sprint(args...)) }
func (l *TUILogger) Errorf(format string, args ...interface{}) {
	l.send("ERROR", fmt.Sprintf(format, args...))
}
func (l *TUILogger) Fatal(args ...interface{}) { l.send("FATAL", fmt.Sprint(args...)) }
func (l *TUILogger) Fatalf(format string, args ...interface{}) {
	l.send("FATAL", fmt.Sprintf(format, args...))
}
func (l *TUILogger) Debug(args ...interface{}) { l.send("DEBUG", fmt.Sprint(args...)) }
func (l *TUILogger) Debugf(format string, args ...interface{}) {
	l.send("DEBUG", fmt.Sprintf(format, args...))
}

// Compile-time interface check.
var _ logger.Logger = (*TUILogger)(nil)
