// Package notify shows macOS notifications.
package notify

import (
	"os/exec"
	"strconv"
)

// Show displays a notification; failures are ignored (e.g. not on macOS).
func Show(title, text string) {
	script := "display notification " + strconv.Quote(text) + " with title " + strconv.Quote(title)
	_ = exec.Command("/usr/bin/osascript", "-e", script).Run()
}
