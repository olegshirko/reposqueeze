package controller

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/olegshirko/reposqueeze/internal/app/clipsetup"
	"github.com/olegshirko/reposqueeze/internal/app/usecase"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/notify"
)

// ClipDeps builds what the clip command needs, once options are known.
type ClipDeps func(ctx context.Context, o clipsetup.Options) (*usecase.ClipUseCase, string, error)

// SetClipDeps enables the clip command.
func (c *CLIController) SetClipDeps(deps ClipDeps) {
	c.clipDeps = deps
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
	key := fs.String("key", clipsetup.KeyFile(), "Shared key file, the same on both Macs (env REPOSQUEEZE_CLIP_KEY)")
	transport := fs.String("transport", clipsetup.TransportSSH, "ssh: git over your SSH key, no token; api: GitLab package registry, needs GITLAB_TOKEN")
	remote := fs.String("remote", "", "ssh: git remote to use (default: git@<gitlab host>:<your user>/<project>.git, detected)")
	quiet := fs.Bool("quiet", false, "No macOS notifications")
	push := fs.Bool("push", true, "watch: push when the same thing is copied twice (⌘C ⌘C)")
	pull := fs.Bool("pull", false, "watch: put clipboards pushed from the other Mac into this one automatically")
	window := fs.Duration("window", time.Second, "watch: max time between the two ⌘C")
	interval := fs.Duration("interval", 5*time.Second, "watch: how often to check GitLab for a new clipboard")
	fs.Parse(rest)

	cfg := usecase.ClipConfig{KeyFile: *key}
	tell := func(text string) {
		fmt.Println(text)
		if !*quiet {
			notify.Show("Clipboard", text)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	uc, where, err := c.clipDeps(ctx, clipsetup.Options{Transport: *transport, Project: *project, Remote: *remote})
	if err != nil {
		tell("clipboard store unavailable: " + err.Error())
		return err
	}

	took := func(start time.Time) string {
		return fmt.Sprintf(" (%.1f s)", time.Since(start).Seconds())
	}
	switch sub {
	case "push":
		start := time.Now()
		res, err := uc.Push(ctx, cfg)
		if err != nil {
			tell("not sent: " + err.Error())
			return err
		}
		tell("sent: " + res.Summary + took(start))
		return nil

	case "pull":
		start := time.Now()
		res, err := uc.Pull(ctx, cfg)
		if err != nil {
			tell("not received: " + err.Error())
			return err
		}
		tell("received: " + res.Describe(time.Now()) + took(start))
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
		c.logger.Infof("Watching the clipboard (%s) via %s; Ctrl+C to stop", what, where)
		return uc.Watch(ctx, cfg, usecase.ClipWatchOptions{
			Push: *push, Window: *window, Pull: *pull, Interval: *interval,
			OnEvent: func(kind string, res *usecase.ClipResult, err error) {
				switch {
				case err != nil && kind == "push":
					tell("not sent: " + err.Error())
				case err != nil:
					tell("not received: " + err.Error())
				case kind == "push":
					tell("sent: " + res.Summary + took(res.At))
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
	fmt.Println("Options: --transport ssh|api --remote <git url> --project clipboard --key ~/.clipsync/key")
	fmt.Println("         --quiet --push=false --pull --window 1s --interval 5s")
}
