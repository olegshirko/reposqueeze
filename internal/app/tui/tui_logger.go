package tui

import (
	"fmt"
	"sync"

	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// TUILogger implements logger.Logger and forwards every log line to a channel.
// It is safe for concurrent use.
type TUILogger struct {
	mu     sync.Mutex
	ch     chan string
	closed bool
}

// NewTUILogger creates a logger that publishes formatted log lines on ch.
func NewTUILogger(ch chan string) *TUILogger {
	return &TUILogger{ch: ch}
}

func (l *TUILogger) send(level string, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	select {
	case l.ch <- fmt.Sprintf("[%s] %s", level, msg):
	default:
	}
}

func (l *TUILogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	close(l.ch)
}

func (l *TUILogger) Info(args ...interface{})  { l.send("INFO", fmt.Sprint(args...)) }
func (l *TUILogger) Infof(format string, args ...interface{}) {
	l.send("INFO", fmt.Sprintf(format, args...))
}
func (l *TUILogger) Warn(args ...interface{})  { l.send("WARN", fmt.Sprint(args...)) }
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
