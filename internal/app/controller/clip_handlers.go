package controller

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/notify"
)

// SetClipUseCase enables the clip command.
func (c *CLIController) SetClipUseCase(uc *usecase.ClipUseCase) {
	c.clipUseCase = uc
}

func defaultClipKey() string {
	if p := os.Getenv("REPOSQUEEZE_CLIP_KEY"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clipsync", "key")
}

func (c *CLIController) handleClip(args []string) error {
	if len(args) == 0 {
		c.printClipUsage()
		return errUsage
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("clip "+sub, flag.ExitOnError)
	setFlagSetUsage(fs)
	project := fs.String("project", usecase.DefaultClipProject, "Private GitLab project that holds the shared clipboard")
	key := fs.String("key", defaultClipKey(), "Shared key file, the same on both Macs (env REPOSQUEEZE_CLIP_KEY)")
	quiet := fs.Bool("quiet", false, "No macOS notifications")
	push := fs.Bool("push", true, "watch: push when the same thing is copied twice (⌘C ⌘C)")
	pull := fs.Bool("pull", false, "watch: put clipboards pushed from the other Mac into this one automatically")
	window := fs.Duration("window", time.Second, "watch: max time between the two ⌘C")
	interval := fs.Duration("interval", 5*time.Second, "watch: how often to check GitLab for a new clipboard")
	fs.Parse(rest)

	cfg := usecase.ClipConfig{Project: *project, KeyFile: *key}
	tell := func(text string) {
		fmt.Println(text)
		if !*quiet {
			notify.Show("Clipboard", text)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch sub {
	case "push":
		res, err := c.clipUseCase.Push(ctx, cfg)
		if err != nil {
			tell("not sent: " + err.Error())
			return err
		}
		tell("sent: " + res.Summary)
		return nil

	case "pull":
		res, err := c.clipUseCase.Pull(ctx, cfg)
		if err != nil {
			tell("not received: " + err.Error())
			return err
		}
		tell("received: " + res.Describe(time.Now()))
		return nil

	case "watch":
		what := ""
		if *push {
			what += "push on ⌘C ⌘C"
		}
		if *pull {
			if what != "" {
				what += ", "
			}
			what += fmt.Sprintf("pull every %s", *interval)
		}
		c.logger.Infof("Watching the clipboard (%s); Ctrl+C to stop", what)
		return c.clipUseCase.Watch(ctx, cfg, usecase.ClipWatchOptions{
			Push: *push, Window: *window, Pull: *pull, Interval: *interval,
			OnEvent: func(kind string, res *usecase.ClipResult, err error) {
				switch {
				case err != nil && kind == "push":
					tell("not sent: " + err.Error())
				case err != nil:
					tell("not received: " + err.Error())
				case kind == "push":
					tell("sent: " + res.Summary)
				default:
					tell("received: " + res.Describe(time.Now()))
				}
			},
		})
	}
	c.printClipUsage()
	return errUsage
}

func (c *CLIController) printClipUsage() {
	fmt.Println("Usage: reposqueeze clip push | pull | watch [options]")
	fmt.Println("  push    send this Mac's clipboard (text, images, files) to GitLab, encrypted")
	fmt.Println("  pull    replace this Mac's clipboard with the last one sent")
	fmt.Println("  watch   run in the background: send when you press ⌘C twice; with --pull also receive automatically")
	fmt.Println("Options: --project clipboard --key ~/.clipsync/key --quiet --push=false --pull --window 1s --interval 5s")
}
