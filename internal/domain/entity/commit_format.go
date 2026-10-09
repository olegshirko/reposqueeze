package entity

import (
	"fmt"
	"regexp"
	"strings"
)

// CommitFormat shapes local commit messages as "<type>: <title> <task>",
// e.g. "fix: handle empty input TASK-123".
type CommitFormat struct {
	Type string `json:"type,omitempty"` // fix, feat, test, ...
	Task string `json:"task,omitempty"` // task reference appended to the title; may contain spaces
}

var (
	commitTypeRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// conventionalPrefixRe matches "feat: ", "fix(scope): ", "refactor!: ".
	conventionalPrefixRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\([^)]*\))?!?:\s*`)
)

// IsZero reports whether no formatting is configured.
func (f CommitFormat) IsZero() bool {
	return f.Type == "" && f.Task == ""
}

// Validate checks the type and task values.
func (f CommitFormat) Validate() error {
	if f.Type != "" && !commitTypeRe.MatchString(f.Type) {
		return fmt.Errorf("invalid commit type %q: use a lowercase word such as fix, feat, test", f.Type)
	}
	if strings.ContainsAny(f.Task, "\r\n") {
		return fmt.Errorf("invalid task %q: must be a single line", f.Task)
	}
	return nil
}

// Apply rewrites the title (first line) of msg; the body is kept.
// An existing conventional prefix is replaced by Type, and Task is appended
// unless the title already mentions it.
func (f CommitFormat) Apply(msg string) string {
	f.Task = strings.TrimSpace(f.Task)
	if f.IsZero() {
		return msg
	}
	title, body, hasBody := strings.Cut(msg, "\n")
	title = strings.TrimSpace(title)
	if f.Type != "" {
		title = f.Type + ": " + conventionalPrefixRe.ReplaceAllString(title, "")
	}
	if f.Task != "" && !strings.Contains(title, f.Task) {
		title += " " + f.Task
	}
	if !hasBody {
		return title
	}
	return title + "\n" + body
}

// Merge returns f with empty fields taken from fallback.
func (f CommitFormat) Merge(fallback CommitFormat) CommitFormat {
	f.Task = strings.TrimSpace(f.Task)
	if f.Type == "" {
		f.Type = fallback.Type
	}
	if f.Task == "" {
		f.Task = fallback.Task
	}
	return f
}
