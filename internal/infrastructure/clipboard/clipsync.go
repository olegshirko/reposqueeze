// Package clipboard talks to the clipsync helper (tools/clipsync), which
// packs and restores the macOS pasteboard with all its types and files.
package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// Clipsync runs the clipsync binary.
type Clipsync struct {
	Bin string
}

var _ gateway.Clipboard = (*Clipsync)(nil)

// DefaultBin is $REPOSQUEEZE_CLIPSYNC or ~/.clipsync/bin/clipsync.
func DefaultBin() string {
	if p := os.Getenv("REPOSQUEEZE_CLIPSYNC"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clipsync", "bin", "clipsync")
}

// NewClipsync uses the given binary path.
func NewClipsync(bin string) *Clipsync {
	return &Clipsync{Bin: bin}
}

func (c *Clipsync) check() error {
	if _, err := os.Stat(c.Bin); err != nil {
		return fmt.Errorf("clipsync helper not found at %s; build it with `make clipsync` in the reposqueeze repo "+
			"(needs Xcode command line tools) or set REPOSQUEEZE_CLIPSYNC", c.Bin)
	}
	return nil
}

// run executes clipsync; its report goes to stderr as "clipsync: ...".
func (c *Clipsync) run(arg string, stdin []byte) ([]byte, string, error) {
	if err := c.check(); err != nil {
		return nil, "", err
	}
	cmd := exec.Command(c.Bin, arg)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	report := strings.TrimSpace(strings.ReplaceAll(stderr.String(), "clipsync: ", ""))
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, "", fmt.Errorf("clipsync %s: %s", arg, report)
	}
	if err != nil {
		return nil, "", err
	}
	return stdout.Bytes(), report, nil
}

// Export packs the current clipboard.
func (c *Clipsync) Export() ([]byte, string, error) {
	return c.run("export", nil)
}

// Import restores a packed clipboard.
func (c *Clipsync) Import(payload []byte) (string, error) {
	_, report, err := c.run("import", payload)
	return report, err
}

// WatchDoubleCopy runs `clipsync watch` and calls onDouble for each "double" line.
func (c *Clipsync) WatchDoubleCopy(ctx context.Context, window time.Duration, onDouble func()) error {
	if err := c.check(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.Bin, "watch", strconv.FormatFloat(window.Seconds(), 'f', 2, 64))
	// Stop the whole process group, so no child keeps the output pipe open.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// Whatever survives the kill, Wait closes the pipe after this delay.
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	lines := make(chan struct{})
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "double" {
				onDouble()
			}
		}
	}()
	select {
	case <-lines: // the helper exited by itself
	case <-ctx.Done():
	}
	err = cmd.Wait() // kills the group if ctx is done, then closes the pipe
	<-lines
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("clipsync watch stopped: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return errors.New("clipsync watch stopped")
}
